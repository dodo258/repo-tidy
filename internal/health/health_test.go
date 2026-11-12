package health

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tr "github.com/dodo258/repo-tidy/internal/testrepo"
)

func inspect(t *testing.T, dir string, online bool) Report {
	t.Helper()
	r, err := Inspect(context.Background(), dir, online)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Errs) > 0 {
		t.Fatalf("体检有没查成的项：%v", r.Errs)
	}
	return r
}

// stale 把没推到最新的分支整理成「分支 状态 本地多 backup多」，方便整体比对。
func stale(r Report) string {
	var out []string
	for _, s := range r.Backup.Stale {
		out = append(out, strings.Join([]string{s.Branch, string(s.State), strconv.Itoa(s.LocalOnly), strconv.Itoa(s.RemoteOnly)}, " "))
	}
	return strings.Join(out, "\n")
}

// withBackup 造一个仓库和一个本地裸仓库当 backup：
// main、pushed 推到最新；lagging 推过后本地又提交了一次；diverged 两边各多一个提交；unpushed 没推。
func withBackup(t *testing.T) (dir, bare string) {
	tr.Isolate(t)
	base := t.TempDir()
	dir, bare = filepath.Join(base, "repo"), filepath.Join(base, "backup.git")
	tr.Git(t, base, "init", "-q", "--bare", "-b", "main", bare)
	tr.Init(t, dir)
	g := func(args ...string) string { return tr.Git(t, dir, args...) }
	g("remote", "add", "backup", bare)
	for _, b := range []string{"pushed", "lagging", "diverged", "unpushed"} {
		g("branch", b)
	}
	g("push", "-q", "backup", "main", "pushed", "lagging", "diverged")

	other := filepath.Join(base, "other")
	tr.Git(t, base, "clone", "-q", "-b", "diverged", bare, other)
	tr.Commit(t, other, "o.txt", "o\n", "elsewhere")
	tr.Git(t, other, "push", "-q", "origin", "diverged")
	g("fetch", "-q", "backup")

	for _, b := range []string{"lagging", "diverged"} {
		g("switch", "-q", b)
		tr.Commit(t, dir, b+".txt", b+"\n", "local "+b)
	}
	g("switch", "-q", "main")
	return dir, bare
}

func TestBackupByTrackingRefs(t *testing.T) {
	dir, _ := withBackup(t)
	r := inspect(t, dir, false)
	if !r.Backup.Present || r.Backup.Online || r.Backup.Checked != 5 {
		t.Fatalf("backup 概况不对：%+v", r.Backup)
	}
	want := "diverged diverged 1 1\nlagging behind 1 0\nunpushed not-pushed 0 0"
	if got := stale(r); got != want {
		t.Fatalf("没推到最新的分支：\n%s\n想要：\n%s", got, want)
	}
}

func TestBackupOnline(t *testing.T) {
	dir, _ := withBackup(t)
	// 本地记录丢了 pushed 的跟踪引用：只看本地会以为没推，联网能查到其实推了
	tr.Git(t, dir, "update-ref", "-d", "refs/remotes/backup/pushed")
	if got := stale(inspect(t, dir, false)); !strings.Contains(got, "pushed not-pushed") {
		t.Fatalf("只看本地记录时 pushed 应算没推：\n%s", got)
	}
	r := inspect(t, dir, true)
	if !r.Backup.Online || r.Backup.OnlineErr != "" {
		t.Fatalf("应联网查成功：%+v", r.Backup)
	}
	want := "diverged diverged 1 1\nlagging behind 1 0\nunpushed not-pushed 0 0"
	if got := stale(r); got != want {
		t.Fatalf("联网查的结果：\n%s\n想要：\n%s", got, want)
	}
}

func TestBackupOnlineUnreachable(t *testing.T) {
	dir, _ := withBackup(t)
	tr.Git(t, dir, "remote", "set-url", "backup", filepath.Join(t.TempDir(), "gone.git"))
	start := time.Now()
	r := inspect(t, dir, true)
	if !r.Backup.Online || r.Backup.OnlineErr == "" {
		t.Fatalf("联不上应记下原因：%+v", r.Backup)
	}
	if time.Since(start) > 15*time.Second {
		t.Fatal("联不上应很快返回")
	}
	// 联不上时退回本地记录比对
	if got := stale(r); got != "diverged diverged 1 1\nlagging behind 1 0\nunpushed not-pushed 0 0" {
		t.Fatalf("联不上时应按本地记录比对：\n%s", got)
	}
}

