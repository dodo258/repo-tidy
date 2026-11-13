package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode"

	tr "github.com/dodo258/repo-tidy/internal/testrepo"
)

// langFixture 在临时目录造一组状态各异的仓库，让报告里尽量多的说法都出现：
// squash/快进/rebase 合并、已推到 backup、未合并、干净/有改动/刚动过/锁定/目录已不在的 worktree、
// 上游领先、stash、backup 有分支没推、可删超过 10 项的仓库。提交标题都用英文，英文报告里才不会混进中文。
func langFixture(t *testing.T) string {
	t.Helper()
	tr.Isolate(t)
	root := t.TempDir()
	wts := t.TempDir()

	one := filepath.Join(root, "one")
	tr.Init(t, one)
	remote := filepath.Join(t.TempDir(), "backup.git")
	tr.Git(t, root, "init", "-q", "--bare", remote)
	tr.Git(t, one, "remote", "add", "backup", remote)
	tr.Git(t, one, "push", "-q", "-u", "backup", "main")
	tr.Commit(t, one, "ahead.txt", "a\n", "ahead of backup")
	tr.Git(t, one, "switch", "-q", "-c", "done")
	tr.Commit(t, one, "d.txt", "d\n", "done work")
	tr.Git(t, one, "switch", "-q", "main")
	tr.Git(t, one, "merge", "-q", "--squash", "done")
	tr.Git(t, one, "commit", "-q", "-m", "squash done")
	tr.Git(t, one, "switch", "-q", "-c", "wip")
	tr.Commit(t, one, "w.txt", "w\n", "wip work")
	tr.Commit(t, one, "w2.txt", "w\n", "more wip")
	tr.Git(t, one, "switch", "-q", "-c", "shared", "main")
	tr.Commit(t, one, "s.txt", "s\n", "shared work")
	tr.Git(t, one, "push", "-q", "backup", "shared")
	tr.Git(t, one, "switch", "-q", "main")
	tr.Git(t, one, "worktree", "add", "-q", "-b", "clean-wt", filepath.Join(wts, "clean"))
	tr.Git(t, one, "worktree", "add", "-q", "-b", "dirty-wt", filepath.Join(wts, "dirty"))
	tr.Write(t, filepath.Join(wts, "dirty"), "new.txt", "x\n")
	tr.Git(t, one, "worktree", "add", "-q", "-b", "locked-wt", filepath.Join(wts, "locked"))
	tr.Git(t, one, "worktree", "lock", "--reason", "agent at work", filepath.Join(wts, "locked"))
	tr.Git(t, one, "worktree", "add", "-q", "--detach", filepath.Join(wts, "gone"))
	if err := os.RemoveAll(filepath.Join(wts, "gone")); err != nil {
		t.Fatal(err)
	}
	tr.Git(t, one, "worktree", "add", "-q", "-b", "busy-wt", filepath.Join(wts, "busy")) // 刚动过
	for _, w := range []string{"clean", "dirty", "locked"} {
		tr.Idle(t, filepath.Join(wts, w))
	}
	tr.Write(t, one, "scratch.txt", "s\n")
	tr.Git(t, one, "add", "scratch.txt")
	tr.Git(t, one, "stash", "-q")
	tr.Write(t, one, "untracked.txt", "u\n")

	two := filepath.Join(root, "two")
	tr.Init(t, two)
	tr.Git(t, two, "branch", "same")
	for i := range 11 {
		b := fmt.Sprintf("feature-%02d", i)
		tr.Git(t, two, "switch", "-q", "-c", b)
		tr.Commit(t, two, b+".txt", b+"\n", "add "+b)
		tr.Git(t, two, "switch", "-q", "main")
		tr.Git(t, two, "merge", "-q", "--no-ff", "-m", "merge "+b, b)
	}
	tr.Git(t, two, "switch", "-q", "-c", "rebased")
	tr.Commit(t, two, "r.txt", "r\n", "rebased work")
	tr.Commit(t, two, "r2.txt", "r\n", "more rebased work")
	tr.Git(t, two, "switch", "-q", "main")
	tr.Commit(t, two, "m.txt", "m\n", "main moves on")
	tr.Git(t, two, "cherry-pick", "main..rebased")

	three := filepath.Join(root, "three")
	tr.Init(t, three)
	return root
}

func runOut(t *testing.T, args ...string) string {
	t.Helper()
	var out, errb bytes.Buffer
	if code := run(context.Background(), args, &out, &errb); code != 0 {
		t.Fatalf("%v 退出码 %d：%s%s", args, code, out.String(), errb.String())
	}
	return out.String()
}

