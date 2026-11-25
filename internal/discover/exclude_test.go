package discover

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestSplitEnv(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []string
	}{
		{"", nil},
		{":::", nil},
		{"/a", []string{"/a"}},
		{":/a::b:", []string{"/a", "b"}},
	} {
		if got := SplitEnv(c.in); !slices.Equal(got, c.want) {
			t.Errorf("SplitEnv(%q) = %q，应为 %q", c.in, got, c.want)
		}
	}
}

func TestExcludesMatch(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	home := filepath.Join(root, "home")
	mk(t, filepath.Join(root, "work", "app"))
	t.Chdir(filepath.Join(root, "work"))
	ex, err := NewExcludes([]string{"/abs/ap", "app/", "~/proj", "~"}, home)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		p    string
		want bool
	}{
		{"/abs/ap", true},
		{"/abs/ap/x/y", true},
		{"/abs/app", false}, // 同前缀的邻居不算
		{"/abs", false},
		{filepath.Join(root, "work", "app"), true}, // 相对当前目录
		{filepath.Join(root, "work", "app", "sub"), true},
		{filepath.Join(root, "work", "apple"), false},
		{filepath.Join(home, "proj", "r"), true}, // ~/ 展开
		{home, true},                             // ~ 本身
		{filepath.Join(root, "homework"), false},
	} {
		if got := ex.Match(c.p); got != c.want {
			t.Errorf("Match(%q) = %v，应为 %v", c.p, got, c.want)
		}
	}
	if (Excludes{}).Any() || (Excludes{}).Match("/abs/ap") {
		t.Error("零值不应排除任何路径")
	}
}

// 排除路径和被比的路径一边经过符号链接时也认得出（macOS 上 /var 是 /private/var 的链接）。
func TestExcludesSymlink(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	real := filepath.Join(root, "real")
	mk(t, filepath.Join(real, "r"))
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	byLink, _ := NewExcludes([]string{link}, "")
	byReal, _ := NewExcludes([]string{real}, "")
	for _, c := range []struct {
		ex Excludes
		p  string
	}{
		{byLink, filepath.Join(real, "r")},
		{byReal, filepath.Join(link, "r")},
	} {
		if !c.ex.Match(c.p) {
			t.Errorf("%v 应排除 %s", c.ex.dirs, c.p)
		}
	}
}
