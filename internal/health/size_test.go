package health

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// walkSize 是逐个遍历的参照实现（改成并行之前的做法），并行版的结果必须和它一样。
func walkSize(t *testing.T, root string, skip map[string]bool) int64 {
	t.Helper()
	var total int64
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() && p != root {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if p != root && skip[p] {
				return fs.SkipDir
			}
			return nil
		}
		if fi, err := d.Info(); err == nil {
			total += fi.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return total
}

func write(t *testing.T, p string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.Repeat("x", n)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDirSizesMatchSequentialWalk(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(base, "main")
	n := 0
	for i := range 40 {
		for j := range 15 {
			n++
			write(t, filepath.Join(main, "d"+string(rune('a'+i%26)), "sub"+string(rune('a'+j)), "f.txt"), n)
		}
	}
	write(t, filepath.Join(main, "top.txt"), 7)
	// 放在主工作区里的 worktree 和 .git：算主工作区时要跳过
	nested := filepath.Join(main, ".claude", "worktrees", "x")
	write(t, filepath.Join(nested, "a.txt"), 1000)
	write(t, filepath.Join(main, ".git", "objects", "o"), 500)
	// 符号链接不跟随，只算链接本身
	if err := os.Symlink(filepath.Join(main, "da"), filepath.Join(main, "link")); err != nil {
		t.Fatal(err)
	}
	// 读不了的子目录整个跳过
	locked := filepath.Join(main, "locked")
	write(t, filepath.Join(locked, "secret"), 300)
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	file := filepath.Join(base, "plain.txt")
	write(t, file, 42)

	skip := map[string]bool{nested: true, filepath.Join(main, ".git"): true}
	roots := []sizeRoot{
		{path: filepath.Join(main, ".git")},
		{path: main, useSkip: true},
		{path: nested, useSkip: true},
		{path: filepath.Join(base, "gone")},
		{path: file},
	}
	sizes, errs := dirSizes(context.Background(), roots, skip)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("root %d：%v", i, err)
		}
	}
	want := []int64{500, walkSize(t, main, skip), 1000, 0, 42}
	for i, w := range want {
		if sizes[i].Bytes != w {
			t.Fatalf("root %d（%s）= %d，逐个遍历 = %d", i, roots[i].path, sizes[i].Bytes, w)
		}
	}
	if !sizes[3].Missing || sizes[1].Missing {
		t.Fatalf("Missing 标错了：%+v", sizes)
	}
	if sizes[1].Bytes <= 7 {
		t.Fatalf("主工作区大小不对：%d", sizes[1].Bytes)
	}
}

func TestDirSizesCancelled(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a", "b"), 10)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, errs := dirSizes(ctx, []sizeRoot{{path: dir}}, nil)
	if errs[0] == nil {
		t.Fatal("取消后应报错")
	}
}
