package judge

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tr "github.com/dodo258/repo-tidy/internal/testrepo"
)

func setup(t *testing.T) string {
	t.Helper()
	tr.Isolate(t)
	dir := filepath.Join(t.TempDir(), "repo")
	tr.Init(t, dir)
	tr.Commit(t, dir, ".gitignore", ".env\nnode_modules/\nsecret/\n", "ignore")
	return dir
}

func inspect(t *testing.T, dir string) RepoReport {
	t.Helper()
	rep, err := Inspect(context.Background(), dir, Options{})
	if err != nil {
		t.Fatalf("Inspect：%v", err)
	}
	return rep
}

func find(t *testing.T, items []Item, name string) Item {
	t.Helper()
	for _, it := range items {
		if it.Name == name || filepath.Base(it.Name) == name {
			return it
		}
	}
	t.Fatalf("没找到 %s，现有：%v", name, items)
	return Item{}
}

func wantRemovable(t *testing.T, it Item, basis Basis, evidence ...string) {
	t.Helper()
	if !it.Removable || it.Basis != basis {
		t.Fatalf("%s 应可删（%s），实际 removable=%v basis=%s reasons=%v", it.Name, basis, it.Removable, it.Basis, it.Reasons)
	}
	for _, e := range evidence {
		if !strings.Contains(it.Evidence, e) {
			t.Fatalf("%s 的证据应含 %q，实际：%s", it.Name, e, it.Evidence)
		}
	}
}

func wantKeep(t *testing.T, it Item, reason string) {
	t.Helper()
	if it.Removable || it.Basis != Keep {
		t.Fatalf("%s 应留着，实际 basis=%s evidence=%s", it.Name, it.Basis, it.Evidence)
	}
	all := strings.Join(it.Reasons, "|")
	if !strings.Contains(all, reason) {
		t.Fatalf("%s 的原因应含 %q，实际：%s", it.Name, reason, all)
	}
}

func short8(s string) string { return s[:8] }

func TestMergedVariants(t *testing.T) {
	dir := setup(t)
	g := func(args ...string) string { return tr.Git(t, dir, args...) }

	// 普通合并
	g("switch", "-q", "-c", "merged")
	tr.Commit(t, dir, "m.txt", "m\n", "m work")
	g("switch", "-q", "main")
	g("merge", "-q", "--no-ff", "-m", "merge merged", "merged")
	mergeCommit := g("rev-parse", "HEAD")

	// 快进
	g("switch", "-q", "-c", "ff")
	ffTip := tr.Commit(t, dir, "ff.txt", "ff\n", "ff work")
	g("switch", "-q", "main")
	g("merge", "-q", "--ff-only", "ff")

	// squash，之后 main 又改了分支改过的同一行
	g("switch", "-q", "-c", "squashed")
	tr.Commit(t, dir, "s.txt", "s1\n", "s1")
	tr.Commit(t, dir, "s.txt", "s1\ns2\n", "s2")
	g("switch", "-q", "main")
	g("merge", "-q", "--squash", "squashed")
	g("commit", "-q", "-m", "squash it")
	squashCommit := g("rev-parse", "HEAD")
	tr.Commit(t, dir, "s.txt", "changed later\n", "later edit")

	// rebase：分支提交逐个 cherry-pick 进 main
	g("switch", "-q", "-c", "rebased")
	tr.Commit(t, dir, "r1.txt", "r1\n", "r1")
	tr.Commit(t, dir, "r2.txt", "r2\n", "r2")
	g("switch", "-q", "main")
	tr.Commit(t, dir, "before-rebase.txt", "x\n", "main moves on")
	g("cherry-pick", "rebased~1", "rebased")
	rebaseLast := g("rev-parse", "HEAD")

	// 内容和别的改动一起进了 main（patch-id 对不上），之后 main 又改了别的文件
	g("switch", "-q", "-c", "combined")
	tr.Commit(t, dir, "c.txt", "c\n", "c work")
	g("switch", "-q", "main")
	tr.Write(t, dir, "c.txt", "c\n")
	tr.Write(t, dir, "other.txt", "other\n")
	g("add", "-A")
	g("commit", "-q", "-m", "combined landing")
	combined := g("rev-parse", "HEAD")
	tr.Commit(t, dir, "z.txt", "z\n", "unrelated")

	// 没合并也没推送
	g("switch", "-q", "-c", "wip")
	tr.Commit(t, dir, "w.txt", "w\n", "wip work")
	g("switch", "-q", "main")

	// 和 main 改了同一行、有冲突，没合并
	g("switch", "-q", "-c", "conflict")
	tr.Commit(t, dir, "a.txt", "X\n2\n3\n4\n5\n6\n7\n8\n9\n10\n", "conflict edit")
	g("switch", "-q", "main")
	tr.Commit(t, dir, "a.txt", "Y\n2\n3\n4\n5\n6\n7\n8\n9\n10\n", "main edit")

	rep := inspect(t, dir)
	if rep.DefaultBranch != "main" {
		t.Fatalf("默认分支 = %q", rep.DefaultBranch)
	}
	wantRemovable(t, find(t, rep.Branches, "merged"), Merged, "已合并进 main", short8(mergeCommit))
	wantRemovable(t, find(t, rep.Branches, "ff"), Merged, "已在 main 上", short8(ffTip))
	wantRemovable(t, find(t, rep.Branches, "squashed"), Merged, "squash", short8(squashCommit))
	wantRemovable(t, find(t, rep.Branches, "rebased"), Merged, "rebase", short8(rebaseLast))
	wantRemovable(t, find(t, rep.Branches, "combined"), Merged, "内容已全部在 main 里", short8(combined))
	wantKeep(t, find(t, rep.Branches, "wip"), "有 1 个提交既不在 main 也没推到任何远程")
	wantKeep(t, find(t, rep.Branches, "conflict"), "有 1 个提交")
	for _, b := range rep.Branches {
		if b.Name == "main" {
			t.Fatal("默认分支不该出现在分支列表里")
		}
	}
}

