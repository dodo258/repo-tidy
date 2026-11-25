package discover

import (
	"os"
	"path/filepath"
	"strings"
)

// Excludes 是 --exclude 和 REPO_TIDY_EXCLUDE 给的排除路径。零值不排除任何路径。
type Excludes struct {
	dirs []string // 绝对、干净的路径；能解析符号链接的另存一份解析后的
}

// SplitEnv 按冒号拆开 REPO_TIDY_EXCLUDE，丢掉空段。
func SplitEnv(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ":") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// NewExcludes 把排除路径换成绝对路径：「~」和「~/」开头的按 home 展开，相对路径按当前目录。
// 路径不必存在；空字符串由调用方先挡掉。
func NewExcludes(paths []string, home string) (Excludes, error) {
	var ex Excludes
	for _, p := range paths {
		if p == "~" || strings.HasPrefix(p, "~/") {
			p = filepath.Join(home, p[1:])
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return Excludes{}, err
		}
		ex.dirs = append(ex.dirs, forms(abs)...)
	}
	return ex, nil
}

// Any 说明有没有排除路径。
func (ex Excludes) Any() bool { return len(ex.dirs) > 0 }

// Match 说明路径 p（绝对路径）是不是某个排除路径本身或在它下面。按目录边界比，
// /a/app 不算在 /a/ap 下面。两边都按原样和解析符号链接后的样子比（如 macOS 的 /var 和 /private/var）。
func (ex Excludes) Match(p string) bool {
	if !ex.Any() {
		return false
	}
	for _, c := range forms(filepath.Clean(p)) {
		for _, d := range ex.dirs {
			if within(c, d) {
				return true
			}
		}
	}
	return false
}

// forms 返回路径本身，以及解析符号链接后不一样的那个（解析不了就只有本身）。
func forms(p string) []string {
	if r, err := filepath.EvalSymlinks(p); err == nil && r != p {
		return []string{p, r}
	}
	return []string{p}
}

func within(p, dir string) bool {
	if p == dir {
		return true
	}
	if !strings.HasSuffix(dir, string(os.PathSeparator)) {
		dir += string(os.PathSeparator)
	}
	return strings.HasPrefix(p, dir)
}
