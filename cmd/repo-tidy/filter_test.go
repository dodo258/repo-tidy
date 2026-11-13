package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tr "github.com/dodo258/repo-tidy/internal/testrepo"
)

// filterFixture 造一个仓库：两个月前的可删分支 old-done、未合并分支 old-wip、可删 worktree wt-old（分支 wt-old），
// 今天的可删分支 new-done、可删 worktree wt-new（分支 wt-new）。返回仓库路径和两个 worktree 的路径。
func filterFixture(t *testing.T) (repo, wtOld, wtNew string) {
	t.Helper()
	tr.Isolate(t)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	wtRoot, _ := filepath.EvalSymlinks(t.TempDir())
	repo = filepath.Join(root, "r")
	wtOld, wtNew = filepath.Join(wtRoot, "wt-old"), filepath.Join(wtRoot, "wt-new")

	old := time.Now().Add(-60 * 24 * time.Hour).Truncate(time.Second)
	t.Setenv("GIT_AUTHOR_DATE", old.Format(time.RFC3339))
	t.Setenv("GIT_COMMITTER_DATE", old.Format(time.RFC3339)) // 提交时间和 reflog 记录的时间都用它
	tr.Init(t, repo)
	tr.Git(t, repo, "branch", "old-done")
	tr.Git(t, repo, "switch", "-q", "-c", "old-wip")
	tr.Commit(t, repo, "w.txt", "w\n", "old wip")
	tr.Git(t, repo, "switch", "-q", "main")
	tr.Git(t, repo, "worktree", "add", "-q", "-b", "wt-old", wtOld)
	gitDir := tr.Git(t, wtOld, "rev-parse", "--absolute-git-dir")
	for _, f := range []string{"index", "HEAD", filepath.Join("logs", "HEAD")} {
		if err := os.Chtimes(filepath.Join(gitDir, f), old, old); err != nil {
			t.Fatal(err)
		}
	}
	os.Unsetenv("GIT_AUTHOR_DATE")
	os.Unsetenv("GIT_COMMITTER_DATE")

	tr.Commit(t, repo, "n.txt", "n\n", "new on main")
	tr.Git(t, repo, "branch", "new-done")
	tr.Git(t, repo, "worktree", "add", "-q", "-b", "wt-new", wtNew)
	tr.Idle(t, wtNew)
	return repo, wtOld, wtNew
}

func runOK(t *testing.T, args ...string) string {
	t.Helper()
	var out, errb bytes.Buffer
	if code := run(context.Background(), args, &out, &errb); code != 0 {
		t.Fatalf("%v 退出码 %d：%s%s", args, code, out.String(), errb.String())
	}
	return out.String()
}

