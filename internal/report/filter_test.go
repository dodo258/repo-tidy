package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dodo258/repo-tidy/internal/judge"
	"github.com/dodo258/repo-tidy/internal/msg"
)

func TestParseAge(t *testing.T) {
	day := 24 * time.Hour
	good := map[string]time.Duration{"30d": 30 * day, "2w": 14 * day, "1d": day, "36h": 36 * time.Hour, "90m": 90 * time.Minute}
	for s, want := range good {
		if got, ok := ParseAge(s); !ok || got != want {
			t.Errorf("ParseAge(%q) = %v, %v；应为 %v", s, got, ok, want)
		}
	}
	for _, s := range []string{"", "d", "30", "0d", "-1d", "+3d", "1.5d", "3D", "30 d", "abc", "0", "0h", "-2h", "1m2d", "99999999999999w"} {
		if got, ok := ParseAge(s); ok {
			t.Errorf("ParseAge(%q) 应报错，得到 %v", s, got)
		}
	}
}

func TestParseOnly(t *testing.T) {
	for _, s := range []string{OnlyWorktrees, OnlyBranches} {
		if got, ok := ParseOnly(s); !ok || got != s {
			t.Errorf("ParseOnly(%q) = %q, %v", s, got, ok)
		}
	}
	for _, s := range []string{"", "worktree", "branch", "all", "Branches"} {
		if _, ok := ParseOnly(s); ok {
			t.Errorf("ParseOnly(%q) 应报错", s)
		}
	}
}

// filterRepo：一个仓库里新旧 worktree、新旧分支各一个可删、一个留着（未合并），外加一个不知道最后活动时间的分支，
// 和两个检出在可删 worktree 里的旧分支（worktree 一旧一新）。
func filterRepo(now time.Time) Repo {
	old, recent := now.Add(-60*24*time.Hour), now.Add(-2*24*time.Hour)
	item := func(kind, name string, at time.Time, removable bool) judge.Item {
		it := judge.Item{Kind: kind, Name: name, LastActive: at, Removable: removable, Basis: judge.Merged}
		if !removable {
			it.Basis, it.Unmerged, it.Reasons = judge.Keep, true, []string{"有未合并的提交"}
		}
		return it
	}
	wt := func(name, branch string, at time.Time, removable bool) judge.Item {
		it := item(judge.KindWorktree, name, at, removable)
		it.Branch = branch
		return it
	}
	return Repo{RepoReport: judge.RepoReport{
		Root: "/r", DefaultBranch: "main",
		Worktrees: []judge.Item{
			wt("/wt/old", "in-old-wt", old, true),
			wt("/wt/old-wip", "", old, false),
			wt("/wt/new", "in-new-wt", recent, true),
		},
		Branches: []judge.Item{
			item(judge.KindBranch, "old", old, true),
			item(judge.KindBranch, "old-wip", old, false),
			item(judge.KindBranch, "new", recent, true),
			item(judge.KindBranch, "unknown", time.Time{}, true),
			item(judge.KindBranch, "in-old-wt", old, true),
			item(judge.KindBranch, "in-new-wt", old, true),
		},
	}}
}

func names(items []judge.Item) string {
	var out []string
	for _, it := range items {
		out = append(out, it.Name)
	}
	return strings.Join(out, ",")
}

