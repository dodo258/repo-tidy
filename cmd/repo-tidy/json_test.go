package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/dodo258/repo-tidy/internal/apply"
	tr "github.com/dodo258/repo-tidy/internal/testrepo"
)

// jsonOut 是测试用来解析 --json 输出的结构，只取要对比的字段。
type jsonOut struct {
	Version int `json:"version"`
	Summary struct {
		Repos              int   `json:"repos"`
		RemovableWorktrees int   `json:"removable_worktrees"`
		RemovableBranches  int   `json:"removable_branches"`
		KeepWorktrees      int   `json:"keep_worktrees"`
		KeepBranches       int   `json:"keep_branches"`
		Unmerged           int   `json:"unmerged"`
		FreeBytes          int64 `json:"free_bytes"`
	} `json:"summary"`
	Repos []struct {
		Root      string     `json:"root"`
		Mess      int        `json:"mess"`
		Worktrees []jsonItem `json:"worktrees"`
		Branches  []jsonItem `json:"branches"`
		Health    *struct {
			Changes int `json:"changes"`
			Backup  struct {
				Present bool `json:"present"`
			} `json:"backup"`
		} `json:"health"`
	} `json:"repos"`
}

type jsonItem struct {
	Repo       string     `json:"repo"`
	Kind       string     `json:"kind"`
	Name       string     `json:"name"`
	Verdict    string     `json:"verdict"`
	Basis      string     `json:"basis"`
	Evidence   string     `json:"evidence"`
	Reasons    []string   `json:"reasons"`
	Unmerged   bool       `json:"unmerged"`
	LastActive *time.Time `json:"last_active"`
	SizeBytes  *int64     `json:"size_bytes"`
}

// textClasses 从文字报告里取出每一项的分类，键是「仓库|类型|名字」，值是「可删」或「留着」。
func textClasses(t *testing.T, s string) map[string]string {
	t.Helper()
	got := map[string]string{}
	var repo, kind string
	for _, l := range strings.Split(s, "\n") {
		switch {
		case strings.Contains(l, " 个附加 worktree · ") && !strings.HasPrefix(l, " "):
			repo = l[:strings.Index(l, "  ")]
		case strings.HasPrefix(l, "  worktree  可删 "):
			kind = "worktree"
		case strings.HasPrefix(l, "  本地分支  可删 "):
			kind = "branch"
		case strings.HasPrefix(l, "    可删  "), strings.HasPrefix(l, "    留着  "):
			rest := l[len("    可删  "):]
			name := rest[:strings.Index(rest, "  ")]
			got[repo+"|"+kind+"|"+name] = strings.TrimSpace(l)[:len("可删")]
		}
	}
	return got
}