// noChinese 报出 s 里的中文字符和全角标点。
func noChinese(t *testing.T, what, s string) {
	t.Helper()
	for i, l := range strings.Split(s, "\n") {
		for _, r := range l {
			if unicode.Is(unicode.Han, r) || (r >= 0x3000 && r <= 0x303F) || (r >= 0xFF00 && r <= 0xFFEF) {
				t.Fatalf("%s 第 %d 行有中文字符 %q：%s\n%s", what, i+1, r, l, s)
			}
		}
	}
}

var digitsRe = regexp.MustCompile(`\d+`)

func TestEnglishReportMatchesChinese(t *testing.T) {
	root := langFixture(t)
	zh := runOut(t, "--lang", "zh", root)
	en := runOut(t, "--lang", "en", root)
	zhV := runOut(t, "--lang", "zh", "--verbose", root)
	enV := runOut(t, "--lang", "en", "--verbose", root)
	noChinese(t, "英文报告", en)
	noChinese(t, "英文 --verbose 报告", enV)

	for _, c := range []struct {
		name, zh, en string
	}{{"默认", zh, en}, {"--verbose", zhV, enV}} {
		// 第一行有扫描时间（分钟），两次运行可能跨分钟，不比
		z := strings.SplitN(c.zh, "\n", 2)[1]
		e := strings.SplitN(c.en, "\n", 2)[1]
		zl, el := strings.Split(z, "\n"), strings.Split(e, "\n")
		if len(zl) != len(el) {
			t.Fatalf("%s：中英文报告行数不同（%d、%d）\n%s\n----\n%s", c.name, len(zl), len(el), c.zh, c.en)
		}
		// 每一行里的数字（个数、大小、天数、提交号、路径里的数字）按顺序一致
		for i := range zl {
			if zd, ed := digitsRe.FindAllString(zl[i], -1), digitsRe.FindAllString(el[i], -1); !reflect.DeepEqual(zd, ed) {
				t.Fatalf("%s：第 %d 行数字对不上\n中：%s\n英：%s", c.name, i+2, zl[i], el[i])
			}
		}
		// 分类一致：可删、留着的行数相同
		for _, p := range []struct{ zh, en string }{{"    可删  ", "    remove  "}, {"    留着  ", "    keep    "}} {
			if zn, enN := strings.Count(z, "\n"+p.zh), strings.Count(e, "\n"+p.en); zn != enN || zn == 0 {
				t.Fatalf("%s：%q 有 %d 行、%q 有 %d 行\n%s\n----\n%s", c.name, p.zh, zn, p.en, enN, c.zh, c.en)
			}
		}
	}
	// 夹具确实覆盖到了这些说法
	for _, want := range []string{
		"Overview  scanned 3 repos (1 clean)", "removable 13 items: merged 13", "squashed into main ", "merged into main ",
		"rebased onto main ", "pushed to backup/shared", "dir gone, only clears git's record", "locked: agent at work",
		"2 commits are neither in main nor pushed to any remote", "uncommitted changes or untracked files (1): new.txt",
		"may still be in use", "[detached HEAD]", "  Checkup", "vs backup/main: 2 ahead, 0 behind", "stashes: 1",
		"not pushed  ", "\nClean repos: 1 (nothing removable, nothing unmerged):\n", "Add --apply to delete the 18 items marked removable",
	} {
		if !strings.Contains(en, want) {
			t.Errorf("英文报告缺 %q", want)
		}
	}
	if !strings.Contains(enV, "content squashed into main: commit ") || !strings.Contains(enV, `"squash done"`) {
		t.Errorf("英文 --verbose 应写全证据和提交标题：\n%s", enV)
	}
	if t.Failed() {
		t.Log(en)
	}
}

// TestJSONSameAcrossLanguages：--json 除了证据、原因、说明这些给人看的文字，其余字段两种语言完全一样。
func TestJSONSameAcrossLanguages(t *testing.T) {
	root := langFixture(t)
	var docs [2]map[string]any
	for i, l := range []string{"zh", "en"} {
		out := runOut(t, "--json", "--lang", l, root)
		if err := json.Unmarshal([]byte(out), &docs[i]); err != nil {
			t.Fatal(err)
		}
	}
	var texts [2][]string
	for i := range docs {
		texts[i] = stripText(docs[i])
		delete(docs[i], "generated_at")
	}
	if !reflect.DeepEqual(docs[0], docs[1]) {
		a, _ := json.MarshalIndent(docs[0], "", " ")
		b, _ := json.MarshalIndent(docs[1], "", " ")
		t.Fatalf("去掉文字后中英 JSON 应完全一样：\n%s", tr.LineDiff(string(a), string(b)))
	}
	if len(texts[0]) != len(texts[1]) || len(texts[1]) < 10 {
		t.Fatalf("证据和原因的条数应相同：%d、%d", len(texts[0]), len(texts[1]))
	}
	noChinese(t, "英文 JSON 的证据和原因", strings.Join(texts[1], "\n"))
	if strings.Join(texts[0], "\n") == strings.Join(texts[1], "\n") {
		t.Fatal("证据和原因应跟着语言变")
	}
}