func TestFilterApply(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	month, _ := ParseAge("30d")
	cases := []struct {
		name     string
		f        Filter
		wts, brs string
	}{
		{"不筛", Filter{}, "/wt/old,/wt/old-wip,/wt/new", "old,old-wip,new,unknown,in-old-wt,in-new-wt"},
		// in-new-wt 本身够旧，但它的 worktree 是新的、被筛掉了，单删分支删不掉，跟着筛掉
		{"只按时间", Filter{OlderThan: "30d", Age: month}, "/wt/old,/wt/old-wip", "old,old-wip,in-old-wt"},
		{"只看 worktree", Filter{Only: OnlyWorktrees}, "/wt/old,/wt/old-wip,/wt/new", ""},
		{"只看分支", Filter{Only: OnlyBranches}, "", "old,old-wip,new,unknown"},
		{"时间加只看分支", Filter{OlderThan: "30d", Age: month, Only: OnlyBranches}, "", "old,old-wip"},
		{"时间加只看 worktree", Filter{OlderThan: "30d", Age: month, Only: OnlyWorktrees}, "/wt/old,/wt/old-wip", ""},
		{"时长比所有项都长", Filter{OlderThan: "52w", Age: 52 * 7 * 24 * time.Hour}, "", ""},
	}
	src := []Repo{filterRepo(now)}
	for _, c := range cases {
		got := c.f.Apply(src, now)
		if g := names(got[0].Worktrees); g != c.wts {
			t.Errorf("%s：worktree 应为 %q，实际 %q", c.name, c.wts, g)
		}
		if g := names(got[0].Branches); g != c.brs {
			t.Errorf("%s：分支应为 %q，实际 %q", c.name, c.brs, g)
		}
	}
	if names(src[0].Branches) != "old,old-wip,new,unknown,in-old-wt,in-new-wt" {
		t.Fatal("Apply 不该改原来的结果")
	}
}

// 总览按筛选后的算并注明「已筛选」；JSON 的 summary.filter 写出条件，不筛时是 null。
func TestFilterOverviewAndJSON(t *testing.T) {
	now := time.Now()
	month, _ := ParseAge("30d")
	f := Filter{OlderThan: "30d", Age: month, Only: OnlyBranches}
	reps := f.Apply([]Repo{filterRepo(now)}, now)

	o := Summarize(reps)
	if o.Worktrees != 0 || o.Branches != 2 || o.RemovableBr != 1 || o.KeptBr != 1 || o.Unmerged != 1 || o.RemovableWT != 0 {
		t.Fatalf("总览应按筛选后的算：%+v", o)
	}
	var zh, en, plain bytes.Buffer
	Text(&zh, reps, now, "", false, msg.ZH, f)
	Text(&en, reps, now, "", false, msg.EN, f)
	Text(&plain, []Repo{filterRepo(now)}, now, "", false, msg.ZH, Filter{})
	if s := zh.String(); !strings.Contains(s, "已筛选：最后活动在 30d 之前、只看本地分支") || !strings.Contains(s, "可删 1 项（worktree 0、分支 1）") {
		t.Fatalf("中文总览没注明筛选或数字不对：\n%s", s)
	}
	if s := en.String(); !strings.Contains(s, "filtered: last active more than 30d ago, local branches only") {
		t.Fatalf("英文总览没注明筛选：\n%s", s)
	}
	if strings.Contains(plain.String(), "已筛选") {
		t.Fatalf("不筛时不该写「已筛选」：\n%s", plain.String())
	}
	if s := zh.String(); strings.Contains(s, "new") || strings.Contains(s, "/wt/") || strings.Contains(s, "unknown") {
		t.Fatalf("筛掉的项不该出现在报告里：\n%s", s)
	}

	type doc struct {
		Summary struct {
			Branches int `json:"branches"`
			Filter   *struct {
				OlderThan        string `json:"older_than"`
				OlderThanSeconds int64  `json:"older_than_seconds"`
				Only             string `json:"only"`
			} `json:"filter"`
		} `json:"summary"`
	}
	var b bytes.Buffer
	if err := JSON(&b, reps, now, f); err != nil {
		t.Fatal(err)
	}
	var d doc
	if err := json.Unmarshal(b.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if fl := d.Summary.Filter; fl == nil || fl.OlderThan != "30d" || fl.OlderThanSeconds != 30*24*3600 || fl.Only != "branches" || d.Summary.Branches != 2 {
		t.Fatalf("summary.filter 不对：%s", b.String())
	}
	b.Reset()
	if err := JSON(&b, []Repo{filterRepo(now)}, now, Filter{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `"filter": null`) {
		t.Fatalf("不筛时 summary.filter 应为 null：%s", b.String())
	}
}
