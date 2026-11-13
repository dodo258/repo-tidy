package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dodo258/repo-tidy/internal/msg"
	tr "github.com/dodo258/repo-tidy/internal/testrepo"
)

// 两个仓库，其中 one 有一个放在扫描目录里的兄弟 worktree（目录里也有 .git）。
func progressRepos(t *testing.T) string {
	t.Helper()
	tr.Isolate(t)
	root := t.TempDir()
	one := filepath.Join(root, "one")
	tr.Init(t, one)
	tr.Git(t, one, "worktree", "add", "-q", filepath.Join(root, "one-side"), "-b", "side")
	tr.Init(t, filepath.Join(root, "two"))
	return root
}

func TestProgressOnlyOnStderr(t *testing.T) {
	root := progressRepos(t)
	for _, args := range [][]string{{"--progress"}, {"--progress", "--json"}} {
		var out, errb bytes.Buffer
		if code := run(context.Background(), append(args, root), &out, &errb); code != 0 {
			t.Fatalf("%v 退出码 %d：%s", args, code, errb.String())
		}
		if strings.Contains(out.String(), "正在扫") {
			t.Fatalf("%v：stdout 混进了进度：\n%s", args, out.String())
		}
		// 兄弟 worktree 和 one 是同一个仓库，只扫一次：一共 2 个
		lines := strings.Split(strings.TrimSpace(errb.String()), "\n")
		if len(lines) != 2 || !strings.HasPrefix(lines[0], "正在扫 1/2 ") || !strings.HasPrefix(lines[1], "正在扫 2/2 ") {
			t.Fatalf("%v：stderr 应是两行进度，实际：%q", args, errb.String())
		}
		names := lines[0][len("正在扫 1/2 "):] + " " + lines[1][len("正在扫 2/2 "):]
		if !strings.Contains(names, "one") || !strings.Contains(names, "two") {
			t.Fatalf("进度里的仓库名不对：%q", errb.String())
		}
		if args[len(args)-1] == "--json" {
			var doc struct {
				Summary struct{ Repos int } `json:"summary"`
			}
			if err := json.Unmarshal(out.Bytes(), &doc); err != nil || doc.Summary.Repos != 2 {
				t.Fatalf("--json 输出不对（%v）：%s", err, out.String())
			}
		}
	}
}

// 不加 --progress、stderr 不是终端（被重定向）时什么都不写。
func TestNoProgressWhenNotTerminal(t *testing.T) {
	root := progressRepos(t)
	for _, args := range [][]string{nil, {"--json"}} {
		var out, errb bytes.Buffer
		if code := run(context.Background(), append(args, root), &out, &errb); code != 0 {
			t.Fatalf("%v 退出码 %d：%s", args, code, errb.String())
		}
		if errb.Len() != 0 {
			t.Fatalf("%v：stderr 应为空，实际：%q", args, errb.String())
		}
		if strings.Contains(out.String(), "正在扫") {
			t.Fatalf("%v：stdout 混进了进度", args)
		}
	}
}

func TestProgressInPlace(t *testing.T) {
	var b bytes.Buffer
	p := &progressOut{w: &b, format: msg.For(msg.ZH).Scanning, inPlace: true}
	p.show(3, 13, "/x/repo-name")
	p.done()
	if got := b.String(); got != "\r\033[K正在扫 3/13 repo-name\r\033[K" {
		t.Fatalf("终端进度 = %q", got)
	}
	if newProgress(&b, msg.For(msg.ZH), false, true) != nil || newProgress(&b, msg.For(msg.ZH), false, false) != nil {
		t.Fatal("不是终端时不该显示进度")
	}
}

// --depth 决定往下找几层仓库；0、超过 4、不是数字都是参数错。
func TestDepthFlag(t *testing.T) {
	tr.Isolate(t)
	root := t.TempDir()
	tr.Init(t, filepath.Join(root, "top"))
	tr.Init(t, filepath.Join(root, "org", "two"))
	tr.Init(t, filepath.Join(root, "org", "team", "three"))
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{nil, []string{"top"}},
		{[]string{"--depth", "1"}, []string{"top"}},
		{[]string{"--depth", "2"}, []string{"top", "two"}},
		{[]string{"--depth=3"}, []string{"top", "two", "three"}},
	} {
		var out, errb bytes.Buffer
		if code := run(context.Background(), append(append([]string{"--json"}, tc.args...), root), &out, &errb); code != 0 {
			t.Fatalf("%v 退出码 %d：%s", tc.args, code, errb.String())
		}
		var doc struct {
			Repos []struct{ Root string } `json:"repos"`
		}
		if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, r := range doc.Repos {
			got = append(got, filepath.Base(r.Root))
		}
		slices.Sort(got)
		want := slices.Clone(tc.want)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%v：扫到 %v，应为 %v", tc.args, got, want)
		}
	}
	for _, bad := range []string{"0", "5", "-1", "two"} {
		var out, errb bytes.Buffer
		if code := run(context.Background(), []string{"--depth", bad, root}, &out, &errb); code != 2 {
			t.Errorf("--depth %s 退出码应为 2，实际 %d", bad, code)
		}
		if out.Len() != 0 || !strings.Contains(errb.String(), "--depth") && bad != "two" {
			t.Errorf("--depth %s：stdout %q stderr %q", bad, out.String(), errb.String())
		}
	}
	var errb bytes.Buffer
	run(context.Background(), []string{"--lang", "en", "--depth", "9", root}, &bytes.Buffer{}, &errb)
	if want := "--depth must be between 1 and 4, got 9"; !strings.Contains(errb.String(), want) {
		t.Errorf("英文报错应含 %q，实际 %q", want, errb.String())
	}
}
