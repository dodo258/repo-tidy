package judge

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tr "github.com/dodo258/repo-tidy/internal/testrepo"
)

func TestLockPIDs(t *testing.T) {
	cases := map[string][]int{
		"claude session fix-login (pid 4321 start 1732350000)": {4321},
		"claude agent a1 (pid 77)":                             {77},
		"pid 99 owns this":                                     {99},
		"held by pid 12 and pid 34":                            {12, 34},
		"agent running":                                        nil,
		"rapid 5 retries":                                      nil,
		"stupid 5":                                             nil,
		"(pid 1)":                                              nil, // init 不会是持锁的会话
		"(pid 0)":                                              nil,
		"(pid 99999999999999999999)":                           nil,
		"":                                                     nil,
	}
	for reason, want := range cases {
		if got := lockPIDs(reason); !reflect.DeepEqual(got, want) {
			t.Errorf("lockPIDs(%q) = %v，应为 %v", reason, got, want)
		}
	}
}

func TestStaleLockNeedsAllPIDsGone(t *testing.T) {
	alive := func(p int) bool { return p == 2 }
	if _, stale := staleLock("pid 3 and pid 4", alive); !stale {
		t.Fatal("两个 pid 都不在了，锁应算失效")
	}
	if _, stale := staleLock("pid 3 and pid 2", alive); stale {
		t.Fatal("还有一个持锁进程在，锁应有效")
	}
	if _, stale := staleLock("agent running", func(int) bool { return false }); stale {
		t.Fatal("没写 pid 的锁应有效")
	}
}

func TestPIDAlive(t *testing.T) {
	if !pidAlive(os.Getpid()) {
		t.Fatal("自己的进程应算在")
	}
	if pidAlive(tr.DeadPID(t)) {
		t.Fatal("已退出的进程应算不在")
	}
}

func TestLockedWorktreeWithDeadHolder(t *testing.T) {
	dir := setup(t)
	g := func(args ...string) string { return tr.Git(t, dir, args...) }
	wtRoot := filepath.Join(t.TempDir(), "wts")
	dead := tr.DeadPID(t)
	add := func(name, reason string) string {
		p := filepath.Join(wtRoot, name)
		g("worktree", "add", "-q", "-b", name, p)
		g("worktree", "lock", "--reason", reason, p)
		return p
	}
	deadReason := fmt.Sprintf("claude session %%s (pid %d start 1732350000)", dead)

	// 持锁进程已退出、已合并、干净：可删，证据注明锁已失效
	stale := add("stale", fmt.Sprintf(deadReason, "stale"))
	tr.Commit(t, stale, "s.txt", "s\n", "stale work")
	g("merge", "-q", "--no-ff", "-m", "merge stale", "stale")
	// 持锁进程还在：留着
	add("live", fmt.Sprintf("claude session live (pid %d)", os.Getpid()))
	// 持锁进程已退出，但有未提交改动：照常按改动留着
	dirty := add("staledirty", fmt.Sprintf(deadReason, "staledirty"))
	tr.Write(t, dirty, "README.md", "changed\n")
	// 持锁进程已退出，有没合并也没推送的提交：留着
	wip := add("stalewip", fmt.Sprintf(deadReason, "stalewip"))
	tr.Commit(t, wip, "w.txt", "w\n", "unmerged work")
	// 持锁进程已退出，目录也没了：只清登记
	gone := add("stalegone", fmt.Sprintf(deadReason, "stalegone"))
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	// 锁原因里没写 pid：照旧留着
	add("plain", "on a USB drive")
	// 目录没了、锁原因没写 pid：照旧留着（git 不把锁定的报成 prunable）
	plainGone := add("plaingone", "on a USB drive")
	if err := os.RemoveAll(plainGone); err != nil {
		t.Fatal(err)
	}

	rep := inspect(t, dir)
	deadNote := fmt.Sprintf("持锁进程（pid %d）已退出", dead)
	it := find(t, rep.Worktrees, "stale")
	wantRemovable(t, it, Merged, "已合并进 main", deadNote)
	if it.StaleLock != fmt.Sprintf(deadReason, "stale") {
		t.Fatalf("StaleLock 应是原来的锁原因，实际 %q", it.StaleLock)
	}
	if !strings.Contains(it.Short, fmt.Sprintf("锁失效 pid %d", dead)) {
		t.Fatalf("短证据应注明锁失效：%s", it.Short)
	}
	wantRemovable(t, find(t, rep.Branches, "stale"), Merged, "先删检出它的 worktree")

	wantKeep(t, find(t, rep.Worktrees, "live"), fmt.Sprintf("持锁进程还在运行（pid %d）", os.Getpid()))
	wantKeep(t, find(t, rep.Branches, "live"), "在留着的 worktree 里检出")
	wantKeep(t, find(t, rep.Worktrees, "staledirty"), "未提交改动")
	wantKeep(t, find(t, rep.Worktrees, "stalewip"), "有 1 个提交")
	wantRemovable(t, find(t, rep.Worktrees, "stalegone"), Prunable, "目录已不存在", deadNote)
	wantKeep(t, find(t, rep.Worktrees, "plain"), "已锁定：on a USB drive")
	wantKeep(t, find(t, rep.Worktrees, "plaingone"), "已锁定：on a USB drive")
	for _, name := range []string{"live", "plain", "plaingone"} {
		if s := find(t, rep.Worktrees, name).StaleLock; s != "" {
			t.Fatalf("%s 的锁有效，StaleLock 应为空，实际 %q", name, s)
		}
	}
}

// 锁失效、目录也不在本机的 worktree，登记目录最近动过（可能在容器或别的机器上正用着）时留着。
func TestStaleLockGoneDirRecentlyTouched(t *testing.T) {
	dir := setup(t)
	p := filepath.Join(t.TempDir(), "wts", "away")
	tr.Git(t, dir, "worktree", "add", "-q", "-b", "away", p)
	admin := tr.Git(t, p, "rev-parse", "--absolute-git-dir")
	tr.Git(t, dir, "worktree", "lock", "--reason", fmt.Sprintf("claude session away (pid %d)", tr.DeadPID(t)), p)
	if err := os.RemoveAll(p); err != nil {
		t.Fatal(err)
	}
	opts := Options{MinIdle: 30 * time.Minute, Procs: noProcs}

	wantKeep(t, find(t, inspectWith(t, dir, opts).Worktrees, "away"), "刚动过")

	old := time.Now().Add(-2 * time.Hour)
	for _, f := range []string{"index", "HEAD", filepath.Join("logs", "HEAD")} {
		if err := os.Chtimes(filepath.Join(admin, f), old, old); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	wantRemovable(t, find(t, inspectWith(t, dir, opts).Worktrees, "away"), Prunable, "目录已不存在", "已退出")
}
