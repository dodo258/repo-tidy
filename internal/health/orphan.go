package health

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dodo258/repo-tidy/internal/gitx"
)

// orphanBase 是代理放 worktree 的目录（相对主工作区），只在它下面找孤儿目录。
const orphanBase = ".claude/worktrees"

// Orphan 是 .claude/worktrees 下 git 已经没有登记的目录。只报不动：里面可能有没提交的改动。
type Orphan struct {
	Size
	HasGitFile bool // .git 文件还在，只是登记没了；否则连 .git 都没有
}

// orphans 找主工作区 .claude/worktrees 下的孤儿目录；某个目录没查成就记下来，其余照报。
// 只读文件系统和 index，不进那些目录里跑 git：git 会往登记里写东西，没有 .git 的目录还会被认成主工作区。
func (r *Report) orphans(ctx context.Context, common string, wts []gitx.Worktree) {
	fail := func(err error) { r.fail(fmt.Sprintf(r.t.FindOrphans, orphanBase), err) }
	root := canon(r.Root)
	base := filepath.Join(root, filepath.FromSlash(orphanBase))
	switch real, err := filepath.EvalSymlinks(base); {
	case errors.Is(err, fs.ErrNotExist):
		return
	case err != nil:
		fail(err)
		return
	case real != base:
		return // .claude 或 worktrees 是符号链接，里面的不一定是这个仓库的
	}
	s := &orphanScan{common: common, fail: fail}
	for _, w := range wts[1:] {
		s.registered = append(s.registered, canon(w.Path))
	}
	tracked, err := gitx.Run(ctx, root, "ls-files", "-z", "--", orphanBase)
	if err != nil {
		fail(err)
		return
	}
	for _, f := range strings.Split(tracked, "\x00") {
		s.tracked = append(s.tracked, filepath.Join(root, filepath.FromSlash(f)))
	}
	s.scan(base)
	r.Orphans = s.found
}

type orphanScan struct {
	common     string   // git 公共目录
	registered []string // 登记着的附加 worktree
	tracked    []string // 主仓库跟踪的、放在 .claude/worktrees 下的文件
	found      []Orphan
	fail       func(error)
}

func (s *orphanScan) scan(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		s.fail(err)
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue // 文件和符号链接
		}
		if err := s.visit(filepath.Join(dir, e.Name())); err != nil {
			s.fail(err)
		}
	}
}

// visit 看一个目录：是孤儿就记下；只是给 worktree 分组的目录（如 agent/）往里看。
func (s *orphanScan) visit(dir string) error {
	if slices.Contains(s.registered, dir) || anyUnder(s.tracked, dir) {
		return nil
	}
	outer := anyUnder(s.registered, dir) // 里面有登记着的 worktree
	info, err := os.Lstat(filepath.Join(dir, ".git"))
	if err == nil {
		// .git 是目录的是独立的仓库；套着登记的 worktree 的不报，免得连里面的一起被删
		if info.IsDir() || outer {
			return nil
		}
		gone, err := registrationGone(dir, s.common)
		if gone {
			s.found = append(s.found, Orphan{Size: Size{Path: dir}, HasGitFile: true})
		}
		return err
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	switch {
	case outer || groupsCheckouts(dir, entries):
		s.scan(dir)
	case len(entries) == 0: // 不占空间，也不挡 git worktree add
	case !hasAny(dir, ".jj", ".hg", ".svn"): // 有这些的是别的版本管理的工作区
		s.found = append(s.found, Orphan{Size: Size{Path: dir}})
	}
	return nil
}

// registrationGone 说明 dir 的 .git 文件指向的 worktree 登记是不是没了。
// 登记还在、属于别的仓库，或只是目录被移动过（git worktree repair 能接回来）的都不算没了。
func registrationGone(dir, common string) (bool, error) {
	link := filepath.Join(dir, ".git")
	b, err := os.ReadFile(link)
	if err != nil {
		return false, err
	}
	target, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir: ")
	if !ok {
		return true, nil // 内容坏了，git 认不出
	}
	admin, ours := absFrom(dir, target), filepath.Join(common, "worktrees")
	switch _, err := os.Stat(admin); {
	case errors.Is(err, fs.ErrNotExist):
		// 整个仓库被移动过时 .git 文件里还是老路径，登记在现在的仓库里
		admin = filepath.Join(ours, filepath.Base(admin))
	case err != nil:
		return false, err
	case canon(filepath.Dir(admin)) != canon(ours):
		return false, nil // 别的仓库的
	}
	b, err = os.ReadFile(filepath.Join(admin, "gitdir"))
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	owner, err := os.Stat(absFrom(admin, strings.TrimSpace(string(b))))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil // 登记还在，记的是移动前的位置
	}
	if err != nil {
		return false, err
	}
	self, err := os.Stat(link)
	if err != nil {
		return false, err
	}
	return !os.SameFile(owner, self), nil // 不是同一个：登记归别的目录了
}

// anyUnder 说明 paths 里有没有在 dir 下面的。
func anyUnder(paths []string, dir string) bool {
	prefix := dir + string(filepath.Separator)
	return slices.ContainsFunc(paths, func(p string) bool { return strings.HasPrefix(p, prefix) })
}

// groupsCheckouts：里面没有文件、有子目录带 .git 的，只是给 worktree 分组的目录。
func groupsCheckouts(dir string, entries []os.DirEntry) bool {
	found := false
	for _, e := range entries {
		if !e.IsDir() && e.Name() != ".DS_Store" {
			return false
		}
		found = found || e.IsDir() && hasAny(filepath.Join(dir, e.Name()), ".git")
	}
	return found
}

func hasAny(dir string, names ...string) bool {
	return slices.ContainsFunc(names, func(n string) bool {
		_, err := os.Lstat(filepath.Join(dir, n))
		return err == nil
	})
}

func absFrom(dir, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(dir, p)
}