// stripText 把给人看的文字字段（evidence、reasons、note、error、errors、online_error）从 v 里拿掉并按出现顺序返回。
func stripText(v any) []string {
	var out []string
	switch x := v.(type) {
	case map[string]any:
		for _, k := range []string{"evidence", "reasons", "note", "error", "errors", "online_error"} {
			switch t := x[k].(type) {
			case string:
				if t != "" {
					out = append(out, t)
				}
			case []any:
				for _, s := range t {
					out = append(out, s.(string))
				}
			}
			delete(x, k)
		}
		for _, k := range sortedKeys(x) {
			out = append(out, stripText(x[k])...)
		}
	case []any:
		for _, e := range x {
			out = append(out, stripText(e)...)
		}
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return sortStrings(ks)
}

func sortStrings(s []string) []string {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
	return s
}

func TestLangFromEnvAndFlag(t *testing.T) {
	tr.Isolate(t)
	root := t.TempDir()
	tr.Init(t, filepath.Join(root, "one"))
	cases := []struct {
		env  map[string]string
		args []string
		want string
	}{
		{map[string]string{"LC_ALL": "", "LC_MESSAGES": "", "LANG": "en_US.UTF-8"}, nil, "read-only report"},
		{map[string]string{"LC_ALL": "", "LC_MESSAGES": "", "LANG": ""}, nil, "read-only report"},
		{map[string]string{"LC_ALL": "", "LC_MESSAGES": "zh_CN.UTF-8", "LANG": "en_US.UTF-8"}, nil, "只读报告"},
		{map[string]string{"LC_ALL": "en_US.UTF-8", "LANG": "zh_CN.UTF-8"}, nil, "read-only report"},
		{map[string]string{"LC_ALL": "zh_CN.UTF-8"}, []string{"--lang", "en"}, "read-only report"},
		{map[string]string{"LC_ALL": "en_US.UTF-8"}, []string{"--lang=zh"}, "只读报告"},
	}
	for _, c := range cases {
		for k, v := range c.env {
			t.Setenv(k, v)
		}
		if out := runOut(t, append(c.args, root)...); !strings.Contains(out, c.want) {
			t.Errorf("环境 %v 参数 %v：应含 %q\n%s", c.env, c.args, c.want, out)
		}
	}

	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"--lang", "fr", root}, &out, &errb); code != 2 || !strings.Contains(errb.String(), "--lang 只能是 zh 或 en") {
		t.Fatalf("--lang fr 应报参数错（退出码 2）：%d %s", code, errb.String())
	}
	errb.Reset()
	if code := run(context.Background(), []string{"--lang", "en", "--help"}, &out, &errb); code != 0 || !strings.Contains(errb.String(), "Usage: repo-tidy") {
		t.Fatalf("--lang en --help 应是英文帮助：%d %s", code, errb.String())
	}
	noChinese(t, "英文帮助", errb.String())
	errb.Reset()
	t.Setenv("LC_ALL", "en_US.UTF-8")
	if code := run(context.Background(), []string{"--min-idle", "-1m", root}, &out, &errb); code != 2 || errb.String() != "repo-tidy: --min-idle can't be negative\n" {
		t.Fatalf("英文参数错：%d %q", code, errb.String())
	}
}

func TestApplyInEnglish(t *testing.T) {
	root := langFixture(t)
	out := runOut(t, "--apply", "--lang", "en", root)
	noChinese(t, "英文删除输出", out)
	for _, want := range []string{"\nDeleting:\n", "  deleted  branch done: deleted\n", "  deleted  worktree ", ": cleared git's record\n", "Restore commands are in "} {
		if !strings.Contains(out, want) {
			t.Fatalf("英文删除输出缺 %q：\n%s", want, out)
		}
	}
	logs, _ := filepath.Glob(filepath.Join(os.Getenv("XDG_STATE_HOME"), "repo-tidy", "apply-*.log"))
	if len(logs) != 1 {
		t.Fatalf("应有一份恢复日志：%v", logs)
	}
	data, err := os.ReadFile(logs[0])
	if err != nil {
		t.Fatal(err)
	}
	noChinese(t, "英文恢复日志", string(data))
	if !strings.Contains(string(data), "  restore: git -C ") {
		t.Fatalf("恢复日志应有恢复命令：\n%s", data)
	}
}