// 远程不响应（挂住）时按超时算联不上，不会一直等。
func TestBackupOnlineTimeout(t *testing.T) {
	dir, _ := withBackup(t)
	tr.Git(t, dir, "config", "protocol.ext.allow", "always")
	tr.Git(t, dir, "remote", "set-url", "backup", "ext::sleep 30")
	old := lsRemoteTimeout
	lsRemoteTimeout = time.Second
	t.Cleanup(func() { lsRemoteTimeout = old })
	start := time.Now()
	r := inspect(t, dir, true)
	if !strings.Contains(r.Backup.OnlineErr, "超时") {
		t.Fatalf("挂住的远程应报超时：%+v", r.Backup)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("超时后没及时返回：%v", d)
	}
	if !strings.Contains(stale(r), "unpushed not-pushed") {
		t.Fatalf("超时后应按本地记录比对：\n%s", stale(r))
	}
}

func TestBackupAllUpToDate(t *testing.T) {
	dir, _ := withBackup(t)
	tr.Git(t, dir, "push", "-q", "--force", "backup", "--all")
	r := inspect(t, dir, true)
	if !r.Backup.Present || len(r.Backup.Stale) != 0 || r.Backup.Checked != 5 {
		t.Fatalf("全推到最新后不该有落后分支：%+v", r.Backup)
	}
}

func TestNoBackup(t *testing.T) {
	tr.Isolate(t)
	dir := filepath.Join(t.TempDir(), "repo")
	tr.Init(t, dir)
	tr.Git(t, dir, "branch", "x")
	r := inspect(t, dir, true)
	if r.Backup.Present || r.Backup.Online || len(r.Backup.Stale) != 0 {
		t.Fatalf("没有 backup 远程时应标出来、不联网：%+v", r.Backup)
	}
}

func TestWorkdirUpstreamAndLastCommit(t *testing.T) {
	tr.Isolate(t)
	base := t.TempDir()
	dir, bare := filepath.Join(base, "repo"), filepath.Join(base, "origin.git")
	tr.Git(t, base, "init", "-q", "--bare", "-b", "main", bare)
	tr.Init(t, dir)
	g := func(args ...string) string { return tr.Git(t, dir, args...) }
	g("remote", "add", "origin", bare)
	g("push", "-q", "-u", "origin", "main")

	other := filepath.Join(base, "other")
	tr.Git(t, base, "clone", "-q", bare, other)
	tr.Commit(t, other, "o1.txt", "1\n", "o1")
	tr.Commit(t, other, "o2.txt", "2\n", "o2")
	tr.Git(t, other, "push", "-q", "origin", "main")
	g("fetch", "-q", "origin")
	tr.Commit(t, dir, "l.txt", "l\n", "local")

	tr.Write(t, dir, "s.txt", "stash me\n")
	g("stash", "push", "-q", "-u", "-m", "test stash")
	tr.Write(t, dir, "README.md", "changed\n")
	tr.Write(t, dir, "new.txt", "new\n")

	later := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	g("branch", "later")
	wt := filepath.Join(base, "wt-later")
	g("worktree", "add", "-q", wt, "later")
	t.Setenv("GIT_COMMITTER_DATE", later.Format(time.RFC3339))
	tr.Commit(t, wt, "later.txt", "x\n", "later")
	os.Unsetenv("GIT_COMMITTER_DATE")

	r := inspect(t, dir, false)
	if r.Branch != "main" || r.Upstream != "origin/main" || r.Ahead != 1 || r.Behind != 2 || r.UpstreamGone {
		t.Fatalf("上游比对不对：分支 %q 上游 %q 领先 %d 落后 %d gone %v", r.Branch, r.Upstream, r.Ahead, r.Behind, r.UpstreamGone)
	}
	if r.Changes != 2 || r.Untracked != 1 || r.Stashes != 1 {
		t.Fatalf("改动/stash 数不对：改动 %d 未跟踪 %d stash %d", r.Changes, r.Untracked, r.Stashes)
	}
	if r.LastCommitBranch != "later" || !r.LastCommit.Equal(later) {
		t.Fatalf("最后提交应是 later @ %v，实际 %s @ %v", later, r.LastCommitBranch, r.LastCommit)
	}
	if strings.Count(tr.Git(t, dir, "stash", "list"), "\n") != 0 {
		t.Fatal("体检不该动 stash")
	}

	g("update-ref", "-d", "refs/remotes/origin/main")
	if r := inspect(t, dir, false); !r.UpstreamGone || r.Upstream != "origin/main" {
		t.Fatalf("上游引用不在时应标出来：%+v", r)
	}
	g("switch", "-q", "--detach")
	if r := inspect(t, dir, false); r.Branch != "" || r.Upstream != "" {
		t.Fatalf("分离头指针时不该有分支和上游：%q %q", r.Branch, r.Upstream)
	}
}

