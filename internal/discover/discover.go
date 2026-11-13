// Package discover 从命令行给的路径找出要扫的仓库目录，只看文件系统。
package discover

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// 往下找仓库的层数：默认只看下面一层，最多四层。
const (
	DefaultDepth = 1
	MaxDepth     = 4
)

// Repos 返回候选仓库目录：路径本身有 .git 就是它；否则往下最多 depth 层找有 .git 的目录。
// 找到仓库就不再进它里面（里面的 worktree、子模块不算独立仓库）；跳过隐藏目录（含 .git）、
// node_modules 和符号链接。下层读不了的目录跳过，给的路径本身读不了才报错。
// 同一个仓库的去重由调用方按主工作区做。depth 由调用方校验在 1 到 MaxDepth 之间。
func Repos(paths []string, depth int) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, err // 已带路径，如 stat /x: no such file or directory
		}
		if !info.IsDir() {
			return nil, &fs.PathError{Op: "scan", Path: p, Err: syscall.ENOTDIR}
		}
		if hasGit(abs) {
			add(abs)
			continue
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			return nil, err
		}
		var found []string
		walk(abs, entries, depth, &found)
		sort.Strings(found)
		for _, c := range found {
			add(c)
		}
	}
	return out, nil
}

// walk 在 dir 的子目录里找仓库，left 是还能往下看的层数（含这一层）。
func walk(dir string, entries []os.DirEntry, left int, found *[]string) {
	for _, e := range entries {
		// DirEntry 不跟符号链接，指向目录的链接 IsDir 为假，正好跳过
		if !e.IsDir() || skip(e.Name()) {
			continue
		}
		child := filepath.Join(dir, e.Name())
		if hasGit(child) {
			*found = append(*found, child)
			continue
		}
		if left <= 1 {
			continue
		}
		sub, err := os.ReadDir(child)
		if err != nil {
			continue // 没权限之类：这一支不往下找
		}
		walk(child, sub, left-1, found)
	}
}

func skip(name string) bool {
	return strings.HasPrefix(name, ".") || name == "node_modules"
}

func hasGit(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}
