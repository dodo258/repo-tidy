package apply

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dodo258/repo-tidy/internal/judge"
	"github.com/dodo258/repo-tidy/internal/msg"
	tr "github.com/dodo258/repo-tidy/internal/testrepo"
)

func undoResult(t *testing.T, res []UndoResult, name string) UndoResult {
	t.Helper()
	for _, r := range res {
		if r.Name == name || filepath.Base(r.Name) == name {
			return r
		}
	}
	t.Fatalf("撤销结果里没有 %s：%+v", name, res)
	return UndoResult{}
}

func readLog(t *testing.T, log string) []Restore {
	t.Helper()
	items, err := ReadLog(strings.NewReader(log), msg.ZH)
	if err != nil {
		t.Fatalf("读日志：%v\n%s", err, log)
	}
	return items
}

// undoFixture 删掉一个已合并的 worktree（连同分支）、一个 squash 合并的分支和一个分离头指针的 worktree，
// 返回删除日志和这几项原来的提交。
func undoFixture(t *testing.T) (f fixture, log, mergedTip, sqTip, detTip, wtMerged, wtDet string) {
	f = newFixture(t)
	wtMerged = filepath.Join(f.wtRoot, "merged")
	f.g(t, "worktree", "add", "-q", "-b", "merged", wtMerged)
	mergedTip = tr.Commit(t, wtMerged, "m.txt", "m\n", "m")
	f.g(t, "merge", "-q", "--no-ff", "-m", "merge m", "merged")
	f.g(t, "switch", "-q", "-c", "sq")
	sqTip = tr.Commit(t, f.dir, "s.txt", "s\n", "s")
	f.g(t, "switch", "-q", "main")
	f.g(t, "merge", "-q", "--squash", "sq")
	f.g(t, "commit", "-q", "-m", "squash sq")
	wtDet = filepath.Join(f.wtRoot, "det")
	f.g(t, "worktree", "add", "-q", "--detach", wtDet)
	detTip = f.g(t, "rev-parse", "HEAD")

	_, res, log := run(t, f.dir)
	for _, name := range []string{"merged", "sq", "det"} {
		if r := result(t, res, name); !r.Done {
			t.Fatalf("准备时 %s 应删掉：%+v", name, r)
		}
	}
	return
}

func TestUndoPreviewThenRestore(t *testing.T) {
	f, log, mergedTip, sqTip, detTip, wtMerged, wtDet := undoFixture(t)
	items := readLog(t, log)
	if len(items) != 4 {
		t.Fatalf("日志里应有 4 条恢复命令：%+v", items)
	}
	ctx := context.Background()

	// 只看：什么都不改
	before := tr.Snapshot(t, filepath.Dir(f.dir))
	res := Undo(ctx, items, false, msg.ZH)
	if d := tr.LineDiff(before, tr.Snapshot(t, filepath.Dir(f.dir))); d != "" {
		t.Fatalf("只看模式改了文件：\n%s", d)
	}
	for _, r := range res {
		if r.Status != UndoReady {
			t.Fatalf("每项都应能恢复：%+v", r)
		}
	}
	// 分支排在 worktree 前面（worktree 要检出的分支得先建回）
	if res[0].Kind != judge.KindBranch || res[1].Kind != judge.KindBranch {
		t.Fatalf("应先恢复分支：%+v", res)
	}

	res = Undo(ctx, items, true, msg.ZH)
	for _, r := range res {
		if r.Status != UndoRestored {
			t.Fatalf("每项都应恢复：%+v", r)
		}
	}
	if f.g(t, "rev-parse", "merged") != mergedTip || f.g(t, "rev-parse", "sq") != sqTip {
		t.Fatal("恢复出来的分支没指向原来的提交")
	}
	if tr.Git(t, wtMerged, "rev-parse", "--abbrev-ref", "HEAD") != "merged" {
		t.Fatal("worktree 应检出原来的分支")
	}
	if _, err := os.Stat(filepath.Join(wtMerged, "m.txt")); err != nil {
		t.Fatalf("恢复的 worktree 里应有 m.txt：%v", err)
	}
	if tr.Git(t, wtDet, "rev-parse", "HEAD") != detTip {
		t.Fatal("分离头指针的 worktree 应回到原来的提交")
	}
	wts := f.g(t, "worktree", "list", "--porcelain")
	if !strings.Contains(wts, "worktree "+wtMerged) || !strings.Contains(wts, "worktree "+wtDet) {
		t.Fatalf("worktree 应登记在原路径：\n%s", wts)
	}

	// 再撤销一次：都已经在了，全部跳过
	for _, r := range Undo(ctx, items, true, msg.ZH) {
		if r.Status != UndoExists {
			t.Fatalf("已经恢复过的应跳过：%+v", r)
		}
	}
}