// 不筛时什么都在；--older-than 和 --only 两种筛选单用、合用，报告只剩筛选后的项，总览注明「已筛选」。
func TestFilterReport(t *testing.T) {
	repo, wtOld, wtNew := filterFixture(t)

	s := runOK(t, repo)
	for _, want := range []string{"old-done", "old-wip", "new-done", "wt-old", "wt-new"} {
		if !strings.Contains(s, want) {
			t.Fatalf("不筛时报告缺 %q：\n%s", want, s)
		}
	}
	if strings.Contains(s, "已筛选") {
		t.Fatalf("不筛时不该写「已筛选」：\n%s", s)
	}

	s = runOK(t, "--older-than", "30d", repo)
	if !strings.Contains(s, "已筛选：最后活动在 30d 之前（") || !strings.Contains(s, "old-done") || !strings.Contains(s, "old-wip") || !strings.Contains(s, filepath.Base(wtOld)) {
		t.Fatalf("--older-than 30d 应留下两个月前的项并注明已筛选：\n%s", s)
	}
	if strings.Contains(s, "new-done") || strings.Contains(s, "wt-new") {
		t.Fatalf("--older-than 30d 不该有今天的项：\n%s", s)
	}
	if !strings.Contains(s, "带上同样的筛选条件加 --apply，删除标「可删」的 3 项") {
		t.Fatalf("筛选后的提示应说带上同样的条件、数字按筛选后的算：\n%s", s)
	}

	s = runOK(t, "--only", "branches", repo)
	if !strings.Contains(s, "已筛选：只看本地分支（") || !strings.Contains(s, "new-done") || strings.Contains(s, wtNew) || strings.Contains(s, "  worktree  可删") {
		t.Fatalf("--only branches 应只列分支：\n%s", s)
	}

	s = runOK(t, repo, "--only", "branches", "--older-than", "2w")
	if !strings.Contains(s, "已筛选：最后活动在 2w 之前、只看本地分支") || !strings.Contains(s, "old-done") || strings.Contains(s, "new-done") || strings.Contains(s, "  worktree  可删") {
		t.Fatalf("两种筛选合用不对：\n%s", s)
	}

	var doc struct {
		Summary struct {
			Worktrees int `json:"worktrees"`
			Branches  int `json:"branches"`
			Filter    *struct {
				OlderThan string `json:"older_than"`
				Only      string `json:"only"`
			} `json:"filter"`
		} `json:"summary"`
		Repos []struct {
			Worktrees []jsonItem `json:"worktrees"`
			Branches  []jsonItem `json:"branches"`
		} `json:"repos"`
	}
	js := runOK(t, "--json", "--older-than", "30d", "--only", "worktrees", repo)
	if err := json.Unmarshal([]byte(js), &doc); err != nil {
		t.Fatal(err)
	}
	f := doc.Summary.Filter
	if f == nil || f.OlderThan != "30d" || f.Only != "worktrees" || doc.Summary.Worktrees != 1 || doc.Summary.Branches != 0 ||
		len(doc.Repos) != 1 || len(doc.Repos[0].Branches) != 0 || len(doc.Repos[0].Worktrees) != 1 || doc.Repos[0].Worktrees[0].Name != wtOld {
		t.Fatalf("--json 筛选后应只剩 wt-old，summary.filter 写出条件：\n%s", js)
	}
}

func TestFilterBadArgs(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--older-than", "30x"}, "--older-than 要写成正数加单位"},
		{[]string{"--older-than", "0d"}, "--older-than 要写成正数加单位"},
		{[]string{"--older-than", "-3d"}, "--older-than 要写成正数加单位"},
		{[]string{"--only", "tags"}, "--only 只能是 worktrees 或 branches"},
		{[]string{"--undo", "--only", "branches"}, "--undo 不能和 --older-than、--only 一起用"},
		{[]string{"--lang", "en", "--older-than", "1y"}, "--older-than must be a positive number with a unit"},
	} {
		var out, errb bytes.Buffer
		args := append(c.args, t.TempDir())
		if code := run(context.Background(), args, &out, &errb); code != 2 || !strings.Contains(errb.String(), c.want) {
			t.Errorf("%v：应退出 2 并报 %q，实际 %d：%s", c.args, c.want, code, errb.String())
		}
	}
}

// --apply 只删筛选后的可删项：筛掉的可删项（今天的分支和 worktree、筛选外的那一类）都还在；
// 检出在被筛掉的 worktree 里的分支 wt-old 不单独删（删不掉，也不该报失败）。
func TestFilterApply(t *testing.T) {
	repo, wtOld, wtNew := filterFixture(t)

	s := runOK(t, "--apply", "--older-than", "30d", "--only", "branches", repo)
	if b := branches(t, repo); b != "main\nnew-done\nold-wip\nwt-new\nwt-old" {
		t.Fatalf("应只删 old-done，剩下：%q\n%s", b, s)
	}
	if !strings.Contains(s, "删了 1 项") {
		t.Fatalf("删除汇总应是 1 项：\n%s", s)
	}
	for _, d := range []string{wtOld, wtNew} {
		if _, err := os.Stat(d); err != nil {
			t.Fatalf("--only branches 不该删 worktree %s：%v", d, err)
		}
	}

	s = runOK(t, "--apply", "--only", "worktrees", "--older-than", "30d", repo)
	if _, err := os.Stat(wtOld); !os.IsNotExist(err) {
		t.Fatalf("wt-old 应被删：%v\n%s", err, s)
	}
	if _, err := os.Stat(wtNew); err != nil {
		t.Fatalf("今天的 wt-new 不该删：%v\n%s", err, s)
	}
	if b := branches(t, repo); b != "main\nnew-done\nold-wip\nwt-new\nwt-old" {
		t.Fatalf("--only worktrees 不该删分支，剩下：%q\n%s", b, s)
	}
}
