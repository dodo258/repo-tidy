// Package discover 从命令行给的路径找出要扫的仓库目录，只看文件系统。
package discover

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Repos 返回候选仓库目录：路径本身有 .git 就是它；否则取它下面一层有 .git 的子目录
// （跳过隐藏目录和符号链接）。同一个仓库的去重由调用方按主工作区做。
func Repos(paths []string) ([]string, error) {
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
			return nil, fmt.Errorf("路径 %s：%w", p, err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("路径 %s：%w", p, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("路径 %s 不是目录", p)
		}
		if hasGit(abs) {
			add(abs)
			continue
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			return nil, fmt.Errorf("读目录 %s：%w", p, err)
		}
		var found []string
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			child := filepath.Join(abs, e.Name())
			if hasGit(child) {
				found = append(found, child)
			}
		}
		sort.Strings(found)
		for _, c := range found {
			add(c)
		}
	}
	return out, nil
}

func hasGit(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}