func TestUndoSkipsExistingBranchAndDir(t *testing.T) {
	f, log, _, _, _, wtMerged, _ := undoFixture(t)
	// 删完之后，原路径上有了别的东西，同名分支也指向了别的提交
	tr.Write(t, wtMerged, "mine.txt", "keep me\n")
	f.g(t, "branch", "sq", "main")
	mainTip := f.g(t, "rev-parse", "main")

	res := Undo(context.Background(), readLog(t, log), true, msg.ZH)
	if r := undoResult(t, res, wtMerged); r.Status != UndoExists || !strings.Contains(r.Msg, "目录已经在了") {
		t.Fatalf("目录已存在应跳过：%+v", r)
	}
	if b, err := os.ReadFile(filepath.Join(wtMerged, "mine.txt")); err != nil || string(b) != "keep me\n" {
		t.Fatalf("已存在的目录不能动：%q %v", b, err)
	}
	if r := undoResult(t, res, "sq"); r.Status != UndoExists || !strings.Contains(r.Msg, mainTip[:8]) {
		t.Fatalf("同名分支已存在应跳过并说明它现在指向哪：%+v", r)
	}
	if f.g(t, "rev-parse", "sq") != mainTip {
		t.Fatal("已存在的分支不能动")
	}
	// 其余的照常恢复
	if r := undoResult(t, res, "merged"); r.Kind == judge.KindBranch && r.Status != UndoRestored {
		t.Fatalf("merged 分支应恢复：%+v", r)
	}
	if r := undoResult(t, res, "det"); r.Status != UndoRestored {
		t.Fatalf("det 应恢复：%+v", r)
	}
}

func TestUndoReportsCommitRemovedByGC(t *testing.T) {
	f := newFixture(t)
	f.g(t, "switch", "-q", "-c", "lost")
	sha := tr.Commit(t, f.dir, "l.txt", "l\n", "lost work")
	f.g(t, "switch", "-q", "main")
	f.g(t, "branch", "-D", "lost")
	f.g(t, "reflog", "expire", "--expire=now", "--all")
	f.g(t, "gc", "-q", "--prune=now")
	wt := filepath.Join(f.wtRoot, "lostwt")
	log := "# repo-tidy --apply\n" +
		"  恢复：git -C " + quote(f.dir) + " branch 'lost' " + sha + "\n" +
		"  恢复：git -C " + quote(f.dir) + " worktree add --detach " + quote(wt) + " " + sha + "\n" +
		"  恢复：git -C " + quote(f.dir) + " worktree add " + quote(wt+"-b") + " 'lost'\n"
	for _, doIt := range []bool{false, true} {
		res := Undo(context.Background(), readLog(t, log), doIt, msg.ZH)
		if r := undoResult(t, res, "lost"); r.Status != UndoLost || !strings.Contains(r.Msg, sha[:8]) || !strings.Contains(r.Msg, "gc") {
			t.Fatalf("提交已被 gc 应报出来：%+v", r)
		}
		if r := undoResult(t, res, "lostwt"); r.Status != UndoLost {
			t.Fatalf("worktree 的提交已被 gc 应报出来：%+v", r)
		}
		if r := undoResult(t, res, "lostwt-b"); r.Status != UndoFailed || !strings.Contains(r.Msg, "分支 lost 不在了") {
			t.Fatalf("要检出的分支恢复不了，worktree 也建不回：%+v", r)
		}
	}
	if hasBranch(t, f.dir, "lost") {
		t.Fatal("不该建出分支")
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatal("不该建出目录")
	}
}

func TestUndoMissingRepo(t *testing.T) {
	tr.Isolate(t)
	gone := filepath.Join(t.TempDir(), "gone")
	log := "  restore: git -C " + quote(gone) + " branch 'x' " + strings.Repeat("a", 40) + "\n"
	res := Undo(context.Background(), readLog(t, log), false, msg.EN)
	if len(res) != 1 || res[0].Status != UndoFailed || !strings.Contains(res[0].Msg, "repo is gone") {
		t.Fatalf("仓库不在了应报失败：%+v", res)
	}
}

func TestReadLog(t *testing.T) {
	sha := strings.Repeat("0123456789", 4)
	odd := "/tmp/it's here"
	log := "# 仓库 x\nbranch b @ " + sha + " — 证据里写了 恢复：git -C 也不算\n" +
		"  恢复：git -C " + quote(odd) + " branch 'feat/x' " + sha + "\n" +
		"  restore: git -C '/r' worktree add '/w/a b' 'feat/x'\n" +
		"  restore: git -C '/r' worktree add --detach '/w/d' " + sha + "\n"
	got := readLog(t, log)
	want := []Restore{
		{Repo: odd, Kind: judge.KindBranch, Name: "feat/x", Head: sha},
		{Repo: "/r", Kind: judge.KindWorktree, Name: "/w/a b", Branch: "feat/x"},
		{Repo: "/r", Kind: judge.KindWorktree, Name: "/w/d", Head: sha},
	}
	if len(got) != len(want) {
		t.Fatalf("应读出 %d 条：%+v", len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 条：\n得到 %+v\n应为 %+v", i+1, got[i], want[i])
		}
	}

	for name, bad := range map[string]string{
		"没有恢复命令":   "# repo-tidy --apply\nbranch x @ y — z\n",
		"提交号不对":    "  恢复：git -C '/r' branch 'x' HEAD\n",
		"引号没闭合":    "  恢复：git -C '/r branch 'x' " + sha + "\n",
		"不认的命令":    "  恢复：git -C '/r' reset --hard " + sha + "\n",
		"选项当路径":    "  恢复：git -C '/r' worktree add --force 'x'\n",
		"相对路径":     "  恢复：git -C '/r' worktree add 'w' 'x'\n",
		"选项当分支":    "  恢复：git -C '/r' branch '-f' " + sha + "\n",
		"仓库不是绝对路径": "  恢复：git -C 'r' branch 'x' " + sha + "\n",
	} {
		if _, err := ReadLog(strings.NewReader(bad), msg.ZH); err == nil {
			t.Errorf("%s：应报错", name)
		}
	}
}
