package judge

import (
	"path/filepath"
	"strings"
	"testing"

	tr "github.com/dodo258/repo-tidy/internal/testrepo"
)

// withOrigin 给 dir 加一个本地裸仓库当 origin，返回在 dir 里跑 git 的函数。
func withOrigin(t *testing.T, dir string) func(args ...string) string {
	t.Helper()
	origin := filepath.Join(t.TempDir(), "origin.git")
	tr.Git(t, filepath.Dir(origin), "init", "-q", "--bare", origin)
	g := func(args ...string) string { return tr.Git(t, dir, args...) }
	g("remote", "add", "origin", origin)
	return g
}

func wantDefault(t *testing.T, rep RepoReport, name string) {
	t.Helper()
	if rep.DefaultBranch != name || rep.Note != "" {
		t.Fatalf("默认分支应为 %q，实际 %q（说明：%s）", name, rep.DefaultBranch, rep.Note)
	}
}

// 本地只有 release/...，远程有 origin/main（没有 origin/HEAD）：拿 origin/main 比对。
func TestDefaultBranchRemoteTrackingOnly(t *testing.T) {
	dir := setup(t)
	g := withOrigin(t, dir)
	g("branch", "ancestor")
	tr.Commit(t, dir, "n.txt", "n\n", "next on main")
	g("switch", "-q", "-c", "sq")
	tr.Commit(t, dir, "s.txt", "s\n", "s work")
	g("switch", "-q", "main")
	g("merge", "-q", "--squash", "sq")
	g("commit", "-q", "-m", "squash sq")
	g("switch", "-q", "-c", "wip")
	tr.Commit(t, dir, "w.txt", "w\n", "wip work")
	g("switch", "-q", "main")
	g("push", "-q", "origin", "main")
	g("switch", "-q", "-c", "release/1")
	g("branch", "-D", "main")

	rep := inspect(t, dir)
	wantDefault(t, rep, "origin/main")
	wantRemovable(t, find(t, rep.Branches, "ancestor"), Merged, "origin/main")
	wantRemovable(t, find(t, rep.Branches, "sq"), Merged, "squash 进 origin/main")
	wantKeep(t, find(t, rep.Branches, "wip"), "不在 origin/main")
	wantKeep(t, find(t, rep.Branches, "release/1"), "主工作区当前检出的分支")
	wantUnmerged(t, rep.Branches, map[string]bool{"wip": true})
}

// origin/HEAD 指向本地没有的 trunk：本地有 main 时仍用 main；本地 main 也没有时用 origin/trunk，
// 不用同样存在的 origin/main。
func TestDefaultBranchOriginHeadNotLocal(t *testing.T) {
	dir := setup(t)
	g := withOrigin(t, dir)
	g("switch", "-q", "-c", "feat")
	tr.Commit(t, dir, "f.txt", "f\n", "feat work")
	g("switch", "-q", "-c", "trunk", "main")
	g("merge", "-q", "--no-ff", "-m", "merge feat", "feat")
	g("push", "-q", "origin", "main", "trunk")
	g("remote", "set-head", "origin", "trunk")
	g("switch", "-q", "main")
	g("branch", "-D", "trunk")

	wantDefault(t, inspect(t, dir), "main")

	g("switch", "-q", "-c", "release/1")
	g("branch", "-D", "main")
	rep := inspect(t, dir)
	wantDefault(t, rep, "origin/trunk")
	wantRemovable(t, find(t, rep.Branches, "feat"), Merged, "已合并进 origin/trunk")
}

// 本地 main 和 origin/main 都有：用本地的，本地领先的内容也算已在默认分支。
func TestDefaultBranchPrefersLocal(t *testing.T) {
	dir := setup(t)
	g := withOrigin(t, dir)
	g("push", "-q", "origin", "main")
	g("remote", "set-head", "origin", "main")
	g("switch", "-q", "-c", "sq")
	tr.Commit(t, dir, "s.txt", "s\n", "s work")
	g("switch", "-q", "main")
	g("merge", "-q", "--squash", "sq")
	g("commit", "-q", "-m", "squash sq") // 没推，origin/main 落后

	rep := inspect(t, dir)
	wantDefault(t, rep, "main")
	it := find(t, rep.Branches, "sq")
	wantRemovable(t, it, Merged, "squash 进 main")
	if strings.Contains(it.Evidence, "origin/") {
		t.Fatalf("应拿本地 main 比对，证据却是：%s", it.Evidence)
	}
	for _, b := range rep.Branches {
		if b.Name == "main" {
			t.Fatal("默认分支 main 不该出现在分支列表里")
		}
	}
}
