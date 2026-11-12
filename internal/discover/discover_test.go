package discover

import (
	"os"
	"path/filepath"
	"testing"
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

	got, err := Repos([]string{root, filepath.Join(root, "a")})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(root, "a"), filepath.Join(root, "b")}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v want %v", got, want)
	}
	if _, err := Repos([]string{filepath.Join(root, "missing")}); err == nil {
		t.Fatal("不存在的路径应报错")
	}
	if _, err := Repos([]string{filepath.Join(root, "file")}); err == nil {
		t.Fatal("文件路径应报错")
	}
}