func TestSizes(t *testing.T) {
	tr.Isolate(t)
	base := t.TempDir()
	dir := filepath.Join(base, "repo")
	tr.Init(t, dir)
	g := func(args ...string) string { return tr.Git(t, dir, args...) }
	tr.Write(t, dir, ".gitignore", ".claude/\n")
	g("add", "-A")
	g("commit", "-q", "-m", "ignore")
	inside := filepath.Join(dir, ".claude", "worktrees", "in")
	outside := filepath.Join(base, "out")
	gone := filepath.Join(base, "gone")
	g("worktree", "add", "-q", inside, "-b", "in")
	g("worktree", "add", "-q", outside, "-b", "out")
	g("worktree", "add", "-q", gone, "-b", "gone")
	tr.Write(t, inside, "big.bin", strings.Repeat("x", 200_000))
	tr.Write(t, outside, "big.bin", strings.Repeat("y", 100_000))
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	r := inspect(t, dir, false)
	if r.GitDir.Bytes <= 0 || r.GitDir.Missing {
		t.Fatalf(".git 大小不对：%+v", r.GitDir)
	}
	if r.Main.Bytes <= 0 || r.Main.Bytes >= 100_000 {
		t.Fatalf("主工作区不该算进 .git 和里面的 worktree：%+v", r.Main)
	}
	if len(r.Worktrees) != 3 {
		t.Fatalf("应有 3 个附加 worktree：%+v", r.Worktrees)
	}
	byPath := map[string]Size{}
	for _, s := range r.Worktrees {
		byPath[filepath.Base(s.Path)] = s
	}
	if s := byPath["in"]; s.Bytes < 200_000 || s.Bytes >= 300_000 {
		t.Fatalf("放在主工作区里的 worktree 大小不对：%+v", s)
	}
	if s := byPath["out"]; s.Bytes < 100_000 || s.Bytes >= 200_000 {
		t.Fatalf("外面的 worktree 大小不对：%+v", s)
	}
	if s := byPath["gone"]; !s.Missing || s.Bytes != 0 {
		t.Fatalf("目录已删的 worktree 应标不在：%+v", s)
	}
}

// 体检不能改仓库：.git 下不多不少文件，大小和修改时间不变（含联网查 backup）。
func TestInspectIsReadOnly(t *testing.T) {
	dir, _ := withBackup(t)
	tr.Write(t, dir, "README.md", "dirty\n")
	tr.Write(t, dir, "untracked.txt", "u\n")
	before := tr.Snapshot(t, filepath.Join(dir, ".git"))
	inspect(t, dir, true)
	inspect(t, dir, false)
	if after := tr.Snapshot(t, filepath.Join(dir, ".git")); before != after {
		t.Fatalf("体检改了 .git：\n%s", tr.LineDiff(before, after))
	}
}

func TestBareRepoSkipped(t *testing.T) {
	tr.Isolate(t)
	bare := filepath.Join(t.TempDir(), "b.git")
	tr.Git(t, filepath.Dir(bare), "init", "-q", "--bare", bare)
	r, err := Inspect(context.Background(), bare, false)
	if err != nil || r.Note == "" {
		t.Fatalf("裸仓库应跳过并说明：%+v %v", r, err)
	}
}