func TestPushed(t *testing.T) {
	dir := setup(t)
	g := func(args ...string) string { return tr.Git(t, dir, args...) }
	bare := filepath.Join(t.TempDir(), "backup.git")
	tr.Git(t, filepath.Dir(bare), "init", "-q", "--bare", bare)
	g("remote", "add", "backup", bare)

	g("switch", "-q", "-c", "pushed")
	tip := tr.Commit(t, dir, "p.txt", "p\n", "pushed work")
	g("push", "-q", "backup", "pushed")

	g("switch", "-q", "-c", "ahead")
	g("push", "-q", "backup", "ahead")
	tr.Commit(t, dir, "q.txt", "q\n", "local only")
	g("switch", "-q", "main")

	rep := inspect(t, dir)
	it := find(t, rep.Branches, "pushed")
	wantRemovable(t, it, Pushed, "已推到 backup/pushed", short8(tip))
	if it.Remote != "backup" || it.RemoteRef != "refs/heads/pushed" {
		t.Fatalf("远程字段不对：%s %s", it.Remote, it.RemoteRef)
	}
	wantKeep(t, find(t, rep.Branches, "ahead"), "有 1 个提交既不在 main 也没推到任何远程：")
	wantUnmerged(t, rep.Branches, map[string]bool{"pushed": false, "ahead": true})
}

// wantUnmerged 核对每一项的 Unmerged 标记。
func wantUnmerged(t *testing.T, items []Item, want map[string]bool) {
	t.Helper()
	for name, w := range want {
		if it := find(t, items, name); it.Unmerged != w {
			t.Fatalf("%s 的 Unmerged 应为 %v", name, w)
		}
	}
}