func TestJSONMatchesTextReport(t *testing.T) {
	tr.Isolate(t)
	root := t.TempDir()
	wts := t.TempDir()
	one := filepath.Join(root, "one")
	tr.Init(t, one)
	tr.Git(t, one, "switch", "-q", "-c", "done")
	tr.Commit(t, one, "d.txt", "d\n", "done work")
	tr.Git(t, one, "switch", "-q", "main")
	tr.Git(t, one, "merge", "-q", "--squash", "done")
	tr.Git(t, one, "commit", "-q", "-m", "squash done")
	tr.Git(t, one, "switch", "-q", "-c", "wip")
	tr.Commit(t, one, "w.txt", "w\n", "wip work")
	tr.Git(t, one, "switch", "-q", "main")
	tr.Git(t, one, "worktree", "add", "-q", "-b", "clean-wt", filepath.Join(wts, "clean"))
	tr.Git(t, one, "worktree", "add", "-q", "-b", "dirty-wt", filepath.Join(wts, "dirty"))
	tr.Write(t, filepath.Join(wts, "dirty"), "new.txt", "x\n")
	tr.Idle(t, filepath.Join(wts, "clean"))
	tr.Idle(t, filepath.Join(wts, "dirty"))
	two := filepath.Join(root, "two")
	tr.Init(t, two)
	tr.Git(t, two, "branch", "same")

	var text, js, errb bytes.Buffer
	if code := run(context.Background(), []string{root}, &text, &errb); code != 0 {
		t.Fatalf("文字报告退出码 %d：%s%s", code, text.String(), errb.String())
	}
	if code := run(context.Background(), []string{"--json", root}, &js, &errb); code != 0 {
		t.Fatalf("--json 退出码 %d：%s%s", code, js.String(), errb.String())
	}
	var doc jsonOut
	if err := json.Unmarshal(js.Bytes(), &doc); err != nil {
		t.Fatalf("--json 输出不是合法 JSON：%v\n%s", err, js.String())
	}
	if doc.Version != 1 || doc.Summary.Repos != 2 || len(doc.Repos) != 2 {
		t.Fatalf("版本或仓库数不对：%+v", doc)
	}

	want := textClasses(t, text.String())
	got := map[string]string{}
	counts := map[string]int{}
	var free int64
	for i, r := range doc.Repos {
		if at := strings.Index(text.String(), "\n"+r.Root+"  "); at < 0 || i > 0 && at < strings.Index(text.String(), "\n"+doc.Repos[i-1].Root+"  ") {
			t.Fatalf("仓库顺序应和文字报告一样：%s\n%s", r.Root, text.String())
		}
		if r.Health == nil || r.Health.Backup.Present {
			t.Fatalf("%s 应带体检、没有 backup：%+v", r.Root, r.Health)
		}
		for _, it := range append(append([]jsonItem(nil), r.Worktrees...), r.Branches...) {
			if it.Repo != r.Root || it.LastActive == nil || it.LastActive.IsZero() {
				t.Fatalf("每项应带所在仓库和最后活动时间：%+v", it)
			}
			label := map[string]string{"removable": "可删", "keep": "留着"}[it.Verdict]
			if label == "" {
				t.Fatalf("结论只能是 removable 或 keep：%+v", it)
			}
			if label == "可删" && it.Evidence == "" || label == "留着" && len(it.Reasons) == 0 {
				t.Fatalf("可删要有证据、留着要有原因：%+v", it)
			}
			name := it.Name
			if it.Kind == "worktree" {
				if it.SizeBytes == nil {
					t.Fatalf("worktree 应带大小：%+v", it)
				}
				if rel, err := filepath.Rel(r.Root, name); err == nil && !strings.HasPrefix(rel, "..") {
					name = rel
				}
			}
			got[r.Root+"|"+it.Kind+"|"+name] = label
			counts[it.Kind+label]++
			if it.Unmerged {
				counts["unmerged"]++
			}
			if it.Kind == "worktree" && label == "可删" {
				free += *it.SizeBytes
			}
		}
	}
	if len(want) != 7 || !equalMaps(got, want) {
		t.Fatalf("JSON 和文字报告的分类对不上：\nJSON：%v\n文字：%v\n%s", sorted(got), sorted(want), text.String())
	}
	s := doc.Summary
	if s.RemovableWorktrees != counts["worktree可删"] || s.KeepWorktrees != counts["worktree留着"] ||
		s.RemovableBranches != counts["branch可删"] || s.KeepBranches != counts["branch留着"] {
		t.Fatalf("summary 和逐项计数对不上：%+v %v", s, counts)
	}
	if s.RemovableWorktrees != 1 || s.KeepWorktrees != 1 || s.RemovableBranches != 3 || s.KeepBranches != 2 ||
		s.Unmerged != counts["unmerged"] || s.Unmerged != 2 || s.FreeBytes != free || free == 0 {
		t.Fatalf("总览数不对：%+v %v free=%d\n%s", s, counts, free, text.String())
	}
	if doc.Repos[0].Mess != 5 || doc.Repos[1].Mess != 1 {
		t.Fatalf("乱度应为 one=5、two=1，实际 %d、%d", doc.Repos[0].Mess, doc.Repos[1].Mess)
	}
}

