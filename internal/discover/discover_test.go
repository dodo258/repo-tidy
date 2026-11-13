package discover

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	tr "github.com/dodo258/repo-tidy/internal/testrepo"
)

func mk(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestRepos(t *testing.T) {
	root := t.TempDir()
	mk(t, filepath.Join(root, "a", ".git"))
	mk(t, filepath.Join(root, "b", ".git"))
	mk(t, filepath.Join(root, "notrepo", "deep", ".git")) // 只看下面一层
	mk(t, filepath.Join(root, ".hidden", ".git"))
	if err := os.WriteFile(filepath.Join(root, "file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Repos([]string{root, filepath.Join(root, "a")}, DefaultDepth)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(root, "a"), filepath.Join(root, "b")}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v want %v", got, want)
	}
	if _, err := Repos([]string{filepath.Join(root, "missing")}, DefaultDepth); err == nil {
		t.Fatal("不存在的路径应报错")
	}
	if _, err := Repos([]string{filepath.Join(root, "file")}, DefaultDepth); err == nil {
		t.Fatal("文件路径应报错")
	}
}

// 一层、两层、三层的仓库，一个仓库里套着它自己的 worktree 和一个子目录里的仓库，
// 再加上该跳过的 node_modules、隐藏目录和符号链接。
func TestReposDepth(t *testing.T) {
	tr.Isolate(t)
	root := t.TempDir()
	a := filepath.Join(root, "a")
	tr.Init(t, a)
	tr.Git(t, a, "worktree", "add", "-q", filepath.Join(a, "wt", "inner"), "-b", "inner")
	mk(t, filepath.Join(a, "vendor", "lib", ".git")) // 仓库里面的不当独立仓库
	b := filepath.Join(root, "org", "b")
	tr.Init(t, b)
	c := filepath.Join(root, "org", "team", "c")
	tr.Init(t, c)
	mk(t, filepath.Join(root, "org", "node_modules", "pkg", ".git"))
	mk(t, filepath.Join(root, "org", ".cache", "x", ".git"))
	if err := os.Symlink(filepath.Join(root, "org"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(b, filepath.Join(root, "org", "team", "b-link")); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		depth int
		want  []string
	}{
		{1, []string{a}},
		{2, []string{a, b}},
		{3, []string{a, b, c}},
		{MaxDepth, []string{a, b, c}},
	} {
		got, err := Repos([]string{root}, tc.depth)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("depth %d：得到 %v，应为 %v", tc.depth, got, tc.want)
		}
	}
}

// 下层读不了的目录跳过，不让整次扫描失败。
func TestReposSkipsUnreadable(t *testing.T) {
	root := t.TempDir()
	mk(t, filepath.Join(root, "ok", "r", ".git"))
	locked := filepath.Join(root, "locked")
	mk(t, filepath.Join(locked, "r", ".git"))
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	got, err := Repos([]string{root}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{filepath.Join(root, "ok", "r")}; !slices.Equal(got, want) {
		t.Fatalf("得到 %v，应为 %v", got, want)
	}
}
