package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dodo258/repo-tidy/internal/apply"
	tr "github.com/dodo258/repo-tidy/internal/testrepo"
)

// scanJSON 跑一次 --json 扫描，返回扫到的仓库（按路径排序）和 summary.excluded、summary.repos。
func scanJSON(t *testing.T, args ...string) (roots []string, excluded, repos int) {
	t.Helper()
	out := runOK(t, append([]string{"--json"}, args...)...)
	var doc struct {
		Summary struct {
			Repos    int  `json:"repos"`
			Excluded *int `json:"excluded"`
		} `json:"summary"`
		Repos []struct {
			Root string `json:"root"`
		} `json:"repos"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("%v 输出不是 JSON：%v\n%s", args, err, out)
	}
	if doc.Summary.Excluded == nil {
		t.Fatalf("summary 里没有 excluded：%s", out)
	}
	for _, r := range doc.Repos {
		roots = append(roots, filepath.Base(r.Root))
	}
	slices.Sort(roots)
	return roots, *doc.Summary.Excluded, doc.Summary.Repos
}

// 一个目录下的几个仓库：重复传 --exclude、环境变量按冒号拆、两者并用、重复和重叠的路径、
// 同前缀的邻居、嵌套目录里的仓库、相对路径和 ~/。
func TestExcludePaths(t *testing.T) {
	tr.Isolate(t)
	t.Setenv("REPO_TIDY_EXCLUDE", "")
	root, _ := filepath.EvalSymlinks(t.TempDir())
	for _, n := range []string{"app", "app2", "app-x", "lib", filepath.Join("org", "b"), filepath.Join("org", "team", "c")} {
		tr.Init(t, filepath.Join(root, n))
	}
	p := func(n string) string { return filepath.Join(root, n) }

	for _, c := range []struct {
		name     string
		env      string
		args     []string
		want     []string
		excluded int
	}{
		{"不排除", "", nil, []string{"app", "app-x", "app2", "b", "c", "lib"}, 0},
		{"重复的 flag", "", []string{"--exclude", p("app"), "--exclude", p("lib")}, []string{"app-x", "app2", "b", "c"}, 2},
		{"环境变量按冒号拆，空段忽略", ":" + p("app") + "::" + p("lib") + ":", nil, []string{"app-x", "app2", "b", "c"}, 2},
		{"flag 和环境变量取并集", p("app"), []string{"--exclude", p("lib")}, []string{"app-x", "app2", "b", "c"}, 2},
		{"重复的路径只计一次", p("app"), []string{"--exclude", p("app"), "--exclude", p("app") + "/"}, []string{"app-x", "app2", "b", "c", "lib"}, 1},
		{"重叠的路径只计一次", "", []string{"--exclude", p("org"), "--exclude", p("org/team"), "--exclude", p("org/team/c")}, []string{"app", "app-x", "app2", "lib"}, 2},
		{"嵌套目录：只排除下层", "", []string{"--exclude", p("org/team")}, []string{"app", "app-x", "app2", "b", "lib"}, 1},
		{"同前缀的邻居不算", "", []string{"--exclude", p("app"), "--exclude", p("or")}, []string{"app-x", "app2", "b", "c", "lib"}, 1},
		{"不存在的路径不报错", "", []string{"--exclude", p("nope")}, []string{"app", "app-x", "app2", "b", "c", "lib"}, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("REPO_TIDY_EXCLUDE", c.env)
			got, excluded, repos := scanJSON(t, append(c.args, "--depth", "3", root)...)
			if !slices.Equal(got, c.want) || excluded != c.excluded || repos != len(c.want) {
				t.Fatalf("扫到 %v（repos %d），跳过 %d；应扫 %v，跳过 %d", got, repos, excluded, c.want, c.excluded)
			}
		})
	}

	t.Run("相对路径和 ~/", func(t *testing.T) {
		t.Setenv("HOME", root)
		t.Chdir(p("org"))
		got, excluded, _ := scanJSON(t, "--exclude", "team", "--exclude", "~/app", "--depth", "3", root)
		if want := []string{"app-x", "app2", "b", "lib"}; !slices.Equal(got, want) || excluded != 2 {
			t.Fatalf("扫到 %v，跳过 %d", got, excluded)
		}
	})

	t.Run("显式给的路径也排除", func(t *testing.T) {
		got, excluded, _ := scanJSON(t, "--exclude", p("app"), p("app"), p("lib"))
		if !slices.Equal(got, []string{"lib"}) || excluded != 1 {
			t.Fatalf("扫到 %v，跳过 %d", got, excluded)
		}
	})
}

// 仓库里嵌着另一个仓库（显式给出两个路径）：排除里面那个不影响外面那个，排除外面那个连里面一起跳过。
func TestExcludeNestedRepo(t *testing.T) {
	tr.Isolate(t)
	t.Setenv("REPO_TIDY_EXCLUDE", "")
	root, _ := filepath.EvalSymlinks(t.TempDir())
	outer, inner := filepath.Join(root, "outer"), filepath.Join(root, "outer", "inner")
	tr.Init(t, outer)
	tr.Init(t, inner)
	if got, n, _ := scanJSON(t, "--exclude", inner, outer, inner); !slices.Equal(got, []string{"outer"}) || n != 1 {
		t.Fatalf("排除 inner：扫到 %v，跳过 %d", got, n)
	}
	if got, n, _ := scanJSON(t, "--exclude", outer, outer, inner); len(got) != 0 || n != 2 {
		t.Fatalf("排除 outer：扫到 %v，跳过 %d", got, n)
	}
}

// 附加 worktree 是仓库的别名：从主工作区或 worktree 哪个路径排除，整个仓库都跳过，只计一次；
// worktree 放在扫描目录外面也一样。
func TestExcludeWorktreeAlias(t *testing.T) {
	tr.Isolate(t)
	t.Setenv("REPO_TIDY_EXCLUDE", "")
	root, _ := filepath.EvalSymlinks(t.TempDir())
	outside, _ := filepath.EvalSymlinks(t.TempDir())
	a, wt, far := filepath.Join(root, "a"), filepath.Join(root, "a-wt"), filepath.Join(outside, "far")
	tr.Init(t, a)
	tr.Git(t, a, "worktree", "add", "-q", "-b", "wt", wt)
	tr.Git(t, a, "worktree", "add", "-q", "-b", "far", far)
	tr.Idle(t, wt)
	tr.Idle(t, far)
	tr.Init(t, filepath.Join(root, "b"))

	if got, n, _ := scanJSON(t, root); !slices.Equal(got, []string{"a", "b"}) || n != 0 {
		t.Fatalf("不排除：扫到 %v，跳过 %d", got, n)
	}
	for _, ex := range []string{a, wt, outside} {
		if got, n, _ := scanJSON(t, "--exclude", ex, root); !slices.Equal(got, []string{"b"}) || n != 1 {
			t.Fatalf("排除 %s：扫到 %v，跳过 %d", ex, got, n)
		}
	}
	// 只给 worktree 的路径扫，排除主工作区
	if got, n, _ := scanJSON(t, "--exclude", a, wt); len(got) != 0 || n != 1 {
		t.Fatalf("从别名进来：扫到 %v，跳过 %d", got, n)
	}
}

// 全部排除：成功退出，零个仓库，总览注明跳过几个（中英文）。
func TestExcludeAll(t *testing.T) {
	tr.Isolate(t)
	t.Setenv("REPO_TIDY_EXCLUDE", "")
	root, _ := filepath.EvalSymlinks(t.TempDir())
	tr.Init(t, filepath.Join(root, "a"))
	tr.Init(t, filepath.Join(root, "b"))
	if got, n, repos := scanJSON(t, "--exclude", root, root); len(got) != 0 || n != 2 || repos != 0 {
		t.Fatalf("扫到 %v（repos %d），跳过 %d", got, repos, n)
	}
	zh := runOK(t, "--lang", "zh", "--exclude", root, root)
	en := runOK(t, "--lang", "en", "--exclude", root, root)
	if !strings.Contains(zh, "扫了 0 个仓库") || !strings.Contains(zh, "跳过 2 个仓库（--exclude）") {
		t.Fatalf("中文总览不对：\n%s", zh)
	}
	if !strings.Contains(en, "scanned 0 repos") || !strings.Contains(en, "skipped 2 repos (--exclude)") {
		t.Fatalf("英文总览不对：\n%s", en)
	}
	if one := runOK(t, "--lang", "en", "--exclude", filepath.Join(root, "a"), root); !strings.Contains(one, "skipped 1 repo (--exclude)") {
		t.Fatalf("英文单数不对：\n%s", one)
	}
	if plain := runOK(t, root); strings.Contains(plain, "--exclude") {
		t.Fatalf("没排除时不该有跳过那一行：\n%s", plain)
	}
}

// --apply --json --select：被排除仓库里勾选的分支不删也不列，没排除的照删；--apply 不带 --select 也一样。
func TestExcludeApply(t *testing.T) {
	tr.Isolate(t)
	t.Setenv("REPO_TIDY_EXCLUDE", "")
	root, _ := filepath.EvalSymlinks(t.TempDir())
	keep, del := filepath.Join(root, "keep"), filepath.Join(root, "del")
	for _, r := range []string{keep, del} {
		tr.Init(t, r)
		tr.Git(t, r, "branch", "done")
	}
	var scan struct {
		Repos []struct {
			Branches []apply.Want `json:"branches"`
		} `json:"repos"`
	}
	if err := json.Unmarshal([]byte(runOK(t, "--json", root)), &scan); err != nil {
		t.Fatal(err)
	}
	var items []apply.Want
	for _, r := range scan.Repos {
		items = append(items, r.Branches...)
	}
	if len(items) != 2 {
		t.Fatalf("应各有一个可删分支：%+v", items)
	}
	sel := filepath.Join(t.TempDir(), "select.json")
	data, _ := json.Marshal(map[string]any{"items": items})
	if err := os.WriteFile(sel, data, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("REPO_TIDY_EXCLUDE", keep)
	var out, errb bytes.Buffer
	code := run(context.Background(), []string{"--apply", "--json", "--select", sel}, &out, &errb)
	var res struct {
		Target   int  `json:"target"`
		Deleted  int  `json:"deleted"`
		Excluded *int `json:"excluded"`
		Results  []struct {
			Repo string `json:"repo"`
			Done bool   `json:"done"`
		} `json:"results"`
	}
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("输出不是 JSON：%v\n%s%s", err, out.String(), errb.String())
	}
	if code != 0 || res.Target != 1 || res.Deleted != 1 || res.Excluded == nil || *res.Excluded != 1 ||
		len(res.Results) != 1 || res.Results[0].Repo != del || !res.Results[0].Done {
		t.Fatalf("退出码 %d，结果 %+v：%s%s", code, res, out.String(), errb.String())
	}
	if bs := branches(t, keep); bs != "done\nmain" {
		t.Fatalf("被排除仓库的分支应保留：%q", bs)
	}
	if bs := branches(t, del); bs != "main" {
		t.Fatalf("没排除的仓库应删掉 done：%q", bs)
	}

	// 不带 --select 的 --apply：被排除的仓库也不动
	tr.Git(t, del, "branch", "done2")
	t.Setenv("REPO_TIDY_EXCLUDE", "")
	text := runOK(t, "--lang", "zh", "--apply", "--exclude", keep, root)
	if strings.Contains(text, keep) || !strings.Contains(text, "跳过 1 个仓库（--exclude）") {
		t.Fatalf("文字报告不该列出被排除的仓库：\n%s", text)
	}
	if bs := branches(t, keep); bs != "done\nmain" {
		t.Fatalf("被排除仓库的分支应保留：%q", bs)
	}
	if bs := branches(t, del); bs != "main" {
		t.Fatalf("没排除的仓库应删掉 done2：%q", bs)
	}
}

func TestExcludeArgs(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--lang", "zh", "--exclude", ""}, "--exclude 不能是空的"},
		{[]string{"--lang", "zh", "--undo", "--exclude", "/x"}, "--undo 不能和 --exclude 一起用"},
	} {
		var out, errb bytes.Buffer
		if code := run(context.Background(), c.args, &out, &errb); code != 2 || !strings.Contains(errb.String(), c.want) {
			t.Errorf("%v：退出码 %d，%q", c.args, code, errb.String())
		}
	}
}
