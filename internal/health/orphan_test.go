package health

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dodo258/repo-tidy/internal/gitx"
	"github.com/dodo258/repo-tidy/internal/msg"
	tr "github.com/dodo258/repo-tidy/internal/testrepo"
)

// orphanRepo 造一个忽略 .claude/worktrees 的仓库；add 在里面加一个 worktree，lose 删掉它的登记、留着目录
// （就是 Claude Code 退出时清了登记没清目录的样子）。
type orphanRepo struct {
	t    *testing.T
	root string
}

func newOrphanRepo(t *testing.T) orphanRepo {
	t.Helper()
	tr.Isolate(t)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := orphanRepo{t, filepath.Join(base, "repo")}
	tr.Init(t, r.root)
	return r
}

func (r orphanRepo) dir(name string) string {
	return filepath.Join(r.root, ".claude", "worktrees", name)
}

func (r orphanRepo) add(name string) {
	r.t.Helper()
	tr.Git(r.t, r.root, "worktree", "add", "-q", "--detach", r.dir(name))
}

func (r orphanRepo) lose(name string) {
	r.t.Helper()
	admin := tr.Git(r.t, r.dir(name), "rev-parse", "--absolute-git-dir")
	if err := os.RemoveAll(admin); err != nil {
		r.t.Fatal(err)
	}
}

// orphans 把孤儿目录整理成「相对 .claude/worktrees 的路径 有没有 .git 文件」，每行一个。
func (r orphanRepo) orphans() string {
	r.t.Helper()
	return r.list(inspect(r.t, r.root, false))
}

func (r orphanRepo) list(rep Report) string {
	r.t.Helper()
	var out []string
	for _, o := range rep.Orphans {
		rel, err := filepath.Rel(r.dir(""), o.Path)
		if err != nil {
			r.t.Fatal(err)
		}
		kind := "no-git"
		if o.HasGitFile {
			kind = "git-file"
		}
		out = append(out, filepath.ToSlash(rel)+" "+kind)
	}
	return strings.Join(out, "\n")
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// 登记没了、目录还在的报出来，大小只算在它自己头上；扫描不写任何东西。
func TestOrphans(t *testing.T) {
	r := newOrphanRepo(t)
	if got := r.orphans(); got != "" {
		t.Fatalf("没有 .claude/worktrees 时不该有孤儿目录：%s", got)
	}
	r.add("live")
	r.add("lost")
	before := inspect(t, r.root, false)
	r.lose("lost")
	tr.Write(t, r.dir("lost"), "big.bin", strings.Repeat("x", 5000))
	tr.Write(t, r.dir("half-removed"), "src/left.go", "package left\n") // git worktree remove 删到一半，连 .git 都没了

	snap := tr.Snapshot(t, r.root)
	rep := inspect(t, r.root, false)
	if d := tr.LineDiff(snap, tr.Snapshot(t, r.root)); d != "" {
		t.Fatalf("扫描改了东西：\n%s", d)
	}
	if got, want := r.list(rep), "half-removed no-git\nlost git-file"; got != want {
		t.Fatalf("孤儿目录：\n%s\n想要：\n%s", got, want)
	}
	if rep.Orphans[0].Bytes != int64(len("package left\n")) || rep.Orphans[1].Bytes < 5000 {
		t.Fatalf("孤儿目录的大小不对：%+v", rep.Orphans)
	}
	if rep.Main.Bytes != before.Main.Bytes {
		t.Fatalf("孤儿目录不该算进主工作区：之前 %d，现在 %d", before.Main.Bytes, rep.Main.Bytes)
	}
	if len(rep.Worktrees) != 1 || rep.Worktrees[0].Path != r.dir("live") {
		t.Fatalf("登记着的 worktree：%+v", rep.Worktrees)
	}
}

// 报出来的目录确实会挡住同名的 git worktree add，没报的空目录不会。
func TestOrphansBlockWorktreeAdd(t *testing.T) {
	r := newOrphanRepo(t)
	r.add("lost")
	r.lose("lost")
	must(t, os.MkdirAll(r.dir("empty"), 0o755))
	if got := r.orphans(); got != "lost git-file" {
		t.Fatalf("孤儿目录：%s", got)
	}
	r.add("empty")
	_, err := gitx.Run(context.Background(), r.root, "worktree", "add", "-q", "--detach", r.dir("lost"))
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("同名 worktree 应报 already exists：%v", err)
	}
}

