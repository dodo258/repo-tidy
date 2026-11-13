package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tr "github.com/dodo258/repo-tidy/internal/testrepo"
)

func runCode(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(context.Background(), args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestUndoLatestLog(t *testing.T) {
	tr.Isolate(t)
	root, err := filepath.EvalSymlinks(t.TempDir()) // macOS 的临时目录在 /var → /private/var 下
	if err != nil {
		t.Fatal(err)
	}
	one := filepath.Join(root, "one")
	tr.Init(t, one)
	tr.Git(t, one, "branch", "done")
	tip := tr.Git(t, one, "rev-parse", "main")
	wt := filepath.Join(root, "wt")
	tr.Git(t, one, "worktree", "add", "-q", "-b", "feat", wt)

	if code, _, errs := runCode(t, "--undo"); code != 1 || !strings.Contains(errs, "里没有恢复日志") {
		t.Fatalf("还没删过东西时应说没有日志：%d %s", code, errs)
	}
	if code, out, errs := runCode(t, "--apply", "--min-idle", "0", one); code != 0 {
		t.Fatalf("--apply 退出码 %d：%s%s", code, out, errs)
	}
	if b := branches(t, one); b != "main" {
		t.Fatalf("应只剩 main：%q", b)
	}

	// 只看：列出要恢复的，什么都不改
	before := tr.Snapshot(t, root)
	code, out, errs := runCode(t, "--undo")
	if code != 0 {
		t.Fatalf("--undo 退出码 %d：%s%s", code, out, errs)
	}
	for _, want := range []string{"只看，什么都没改", "会恢复  分支 done：会建回分支，指向 " + tip[:8], "会恢复  worktree " + wt, "能恢复 3 项；加 --apply 才执行。"} {
		if !strings.Contains(out, want) {
			t.Fatalf("撤销预览缺 %q：\n%s", want, out)
		}
	}
	if d := tr.LineDiff(before, tr.Snapshot(t, root)); d != "" {
		t.Fatalf("只看模式改了文件：\n%s", d)
	}

	code, out, errs = runCode(t, "--undo", "--apply")
	if code != 0 || !strings.Contains(out, "恢复了 3 项。") {
		t.Fatalf("--undo --apply 退出码 %d：%s%s", code, out, errs)
	}
	if b := branches(t, one); b != "done\nfeat\nmain" {
		t.Fatalf("分支应都回来：%q", b)
	}
	if tr.Git(t, one, "rev-parse", "done") != tip || tr.Git(t, wt, "rev-parse", "--abbrev-ref", "HEAD") != "feat" {
		t.Fatal("分支应指向原提交，worktree 应在原路径检出原分支")
	}
}

func TestUndoJSONAndExplicitLog(t *testing.T) {
	tr.Isolate(t)
	root := t.TempDir()
	one := filepath.Join(root, "one")
	tr.Init(t, one)
	tr.Git(t, one, "branch", "a")
	tr.Git(t, one, "branch", "b")
	if code, out, errs := runCode(t, "--apply", "--json", one); code != 0 {
		t.Fatalf("--apply 退出码 %d：%s%s", code, out, errs)
	} else {
		var doc struct{ Log string }
		if err := json.Unmarshal([]byte(out), &doc); err != nil || doc.Log == "" {
			t.Fatalf("删除结果应带日志路径：%v\n%s", err, out)
		}
		// 之后又有一份更新的日志：给了路径就用给的那份
		newer := filepath.Join(filepath.Dir(doc.Log), "apply-99991231-235959.log")
		if err := os.WriteFile(newer, []byte("  恢复：git -C '/nowhere' branch 'z' "+strings.Repeat("a", 40)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		tr.Git(t, one, "branch", "b", "main~0")

		code, out, errs := runCode(t, "--undo", doc.Log, "--apply", "--json", "--lang", "en")
		if code != 0 {
			t.Fatalf("退出码 %d：%s%s", code, out, errs)
		}
		var res struct {
			Log      string
			Apply    bool
			Restored int
			Results  []struct{ Repo, Kind, Name, Head, Branch, Status, Message string }
		}
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("JSON：%v\n%s", err, out)
		}
		if res.Log != doc.Log || !res.Apply || res.Restored != 1 || len(res.Results) != 2 {
			t.Fatalf("撤销结果不对：%+v", res)
		}
		st := map[string]string{}
		for _, r := range res.Results {
			st[r.Name] = r.Status
			noChinese(t, "message", r.Message)
		}
		if st["a"] != "restored" || st["b"] != "exists" {
			t.Fatalf("a 应恢复、b 已在应跳过：%+v", res.Results)
		}

		// 不给路径就用最新的那份：仓库不在了，报失败、退出码 1
		code, out, _ = runCode(t, "--undo", "--lang", "en")
		if code != 1 || !strings.Contains(out, newer) || !strings.Contains(out, "failed") {
			t.Fatalf("应用最新日志并报仓库不在：%d\n%s", code, out)
		}
	}
}

func TestUndoArgs(t *testing.T) {
	tr.Isolate(t)
	if code, _, errs := runCode(t, "--undo", "a.log", "b.log"); code != 2 || !strings.Contains(errs, "只认一个日志路径") {
		t.Fatalf("两个日志应报参数错：%d %s", code, errs)
	}
	if code, _, errs := runCode(t, "--undo", "--apply", "--select", "x.json"); code != 2 || !strings.Contains(errs, "--select") {
		t.Fatalf("--undo 和 --select 一起用应报参数错：%d %s", code, errs)
	}
	bad := filepath.Join(t.TempDir(), "x.log")
	if err := os.WriteFile(bad, []byte("not a log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errs := runCode(t, "--undo", bad); code != 1 || !strings.Contains(errs, "没有恢复命令") {
		t.Fatalf("读不出恢复命令应报错：%d %s", code, errs)
	}
}