func TestWorktrees(t *testing.T) {
	dir := setup(t)
	g := func(args ...string) string { return tr.Git(t, dir, args...) }
	wtRoot := filepath.Join(t.TempDir(), "wts")
	add := func(name string, extra ...string) string {
		p := filepath.Join(wtRoot, name)
		g(append([]string{"worktree", "add", "-q"}, append(extra, p)...)...)
		return p
	}

	// 已合并、干净
	clean := add("clean", "-b", "clean")
	tr.Commit(t, clean, "clean.txt", "c\n", "clean work")
	g("merge", "-q", "--no-ff", "-m", "merge clean", "clean")

	// 已合并，但有未提交改动
	dirty := add("dirty", "-b", "dirty")
	tr.Write(t, dirty, "README.md", "changed\n")
	// 已合并，有未跟踪文件
	untracked := add("untracked", "-b", "untracked")
	tr.Write(t, untracked, "notes.txt", "keep me\n")
	// 已合并，有被忽略的 .env
	envwt := add("envwt", "-b", "envwt")
	tr.Write(t, envwt, ".env", "TOKEN=1\n")
	// 已合并，有被忽略的 node_modules 和与主工作区相同的 .env
	regen := add("regen", "-b", "regen")
	tr.Write(t, regen, "node_modules/x/index.js", "x\n")
	tr.Write(t, dir, ".env", "SAME=1\n")
	tr.Write(t, regen, ".env", "SAME=1\n")
	// 被忽略的目录不在可再生清单里
	secret := add("secret", "-b", "secret")
	tr.Write(t, secret, "secret/key.pem", "k\n")
	// 锁定
	locked := add("locked", "-b", "locked")
	g("worktree", "lock", "--reason", "agent running", locked)
	// 目录已删
	gone := add("gone", "-b", "gone")
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	// 分离头指针，指向 main 上的提交
	g("worktree", "add", "-q", "--detach", filepath.Join(wtRoot, "detached"), "main")
	// 未合并的分支
	wip := add("wip", "-b", "wipwt")
	tr.Commit(t, wip, "w.txt", "w\n", "wt wip")
	// 做到一半的 merge
	mid := add("mid", "-b", "mid")
	tr.Commit(t, mid, "a.txt", "MID\n", "mid edit")
	tr.Commit(t, dir, "a.txt", "MAIN\n", "main edit")
	if out, err := exec.Command("git", "-C", mid, "merge", "main").CombinedOutput(); err == nil {
		t.Fatalf("预期 merge 冲突，实际成功：%s", out)
	}

	rep := inspect(t, dir)
	if len(rep.Worktrees) != 11 {
		t.Fatalf("应有 11 个附加 worktree，实际 %d", len(rep.Worktrees))
	}
	wantRemovable(t, find(t, rep.Worktrees, "clean"), Merged, "已合并进 main")
	wantRemovable(t, find(t, rep.Branches, "clean"), Merged, "先删检出它的 worktree")
	wantKeep(t, find(t, rep.Worktrees, "dirty"), "未提交改动")
	wantKeep(t, find(t, rep.Branches, "dirty"), "在留着的 worktree 里检出")
	wantKeep(t, find(t, rep.Worktrees, "untracked"), "notes.txt")
	wantKeep(t, find(t, rep.Worktrees, "envwt"), ".env")
	wantRemovable(t, find(t, rep.Worktrees, "regen"), Merged)
	wantKeep(t, find(t, rep.Worktrees, "secret"), "secret/")
	wantKeep(t, find(t, rep.Worktrees, "locked"), "agent running")
	wantRemovable(t, find(t, rep.Worktrees, "gone"), Prunable, "目录已不存在")
	wantRemovable(t, find(t, rep.Worktrees, "detached"), Merged)
	wantKeep(t, find(t, rep.Worktrees, "wip"), "有 1 个提交")
	wantKeep(t, find(t, rep.Worktrees, "mid"), "做到一半的 merge")
	// 未合并 = 有独有提交或未提交改动；锁定、被忽略文件、可删项都不算
	wantUnmerged(t, rep.Worktrees, map[string]bool{"clean": false, "dirty": true, "untracked": true, "envwt": false,
		"secret": false, "locked": false, "gone": false, "wip": true, "mid": true})
	wantUnmerged(t, rep.Branches, map[string]bool{"dirty": false, "wipwt": true, "mid": true, "locked": false})
	for _, w := range rep.Worktrees {
		if w.LastActive.IsZero() {
			t.Fatalf("%s 没有最后活动时间", w.Name)
		}
	}
}

func TestMainWorktreeBranchKept(t *testing.T) {
	dir := setup(t)
	tr.Git(t, dir, "switch", "-q", "-c", "here")
	rep := inspect(t, dir)
	wantKeep(t, find(t, rep.Branches, "here"), "主工作区当前检出的分支")
	wantUnmerged(t, rep.Branches, map[string]bool{"here": false})
}

func TestNoDefaultBranch(t *testing.T) {
	dir := setup(t)
	tr.Git(t, dir, "branch", "-m", "main", "trunk")
	tr.Git(t, dir, "branch", "other")
	rep := inspect(t, dir)
	if rep.Note == "" {
		t.Fatal("没有默认分支时应有说明")
	}
	wantKeep(t, find(t, rep.Branches, "other"), "没有默认分支")
}