// TestApplySelectJSON：界面把 --json 里勾选的项原样送回 --select，只删这些；
// 状态变了的、分支还检出在没勾选的 worktree 里的不删，结果以 JSON 逐项给出。
func TestApplySelectJSON(t *testing.T) {
	tr.Isolate(t)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(base, "projects", "one")
	tr.Init(t, repo)
	for _, b := range []string{"a", "b", "c", "untouched"} {
		tr.Git(t, repo, "branch", b)
	}
	wt := filepath.Join(base, "wt-c")
	tr.Git(t, repo, "worktree", "add", "-q", wt, "c")
	tr.Idle(t, wt)

	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"--json", repo}, &out, &errb); code != 0 {
		t.Fatalf("--json 退出码 %d：%s", code, errb.String())
	}
	var doc struct {
		Repos []struct {
			Worktrees []apply.Want `json:"worktrees"`
			Branches  []apply.Want `json:"branches"`
		} `json:"repos"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	pick := func(list []apply.Want, name string) apply.Want {
		for _, w := range list {
			if w.Name == name {
				return w
			}
		}
		t.Fatalf("JSON 里没有 %s：%s", name, out.String())
		return apply.Want{}
	}
	r := doc.Repos[0]
	a, b, c := pick(r.Branches, "a"), pick(r.Branches, "b"), pick(r.Branches, "c")
	tr.Commit(t, repo, "x.txt", "x\n", "main moves on")
	tr.Git(t, repo, "branch", "-f", "a", "main") // a 勾选之后变了

	sel := func(items ...apply.Want) string {
		p := filepath.Join(t.TempDir(), "select.json")
		data, _ := json.Marshal(map[string]any{"items": items})
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	type res struct {
		Log     string `json:"log"`
		Target  int    `json:"target"`
		Deleted int    `json:"deleted"`
		Results []struct {
			Kind, Name, Message string
			Done                bool
		} `json:"results"`
	}
	applyJSON := func(path string) (int, res) {
		out.Reset()
		errb.Reset()
		code := run(context.Background(), []string{"--apply", "--json", "--select", path}, &out, &errb)
		var got res
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatalf("--apply --json 输出不是 JSON：%v\n%s%s", err, out.String(), errb.String())
		}
		return code, got
	}

	code, got := applyJSON(sel(a, b, c))
	if code != 1 || got.Target != 3 || got.Deleted != 1 || got.Log == "" {
		t.Fatalf("应只删 b、退出 1：%d %+v", code, got)
	}
	msgs := map[string]string{}
	for _, x := range got.Results {
		msgs[x.Name] = fmt.Sprint(x.Done, " ", x.Message)
	}
	if !strings.HasPrefix(msgs["b"], "true") || !strings.Contains(msgs["a"], "提交和勾选时不一样了") ||
		!strings.Contains(msgs["c"], "它没一起勾选") {
		t.Fatalf("逐项结果不对：%v", msgs)
	}
	if bs := branches(t, repo); bs != "a\nc\nmain\nuntouched" {
		t.Fatalf("应只删掉 b，实际：%q", bs)
	}

	code, got = applyJSON(sel(pick(r.Worktrees, wt), c))
	if code != 0 || got.Deleted != 2 {
		t.Fatalf("worktree 和它检出的分支一起勾选应都删掉：%d %+v", code, got)
	}
	if bs := branches(t, repo); bs != "a\nmain\nuntouched" {
		t.Fatalf("应删掉 c，实际：%q", bs)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree 目录应已删掉：%v", err)
	}
}

func TestSelectNeedsApply(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"--select", "-", t.TempDir()}, &out, &errb); code != 2 || out.Len() != 0 {
		t.Fatalf("--select 不带 --apply 应退出 2、不输出：%d %q %q", code, out.String(), errb.String())
	}
}

func equalMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func sorted(m map[string]string) []string {
	var out []string
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// 刚建的 worktree 默认按「刚动过」留着，--min-idle 0 关掉这条后可删；参数不对报参数错。
func TestMinIdleFlag(t *testing.T) {
	tr.Isolate(t)
	repo := filepath.Join(t.TempDir(), "one")
	tr.Init(t, repo)
	wt := filepath.Join(t.TempDir(), "fresh")
	tr.Git(t, repo, "worktree", "add", "-q", "-b", "fresh", wt)

	verdict := func(args ...string) jsonItem {
		t.Helper()
		var out, errb bytes.Buffer
		if code := run(context.Background(), append(args, "--json", repo), &out, &errb); code != 0 {
			t.Fatalf("%v 退出码 %d：%s", args, code, errb.String())
		}
		var doc jsonOut
		if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		return doc.Repos[0].Worktrees[0]
	}
	if it := verdict(); it.Verdict != "keep" || !strings.Contains(strings.Join(it.Reasons, "|"), "分钟前刚动过，可能还在用") {
		t.Fatalf("默认应按刚动过留着：%+v", it)
	}
	if it := verdict("--min-idle", "0"); it.Verdict != "removable" {
		t.Fatalf("--min-idle 0 应可删：%+v", it)
	}
	for _, bad := range []string{"-5m", "abc"} {
		var out, errb bytes.Buffer
		if code := run(context.Background(), []string{"--min-idle", bad, repo}, &out, &errb); code != 2 {
			t.Fatalf("--min-idle %s 应是参数错（2），实际 %d", bad, code)
		}
	}
}
