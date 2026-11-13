package apply

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dodo258/repo-tidy/internal/judge"
	tr "github.com/dodo258/repo-tidy/internal/testrepo"
)

type fixture struct {
	dir, bare, wtRoot string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	tr.Isolate(t)
	base, err := filepath.EvalSymlinks(t.TempDir()) // macOS 的临时目录在 /var → /private/var 下
	if err != nil {
		t.Fatal(err)
	}
	f := fixture{dir: filepath.Join(base, "repo"), bare: filepath.Join(base, "backup.git"), wtRoot: filepath.Join(base, "wts")}
	tr.Init(t, f.dir)
	tr.Commit(t, f.dir, ".gitignore", ".env\n", "ignore")
	tr.Git(t, base, "init", "-q", "--bare", f.bare)
	tr.Git(t, f.dir, "remote", "add", "backup", f.bare)
	tr.Git(t, f.dir, "push", "-q", "backup", "main")
	return f
}

func (f fixture) g(t *testing.T, args ...string) string { return tr.Git(t, f.dir, args...) }

func hasBranch(t *testing.T, dir, name string) bool {
	t.Helper()
	return exec.Command("git", "-C", dir, "show-ref", "--verify", "-q", "refs/heads/"+name).Run() == nil
}

func run(t *testing.T, dir string) (judge.RepoReport, []Result, string) {
	t.Helper()
	ctx := context.Background()
	rep, err := judge.Inspect(ctx, dir, judge.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	res, err := Apply(ctx, rep, judge.Options{}, &log)
	if err != nil {
		t.Fatal(err)
	}
	return rep, res, log.String()
}

func result(t *testing.T, res []Result, name string) Result {
	t.Helper()
	for _, r := range res {
		if r.Name == name || filepath.Base(r.Name) == name {
			return r
		}
	}
	t.Fatalf("结果里没有 %s：%v", name, res)
	return Result{}
}

func TestApplyDeletesOnlyRemovableAndLogRestores(t *testing.T) {
	f := newFixture(t)
	// 已合并的 worktree + 分支
	wtMerged := filepath.Join(f.wtRoot, "merged")
	f.g(t, "worktree", "add", "-q", "-b", "merged", wtMerged)
	mergedTip := tr.Commit(t, wtMerged, "m.txt", "m\n", "m")
	f.g(t, "merge", "-q", "--no-ff", "-m", "merge m", "merged")
	// squash 合并、没有 worktree 的分支
	f.g(t, "switch", "-q", "-c", "sq")
	sqTip := tr.Commit(t, f.dir, "s.txt", "s\n", "s")
	f.g(t, "switch", "-q", "main")
	f.g(t, "merge", "-q", "--squash", "sq")
	f.g(t, "commit", "-q", "-m", "squash sq")
	// 已推到 backup 的分支
	f.g(t, "switch", "-q", "-c", "pushed")
	tr.Commit(t, f.dir, "p.txt", "p\n", "p")
	f.g(t, "push", "-q", "backup", "pushed")
	f.g(t, "switch", "-q", "main")
	// 未合并的分支、脏 worktree、带 .env 的 worktree、目录已删的 worktree
	f.g(t, "switch", "-q", "-c", "wip")
	tr.Commit(t, f.dir, "w.txt", "w\n", "w")
	f.g(t, "switch", "-q", "main")
	wtDirty := filepath.Join(f.wtRoot, "dirty")
	f.g(t, "worktree", "add", "-q", "-b", "dirty", wtDirty)
	tr.Write(t, wtDirty, "README.md", "changed\n")
	wtEnv := filepath.Join(f.wtRoot, "env")
	f.g(t, "worktree", "add", "-q", "-b", "envb", wtEnv)
	tr.Write(t, wtEnv, ".env", "SECRET=1\n")
	wtGone := filepath.Join(f.wtRoot, "gone")
	f.g(t, "worktree", "add", "-q", "-b", "gone", wtGone)
	if err := os.RemoveAll(wtGone); err != nil {
		t.Fatal(err)
	}

	_, res, log := run(t, f.dir)

	for _, name := range []string{"merged", "sq", "pushed", "gone"} {
		if hasBranch(t, f.dir, name) {
			t.Errorf("分支 %s 应已删除，结果：%v", name, res)
		}
	}
	for _, name := range []string{"main", "wip", "dirty", "envb"} {
		if !hasBranch(t, f.dir, name) {
			t.Errorf("分支 %s 应留着", name)
		}
	}
	for _, p := range []string{wtMerged} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("worktree %s 应已删除", p)
		}
	}
	if b, err := os.ReadFile(filepath.Join(wtDirty, "README.md")); err != nil || string(b) != "changed\n" {
		t.Errorf("脏 worktree 的改动应原样留着：%q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(wtEnv, ".env")); err != nil {
		t.Errorf(".env 应留着：%v", err)
	}
	wts := f.g(t, "worktree", "list", "--porcelain")
	if strings.Contains(wts, wtGone) || strings.Contains(wts, wtMerged) {
		t.Errorf("worktree 登记应已清掉：\n%s", wts)
	}
	if r := result(t, res, "pushed"); !r.Done {
		t.Errorf("pushed 应删掉：%s", r.Msg)
	}

	// 日志里的恢复命令能把分支和 worktree 找回来
	for _, want := range []string{"branch 'merged' " + mergedTip, "branch 'sq' " + sqTip, "worktree add '" + wtMerged + "' 'merged'"} {
		if !strings.Contains(log, want) {
			t.Fatalf("日志缺恢复命令 %q：\n%s", want, log)
		}
	}
	for _, l := range strings.Split(log, "\n") {
		cmd, ok := strings.CutPrefix(strings.TrimSpace(l), "恢复：git -C ")
		if !ok || !strings.Contains(cmd, " branch ") {
			continue
		}
		if out, err := exec.Command("sh", "-c", "git -C "+cmd).CombinedOutput(); err != nil {
			t.Fatalf("恢复命令失败：%s\n%s", cmd, out)
		}
	}
	if f.g(t, "rev-parse", "merged") != mergedTip || f.g(t, "rev-parse", "sq") != sqTip {
		t.Fatal("恢复出来的分支提交不对")
	}
	for _, l := range strings.Split(log, "\n") {
		cmd, ok := strings.CutPrefix(strings.TrimSpace(l), "恢复：")
		if ok && strings.Contains(cmd, "worktree add '"+wtMerged) {
			if out, err := exec.Command("sh", "-c", cmd).CombinedOutput(); err != nil {
				t.Fatalf("恢复 worktree 失败：%s\n%s", cmd, out)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(wtMerged, "m.txt")); err != nil {
		t.Fatalf("恢复的 worktree 里应有 m.txt：%v", err)
	}
}

func TestApplyRemoteNoLongerHasCommit(t *testing.T) {
	f := newFixture(t)
	f.g(t, "switch", "-q", "-c", "forced")
	tr.Commit(t, f.dir, "x.txt", "x\n", "x")
	f.g(t, "push", "-q", "backup", "forced")
	f.g(t, "switch", "-q", "main")
	// 别处把远程分支强推成别的提交，本地跟踪引用还是旧的
	other := filepath.Join(t.TempDir(), "other")
	tr.Git(t, filepath.Dir(other), "clone", "-q", f.bare, other)
	tr.Git(t, other, "switch", "-q", "-c", "forced", "origin/main")
	tr.Commit(t, other, "y.txt", "y\n", "y")
	tr.Git(t, other, "push", "-q", "-f", "origin", "forced")

	rep, res, _ := run(t, f.dir)
	if it := rep.Branches[0]; it.Basis != judge.Pushed {
		t.Fatalf("报告时应按本地跟踪引用判为已推送：%+v", it)
	}
	r := result(t, res, "forced")
	if r.Done || !strings.Contains(r.Msg, "本地没有的提交") {
		t.Fatalf("远程已被强推，不该删：%+v", r)
	}
	if !hasBranch(t, f.dir, "forced") {
		t.Fatal("分支被删了")
	}
}

func TestApplyRemoteUnreachable(t *testing.T) {
	f := newFixture(t)
	f.g(t, "switch", "-q", "-c", "lonely")
	tr.Commit(t, f.dir, "x.txt", "x\n", "x")
	f.g(t, "push", "-q", "backup", "lonely")
	f.g(t, "switch", "-q", "main")
	f.g(t, "remote", "set-url", "backup", filepath.Join(t.TempDir(), "missing.git"))

	_, res, _ := run(t, f.dir)
	r := result(t, res, "lonely")
	if r.Done || !strings.Contains(r.Msg, "联网确认 backup 失败") {
		t.Fatalf("联不上远程，不该删：%+v", r)
	}
	if !hasBranch(t, f.dir, "lonely") {
		t.Fatal("分支被删了")
	}
}

func TestApplySkipsWhenStateChanged(t *testing.T) {
	f := newFixture(t)
	f.g(t, "branch", "moved")
	ctx := context.Background()
	rep, err := judge.Inspect(ctx, f.dir, judge.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// 报告之后分支上多了一个没合并的提交
	f.g(t, "switch", "-q", "moved")
	tr.Commit(t, f.dir, "n.txt", "n\n", "new work")
	f.g(t, "switch", "-q", "main")

	res, err := Apply(ctx, rep, judge.Options{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	r := result(t, res, "moved")
	if r.Done || !strings.Contains(r.Msg, "状态和报告时不一样了") {
		t.Fatalf("状态变了不该删：%+v", r)
	}
	if !hasBranch(t, f.dir, "moved") {
		t.Fatal("分支被删了")
	}
}

// 报告时判可删，删之前才有进程进到 worktree 里：重新判定时现查进程，不删。
func TestApplySkipsWorktreeThatBecameBusy(t *testing.T) {
	f := newFixture(t)
	wt := filepath.Join(f.wtRoot, "late")
	f.g(t, "worktree", "add", "-q", "-b", "late", wt)
	ctx := context.Background()
	rep, err := judge.Inspect(ctx, f.dir, judge.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if w, _ := byName(rep.Worktrees, wt); !w.Removable {
		t.Fatalf("报告时应可删：%+v", w)
	}

	cmd := exec.Command("sleep", "60")
	cmd.Dir = wt
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill() // 测试结束收掉 sleep
		_ = cmd.Wait()
	})

	res, err := Apply(ctx, rep, judge.Options{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	r := result(t, res, "late")
	if r.Done || !strings.Contains(r.Msg, fmt.Sprintf("sleep（pid %d）", cmd.Process.Pid)) {
		t.Fatalf("有进程在用的 worktree 不该删，原因要写出进程：%+v", r)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree 目录被删了：%v", err)
	}
	if !hasBranch(t, f.dir, "late") {
		t.Fatal("分支被删了")
	}
}
