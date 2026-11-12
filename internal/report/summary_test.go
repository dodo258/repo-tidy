package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/dodo258/repo-tidy/internal/health"
	"github.com/dodo258/repo-tidy/internal/judge"
)

func wt(path string, removable bool, basis judge.Basis, unmerged bool) judge.Item {
	return judge.Item{Kind: judge.KindWorktree, Name: path, Removable: removable, Basis: basis, Unmerged: unmerged}
}

func br(name string, removable, unmerged bool) judge.Item {
	return judge.Item{Kind: judge.KindBranch, Name: name, Removable: removable, Unmerged: unmerged}
}

func repo(root string, wts, brs []judge.Item, sizes ...health.Size) Repo {
	return Repo{
		RepoReport: judge.RepoReport{Root: root, DefaultBranch: "main", Worktrees: wts, Branches: brs},
		Health:     &health.Report{Root: root, Worktrees: sizes, Backup: health.Backup{Present: true}},
	}
}

func TestSummarizeAndSort(t *testing.T) {
	reps := []Repo{
		repo("/p/a", nil, []judge.Item{br("kept-merged", false, false)}), // 干净：留着的不是未合并
		repo("/p/b", []judge.Item{
			wt("/w/b1", true, judge.Merged, false),
			wt("/w/b2", true, judge.Prunable, false), // 目录已不在，不占空间
			wt("/w/b3", true, judge.Pushed, false),   // 没量到大小
			wt("/w/b4", false, judge.Keep, true),
		}, []judge.Item{br("x", false, true)},
			health.Size{Path: "/w/b1", Bytes: 3000},
			health.Size{Path: "/w/b2", Missing: true},
			health.Size{Path: "/w/b4", Bytes: 9999}), // 留着的不算腾出空间
		{RepoReport: judge.RepoReport{Root: "/p/c", Err: "坏了"}},
		repo("/p/d", nil, []judge.Item{br("y", true, false)}),
	}
	o := Summarize(reps)
	want := Overview{Repos: 4, Failed: 1, Clean: 1, Worktrees: 4, Branches: 3,
		RemovableWT: 3, RemovableBr: 1, KeptWT: 1, KeptBr: 2, Unmerged: 2, FreeBytes: 3000, Unmeasured: 1}
	if o != want {
		t.Fatalf("总览不对：\n实际 %+v\n应为 %+v", o, want)
	}
	if m := Mess(reps[1]); m != 5 {
		t.Fatalf("b 的乱度应为 5（3 个可删 + 2 个未合并），实际 %d", m)
	}

	var order []string
	for _, r := range Sort(reps) {
		order = append(order, r.Root)
	}
	if got := strings.Join(order, " "); got != "/p/b /p/d /p/c /p/a" {
		t.Fatalf("排序应是 乱度 5、乱度 1、扫描失败、干净，实际 %s", got)
	}

	var w bytes.Buffer
	Text(&w, reps, time.Now(), "")
	s := w.String()
	for _, line := range []string{
		"总览  扫了 4 个仓库（1 个干净、1 个扫描失败） · 附加 worktree 4 个 · 本地分支 3 个",
		"可删 4 项（worktree 3、分支 1），能腾出 2.9 KB（另有 1 个 worktree 没量到大小）",
		"留着 3 项（worktree 1、分支 2），其中 2 项有未合并内容",
		"  /p/a  0 个附加 worktree · 1 个本地分支\n",
	} {
		if !strings.Contains(s, line) {
			t.Fatalf("缺 %q：\n%s", line, s)
		}
	}
}

func TestAttention(t *testing.T) {
	h := health.Report{Branch: "main", Upstream: "origin/main", Ahead: 2, Changes: 3, Stashes: 1,
		Backup: health.Backup{Present: true, Stale: []health.BranchSync{{Branch: "x"}}}, Errs: []string{"e"}}
	got := strings.Join(attention(h), "、")
	want := "主工作区 3 个未提交改动、1 个 stash、main 和上游不一致（领先 2，落后 0）、backup 有 1 个分支没推到最新、体检有 1 项没查成"
	if got != want {
		t.Fatalf("实际 %s\n应为 %s", got, want)
	}
	if a := attention(health.Report{Backup: health.Backup{Present: true}}); len(a) != 0 {
		t.Fatalf("没问题时不该有提示：%v", a)
	}
}
