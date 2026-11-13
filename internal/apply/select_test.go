package apply

import (
	"strings"
	"testing"

	"github.com/dodo258/repo-tidy/internal/judge"
	"github.com/dodo258/repo-tidy/internal/msg"
)

func TestReadWants(t *testing.T) {
	ok := `{"items":[{"repo":"/r","kind":"branch","name":"x","head":"abc"}]}`
	if ws, err := ReadWants(strings.NewReader(ok), msg.ZH); err != nil || len(ws) != 1 || ws[0].Name != "x" {
		t.Fatalf("合法输入读错了：%v %+v", err, ws)
	}
	// --json 报告里的一项原样送回也要能读
	full := `{"items":[{"repo":"/r","kind":"branch","name":"x","branch":"x","head":"abc","verdict":"removable",` +
		`"basis":"pushed","evidence":"e","reasons":[],"unmerged":false,"last_active":null,` +
		`"remote":"origin","remote_ref":"refs/heads/x","size_bytes":3}]}`
	if ws, err := ReadWants(strings.NewReader(full), msg.ZH); err != nil || len(ws) != 1 || ws[0] != (Want{Repo: "/r", Kind: "branch", Name: "x", Head: "abc"}) {
		t.Fatalf("--json 的项原样送回读错了：%v %+v", err, ws)
	}
	for _, bad := range []string{
		`{"items":[{"repo":"/r","kind":"tag","name":"x","head":"abc"}]}`,
		`{"items":[{"repo":"/r","kind":"branch","name":"x"}]}`,
		`{"items":[{"repo":"/r","kind":"branch","name":"x","head":"abc","force":true}]}`,
		`not json`,
	} {
		if _, err := ReadWants(strings.NewReader(bad), msg.ZH); err == nil {
			t.Errorf("应拒绝：%s", bad)
		}
	}
}

func TestSelectKeepsOnlyPicked(t *testing.T) {
	rep := judge.RepoReport{
		Root: "/r",
		Worktrees: []judge.Item{
			{Kind: judge.KindWorktree, Name: "/w", Branch: "c", Head: "3", Removable: true},
		},
		Branches: []judge.Item{
			{Kind: judge.KindBranch, Name: "a", Head: "1", Removable: true},
			{Kind: judge.KindBranch, Name: "b", Head: "2", Removable: true},
			{Kind: judge.KindBranch, Name: "c", Head: "3", Removable: true},
			{Kind: judge.KindBranch, Name: "keep", Head: "4"},
		},
	}
	got, skipped := Select(rep, []Want{
		{Repo: "/r", Kind: judge.KindBranch, Name: "a", Head: "1"},
		{Repo: "/r", Kind: judge.KindBranch, Name: "c", Head: "3"},
		{Repo: "/r", Kind: judge.KindBranch, Name: "keep", Head: "4"},
		{Repo: "/r", Kind: judge.KindBranch, Name: "gone", Head: "5"},
	}, msg.ZH)
	removable := map[string]bool{}
	for _, it := range append(append([]judge.Item(nil), got.Worktrees...), got.Branches...) {
		removable[it.Name] = it.Removable
	}
	if !removable["a"] || removable["b"] || removable["c"] || removable["/w"] || removable["keep"] {
		t.Fatalf("只该保留 a 可删：%v", removable)
	}
	if !rep.Branches[1].Removable {
		t.Fatal("Select 不该改动传进来的报告")
	}
	if len(skipped) != 3 {
		t.Fatalf("keep、gone、c 应各记一条没删：%+v", skipped)
	}
}