// 扫描不能改仓库：.git 下不多不少文件，大小不变，对象以外的文件修改时间也不变。
func TestInspectIsReadOnly(t *testing.T) {
	dir := setup(t)
	g := func(args ...string) string { return tr.Git(t, dir, args...) }
	g("switch", "-q", "-c", "combined")
	tr.Commit(t, dir, "c.txt", "c\n", "c work")
	g("switch", "-q", "main")
	tr.Write(t, dir, "c.txt", "c\n")
	tr.Write(t, dir, "d.txt", "d\n")
	g("add", "-A")
	g("commit", "-q", "-m", "combined landing")
	wt := filepath.Join(t.TempDir(), "wt")
	g("worktree", "add", "-q", wt, "-b", "side")
	tr.Write(t, wt, "README.md", "dirty\n")
	tr.Write(t, dir, "README.md", "dirty main\n")

	before := tr.Snapshot(t, filepath.Join(dir, ".git"))
	rep := inspect(t, dir)
	wantRemovable(t, find(t, rep.Branches, "combined"), Merged, "内容已全部在 main 里")
	after := tr.Snapshot(t, filepath.Join(dir, ".git"))
	if before != after {
		t.Fatalf("扫描改了 .git：\n%s", tr.LineDiff(before, after))
	}
}

func TestLastReflogTime(t *testing.T) {
	p := filepath.Join(t.TempDir(), "HEAD")
	content := "0000 aaaa A <a@b> 1700000000 +0800\tcommit (initial): x\n" +
		"aaaa bbbb A <a@b> 1700000100 +0800\tcheckout: moving from main to x\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := lastReflogTime(p).Unix(); got != 1700000100 {
		t.Fatalf("got %d", got)
	}
	if !lastReflogTime(filepath.Join(t.TempDir(), "none")).IsZero() {
		t.Fatal("文件不存在应返回零值")
	}
}

// 被跟踪的 build/ 目录里被忽略的 .env 不是可再生的：git 报的是文件本身，不是整个目录。
func TestIgnoredFileInTrackedRegenerableDir(t *testing.T) {
	dir := setup(t)
	tr.Commit(t, dir, ".gitignore", "*.env\n", "ignore env")
	tr.Commit(t, dir, "build/entitlements.plist", "<plist/>\n", "tracked build dir")
	wt := filepath.Join(t.TempDir(), "wt")
	tr.Git(t, dir, "worktree", "add", "-q", "-b", "w", wt)
	tr.Write(t, wt, "build/prod.env", "API_KEY=only-here\n")

	rep := inspect(t, dir)
	wantKeep(t, find(t, rep.Worktrees, "wt"), "build/prod.env")
}

// 分支经过另一个分支合并进默认分支：证据是默认分支第一父链上最早包含它的那个合并提交，
// 不是中间分支上的合并；在默认分支上的旧提交写「已在 main 上」。
func TestMergedThroughAnotherBranch(t *testing.T) {
	dir := setup(t)
	g := func(args ...string) string { return tr.Git(t, dir, args...) }
	old := g("rev-parse", "HEAD")
	g("branch", "old")
	g("switch", "-q", "-c", "inner")
	tr.Commit(t, dir, "inner.txt", "i\n", "inner work")
	g("switch", "-q", "-c", "feat", "main")
	tr.Commit(t, dir, "feat.txt", "f\n", "feat work")
	g("merge", "-q", "--no-ff", "-m", "inner into feat", "inner")
	g("switch", "-q", "main")
	tr.Commit(t, dir, "m.txt", "m\n", "main moves")
	g("merge", "-q", "--no-ff", "-m", "first merge", "feat")
	first := g("rev-parse", "HEAD")
	tr.Commit(t, dir, "after.txt", "a\n", "after")
	g("switch", "-q", "-c", "late")
	tr.Commit(t, dir, "late.txt", "l\n", "late work")
	g("switch", "-q", "main")
	g("merge", "-q", "--no-ff", "-m", "second merge", "late")
	second := g("rev-parse", "HEAD")
	tr.Commit(t, dir, "z.txt", "z\n", "tail")

	rep := inspect(t, dir)
	wantRemovable(t, find(t, rep.Branches, "inner"), Merged, "已合并进 main", short8(first))
	wantRemovable(t, find(t, rep.Branches, "feat"), Merged, "已合并进 main", short8(first))
	wantRemovable(t, find(t, rep.Branches, "late"), Merged, "已合并进 main", short8(second))
	wantRemovable(t, find(t, rep.Branches, "old"), Merged, "已在 main 上", short8(old))
}
