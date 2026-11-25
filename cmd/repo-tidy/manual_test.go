package main

import (
	"maps"
	"os"
	"regexp"
	"slices"
	"testing"

	"github.com/dodo258/repo-tidy/internal/msg"
)

// 文档里提到的 git 自己的选项。
var gitOptions = []string{"detach", "force", "merged", "show-toplevel"}

// options 收集 re 第一个分组匹配到的名字，去重排序。
func options(text string, re *regexp.Regexp) []string {
	set := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		set[m[1]] = true
	}
	return slices.Sorted(maps.Keys(set))
}

func readText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// 程序认的每个选项，中英 --help 都有一条说明，没有多出来的。
func TestManualHelpListsEveryOption(t *testing.T) {
	registered := options(readText(t, "main.go"), regexp.MustCompile(`fs\.\w+\("([a-z-]+)"`))
	if len(registered) < 10 {
		t.Fatalf("main.go 里只认出这些选项：%q", registered)
	}
	entry := regexp.MustCompile(`(?m)^  --([a-z-]+)`)
	for _, l := range []msg.Lang{msg.ZH, msg.EN} {
		if got := options(msg.For(l).Usage, entry); !slices.Equal(got, registered) {
			t.Errorf("语言 %v 的 --help 说明了 %q，程序认的是 %q", l, got, registered)
		}
	}
}

// README、docs/reference.md 写的选项 --help 里都有；中英 README 写的是同一批。
func TestManualDocsMatchHelp(t *testing.T) {
	mention := regexp.MustCompile(`--([a-z][a-z-]*)`)
	help := options(msg.For(msg.EN).Usage, regexp.MustCompile(`(?m)^  --([a-z-]+)`))
	docs := map[string][]string{}
	for _, f := range []string{"README.md", "README.en.md", "docs/reference.md"} {
		docs[f] = options(readText(t, "../../"+f), mention)
		for _, o := range docs[f] {
			if !slices.Contains(help, o) && !slices.Contains(gitOptions, o) {
				t.Errorf("%s 写了 --%s，--help 里没有", f, o)
			}
		}
	}
	if !slices.Equal(docs["README.md"], docs["README.en.md"]) {
		t.Errorf("中英 README 写的选项不一样：%q 和 %q", docs["README.md"], docs["README.en.md"])
	}
}