// 看着像、其实不是孤儿的都不报。
func TestNotOrphans(t *testing.T) {
	for name, build := range map[string]func(r orphanRepo){
		"主仓库跟踪的目录": func(r orphanRepo) { tr.Commit(t, r.root, ".claude/worktrees/fixture/a.txt", "a\n", "fixture") },
		"独立的仓库":    func(r orphanRepo) { tr.Init(t, r.dir("clone")) },
		"别的仓库的 worktree": func(r orphanRepo) {
			other := filepath.Join(filepath.Dir(r.root), "other")
			tr.Init(t, other)
			tr.Git(t, other, "worktree", "add", "-q", "--detach", r.dir("theirs"))
		},
		"移动过、能用 git worktree repair 接回来的": func(r orphanRepo) {
			r.add("old")
			must(t, os.Rename(r.dir("old"), r.dir("new")))
		},
		"相对路径的登记": func(r orphanRepo) {
			tr.Git(t, r.root, "config", "worktree.useRelativePaths", "true")
			r.add("relative")
		},
		"还登记着、只是 .git 文件被删了": func(r orphanRepo) {
			r.add("no-git")
			must(t, os.Remove(filepath.Join(r.dir("no-git"), ".git")))
		},
		"里面套着登记的 worktree": func(r orphanRepo) {
			r.add("outer")
			r.add("outer/.claude/worktrees/inner")
			r.lose("outer")
		},
		"jj 的工作区": func(r orphanRepo) { tr.Write(t, r.dir("jj"), ".jj/repo", "x\n") },
		"里面有登记的 worktree 和零散文件的分组目录": func(r orphanRepo) {
			r.add("team/agent/live")
			tr.Write(t, r.dir("team/agent"), "notes.md", "x\n")
		},
		"文件和符号链接": func(r orphanRepo) {
			tr.Write(t, r.dir(""), ".DS_Store", "x")
			must(t, os.Symlink(r.root, r.dir("link")))
		},
		"空目录": func(r orphanRepo) { must(t, os.MkdirAll(r.dir("empty"), 0o755)) },
		"整个 worktrees 是符号链接": func(r orphanRepo) {
			shared := filepath.Join(filepath.Dir(r.root), "shared")
			tr.Write(t, shared, "x/a.txt", "a\n")
			must(t, os.MkdirAll(filepath.Join(r.root, ".claude"), 0o755))
			must(t, os.Symlink(shared, filepath.Join(r.root, ".claude", "worktrees")))
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newOrphanRepo(t)
			build(r)
			if got := r.orphans(); got != "" {
				t.Fatalf("不该报：\n%s", got)
			}
		})
	}
}

// 整个仓库挪了地方：.git 文件里全是老路径，但登记都还在，不是孤儿；这时真没了登记的照样认出来。
func TestOrphansAfterRepoMoved(t *testing.T) {
	r := newOrphanRepo(t)
	r.add("live")
	r.add("lost")
	r.lose("lost")
	moved := r.root + "-moved"
	must(t, os.Rename(r.root, moved))
	r.root = moved
	if got := r.orphans(); got != "lost git-file" {
		t.Fatalf("孤儿目录：\n%s", got)
	}
}

// 文件系统不分大小写时，登记里的路径大小写可能和磁盘上的不一样，仍是同一个 worktree。
func TestNotOrphanWhenCaseDiffers(t *testing.T) {
	r := newOrphanRepo(t)
	tr.Git(t, r.root, "worktree", "add", "-q", "--detach", filepath.Join(r.root, ".Claude", "Worktrees", "live"))
	if _, err := os.Stat(r.dir("live")); err != nil {
		t.Skip("文件系统分大小写")
	}
	if got := r.orphans(); got != "" {
		t.Fatalf("不该报：\n%s", got)
	}
}

// 登记的名字被别处同名的 worktree 用了：目录里的 .git 指到的是别人的登记，这个目录仍然是孤儿。
func TestOrphanWhoseRegistrationWasReused(t *testing.T) {
	r := newOrphanRepo(t)
	r.add("task")
	r.lose("task")
	tr.Git(t, r.root, "worktree", "add", "-q", "--detach", filepath.Join(filepath.Dir(r.root), "task"))
	if got := r.orphans(); got != "task git-file" {
		t.Fatalf("孤儿目录：\n%s", got)
	}
}

// .git 文件内容坏了，git 认不出来。
func TestOrphanWithBrokenGitFile(t *testing.T) {
	r := newOrphanRepo(t)
	tr.Write(t, r.dir("broken"), ".git", "")
	if got := r.orphans(); got != "broken git-file" {
		t.Fatalf("孤儿目录：\n%s", got)
	}
}

// 分组用的目录（agent/）往里看：还登记着的不报，没登记的逐个报；全没登记的分组也一样。
func TestOrphansInGroupDirs(t *testing.T) {
	r := newOrphanRepo(t)
	for _, n := range []string{"agent/live", "agent/lost", "gone/a", "gone/b"} {
		r.add(n)
	}
	for _, n := range []string{"agent/lost", "gone/a", "gone/b"} {
		r.lose(n)
	}
	// 里面有文件的不是分组，是删到一半的 worktree，整个算一个
	tr.Write(t, r.dir("half"), "go.mod", "module half\n")
	tr.Write(t, r.dir("half"), "vendor/.git", "gitdir: /nowhere\n")
	want := "agent/lost git-file\ngone/a git-file\ngone/b git-file\nhalf no-git"
	if got := r.orphans(); got != want {
		t.Fatalf("孤儿目录：\n%s\n想要：\n%s", got, want)
	}
}

// 有一个目录读不了：记成没查成，其余照报。
func TestOrphansUnreadableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 什么都读得了")
	}
	r := newOrphanRepo(t)
	tr.Write(t, r.dir("a-locked"), "x.txt", "x\n")
	tr.Write(t, r.dir("b-left"), "x.txt", "x\n")
	must(t, os.Chmod(r.dir("a-locked"), 0))
	t.Cleanup(func() { must(t, os.Chmod(r.dir("a-locked"), 0o755)) })
	rep, err := Inspect(context.Background(), r.root, Options{}, msg.ZH)
	must(t, err)
	if got := r.list(rep); got != "b-left no-git" {
		t.Fatalf("孤儿目录：\n%s", got)
	}
	if len(rep.Errs) != 1 || !strings.Contains(rep.Errs[0], "a-locked") {
		t.Fatalf("读不了的应记成没查成：%v", rep.Errs)
	}
}
