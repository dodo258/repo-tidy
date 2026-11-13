package judge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tr "github.com/dodo258/repo-tidy/internal/testrepo"
)

func noProcs(context.Context) ([]Proc, error) { return nil, nil }

// startSleep 在 dir 里起一个 sleep 子进程，测试结束时杀掉；返回 kill 函数，调用后进程已退出。
func startSleep(t *testing.T, dir string) (*exec.Cmd, func()) {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := false
	kill := func() {
		if done {
			return
		}
		done = true
		_ = cmd.Process.Kill() // 进程可能已退出，Wait 会收尾
		_ = cmd.Wait()
	}
	t.Cleanup(kill)
	return cmd, kill
}

func inspectWith(t *testing.T, dir string, opts Options) RepoReport {
	t.Helper()
	rep, err := Inspect(context.Background(), dir, opts)
	if err != nil {
		t.Fatalf("Inspect：%v", err)
	}
	return rep
}

// 内容已在 main 上的干净 worktree：有进程的工作目录在里面就留着，进程退出、又过了 min-idle 才可删。
func TestWorktreeWithProcessIsKept(t *testing.T) {
	dir := setup(t)
	wt := filepath.Join(t.TempDir(), "busy") // macOS 上经过 /var → /private/var 的符号链接
	tr.Git(t, dir, "worktree", "add", "-q", "-b", "busy", wt)
	sub := filepath.Join(wt, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	tr.Idle(t, wt)
	cmd, kill := startSleep(t, sub) // 子目录也算

	rep := inspectWith(t, dir, DefaultOptions())
	w := find(t, rep.Worktrees, "busy")
	wantKeep(t, w, fmt.Sprintf("sleep（pid %d）", cmd.Process.Pid))
	wantKeep(t, find(t, rep.Branches, "busy"), "在留着的 worktree 里检出")

	kill()
	rep = inspectWith(t, dir, DefaultOptions())
	wantRemovable(t, find(t, rep.Worktrees, "busy"), Merged)
	wantRemovable(t, find(t, rep.Branches, "busy"), Merged)
}

func TestRecentActivityKeepsWorktree(t *testing.T) {
	dir := setup(t)
	wt := filepath.Join(t.TempDir(), "fresh")
	tr.Git(t, dir, "worktree", "add", "-q", "-b", "fresh", wt)

	rep := inspectWith(t, dir, Options{MinIdle: 30 * time.Minute, Procs: noProcs})
	wantKeep(t, find(t, rep.Worktrees, "fresh"), "0 分钟前刚动过，可能还在用")
	wantKeep(t, find(t, rep.Branches, "fresh"), "在留着的 worktree 里检出")

	// --min-idle 0 关掉时间这条
	rep = inspectWith(t, dir, Options{MinIdle: 0, Procs: noProcs})
	wantRemovable(t, find(t, rep.Worktrees, "fresh"), Merged)

	// 只动了 reflog（比如刚切过分支）也算
	tr.Idle(t, wt)
	gitDir := tr.Git(t, wt, "rev-parse", "--absolute-git-dir")
	recent := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(filepath.Join(gitDir, "logs", "HEAD"), recent, recent); err != nil {
		t.Fatal(err)
	}
	rep = inspectWith(t, dir, Options{MinIdle: 30 * time.Minute, Procs: noProcs})
	wantKeep(t, find(t, rep.Worktrees, "fresh"), "10 分钟前刚动过")
	rep = inspectWith(t, dir, Options{MinIdle: 5 * time.Minute, Procs: noProcs})
	wantRemovable(t, find(t, rep.Worktrees, "fresh"), Merged)
}

func TestProcessListFailureKeepsWorktree(t *testing.T) {
	dir := setup(t)
	wt := filepath.Join(t.TempDir(), "unknown")
	tr.Git(t, dir, "worktree", "add", "-q", "-b", "unknown", wt)
	fail := func(context.Context) ([]Proc, error) { return nil, errors.New("lsof 超时") }

	rep := inspectWith(t, dir, Options{Procs: fail})
	wantKeep(t, find(t, rep.Worktrees, "unknown"), "没查成有没有进程在这个目录里（lsof 超时），当作可能在用")
	wantKeep(t, find(t, rep.Branches, "unknown"), "在留着的 worktree 里检出")
}

func TestListProcsFindsChild(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cmd, _ := startSleep(t, dir)
	procs, err := ListProcs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := procsIn(procs, dir)
	if len(got) != 1 || got[0].PID != cmd.Process.Pid || got[0].Name != "sleep" {
		t.Fatalf("应只找到那个 sleep（pid %d），实际 %+v", cmd.Process.Pid, got)
	}
}

func TestProcsInMatchesDirAndChildrenOnly(t *testing.T) {
	procs := []Proc{
		{1, "zsh", "/w/app"}, {2, "go", "/w/app/internal"}, {3, "vim", "/w/app2"}, {4, "make", "/w"},
	}
	var pids []int
	for _, p := range procsIn(procs, "/w/app") {
		pids = append(pids, p.PID)
	}
	if fmt.Sprint(pids) != "[1 2]" {
		t.Fatalf("应只匹配 /w/app 和它下面的目录，实际 %v", pids)
	}
}

func TestParseLsof(t *testing.T) {
	out := "p12\nczsh\nfcwd\nn/w/app\np34\ncgo\nfcwd\nn/w/app/x y\np56\nclaunchd\nfcwd\nn/\n"
	got := parseLsof([]byte(out))
	want := []Proc{{12, "zsh", "/w/app"}, {34, "go", "/w/app/x y"}, {56, "launchd", "/"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("解析 lsof 输出：%+v，想要 %+v", got, want)
	}
}

func TestProcfsProcs(t *testing.T) {
	root := t.TempDir()
	mk := func(pid, cwd, comm string) {
		d := filepath.Join(root, pid)
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if cwd != "" {
			if err := os.Symlink(cwd, filepath.Join(d, "cwd")); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(d, "comm"), []byte(comm+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("101", "/w/app", "bash")
	mk("102", "", "gone") // 读不了 cwd 的跳过
	mk("self", "/w", "x") // 不是数字的跳过
	got, err := procfsProcs(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got) != fmt.Sprint([]Proc{{101, "bash", "/w/app"}}) {
		t.Fatalf("读 /proc：%+v", got)
	}
	if _, err := procfsProcs(context.Background(), t.TempDir()); err == nil || !strings.Contains(err.Error(), "no processes read") {
		t.Fatalf("空的 /proc 应报错：%v", err)
	}
}
