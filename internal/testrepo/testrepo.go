// Package testrepo 在临时目录里造 git 仓库给测试用。
// Isolate 让 git 不读用户的全局配置和钩子（全局钩子会往 backup 推送）。
package testrepo

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Isolate 把 git 的全局配置换成临时文件，并固定作者和时间相关的环境。
func Isolate(t *testing.T) {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	content := "[user]\n\tname = Test\n\temail = test@example.com\n[init]\n\tdefaultBranch = main\n[advice]\n\tdetachedHead = false\n"
	if err := os.WriteFile(cfg, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
}

// Git 在 dir 里跑 git，失败就让测试失败，返回去掉首尾空白的输出。
func Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s：%v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// Init 建一个有初始提交的仓库，默认分支 main。
func Init(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	Git(t, dir, "init", "-q", "-b", "main")
	Write(t, dir, "README.md", "hello\n")
	Write(t, dir, "a.txt", "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n")
	Git(t, dir, "add", "-A")
	Git(t, dir, "commit", "-q", "-m", "init")
}

// Write 写一个文件（自动建目录）。
func Write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Commit 写文件并提交，返回提交号。
func Commit(t *testing.T, dir, rel, content, msg string) string {
	t.Helper()
	Write(t, dir, rel, content)
	Git(t, dir, "add", "-A")
	Git(t, dir, "commit", "-q", "-m", msg)
	return Git(t, dir, "rev-parse", "HEAD")
}

// Snapshot 记下 root 下每个文件的路径、大小和修改时间，用来证明扫描没写东西。
// git 写对象时发现对象已存在，会刷新那个对象文件的修改时间（防 gc），内容不变；
// 所以对象文件只比路径和大小。
func Snapshot(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b.WriteString(p)
		// git 写对象时发现对象已存在，会刷新那个对象文件的修改时间（防 gc），内容不变；
		// 对象文件只比路径和大小，其他文件连修改时间一起比。
		if !strings.Contains(p, "/objects/") {
			b.WriteString(" " + info.ModTime().String())
		}
		if !d.IsDir() {
			fmt.Fprintf(&b, " %d", info.Size())
		}
		b.WriteString("\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// LineDiff 列出两次 Snapshot 之间多出（+）和少了（-）的行。
func LineDiff(a, b string) string {
	in := map[string]bool{}
	for _, l := range strings.Split(a, "\n") {
		in[l] = true
	}
	var out []string
	for _, l := range strings.Split(b, "\n") {
		if !in[l] {
			out = append(out, "+ "+l)
		}
	}
	in = map[string]bool{}
	for _, l := range strings.Split(b, "\n") {
		in[l] = true
	}
	for _, l := range strings.Split(a, "\n") {
		if !in[l] {
			out = append(out, "- "+l)
		}
	}
	return strings.Join(out, "\n")
}

// Idle 把 worktree 的 index、HEAD、reflog 的修改时间往回拨两小时，
// 让它不被「刚动过，可能还在用」那条规则留下（默认 --min-idle 是 30 分钟）。
func Idle(t *testing.T, wt string) {
	t.Helper()
	gitDir := Git(t, wt, "rev-parse", "--absolute-git-dir")
	old := time.Now().Add(-2 * time.Hour)
	for _, f := range []string{"index", "HEAD", filepath.Join("logs", "HEAD")} {
		p := filepath.Join(gitDir, f)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
}
