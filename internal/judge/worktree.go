package judge

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dodo258/repo-tidy/internal/gitx"
)

// regenerable 是删了也能重新生成的被忽略目录或文件名。
var regenerable = map[string]bool{
	"node_modules": true, "target": true, "dist": true, "build": true,
	".venv": true, "venv": true, "__pycache__": true, ".pytest_cache": true,
	".mypy_cache": true, ".ruff_cache": true, ".next": true, ".nuxt": true,
	".turbo": true, ".cache": true, "DerivedData": true, ".build": true,
	".gradle": true, "coverage": true, ".DS_Store": true,
}

// inProgress 是 git 目录里表示操作做到一半的文件；name 为空的是「rebase 或 am」，按语言取。
var inProgress = []struct{ file, name string }{
	{"rebase-merge", "rebase"}, {"rebase-apply", ""},
	{"MERGE_HEAD", "merge"}, {"CHERRY_PICK_HEAD", "cherry-pick"},
	{"REVERT_HEAD", "revert"}, {"BISECT_LOG", "bisect"},
}

// worktreeWorkers 是一个仓库里同时查几个 worktree 自己的目录（git status 等）。
const worktreeWorkers = 8

// wtCheck 是一个 worktree 自己目录里查到的情况，和提交判定无关。done 为真时 item 已是最终结论
// （锁定、只剩登记、读不了），否则 reasons 是目录里查出的留着原因，还要再看提交判定。
type wtCheck struct {
	item    Item
	done    bool
	reasons []string
	dirty   bool
}

// checkWorktrees 并行查每个 worktree 的目录（数量有上限），结果顺序和 wts 相同。
// 只读 checker 里扫描前就定好的字段，不碰提交判定的缓存。
func (c *checker) checkWorktrees(wts []gitx.Worktree) []wtCheck {
	out := make([]wtCheck, len(wts))
	sem := make(chan struct{}, worktreeWorkers)
	var wg sync.WaitGroup
	for i, w := range wts {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = c.checkWorktree(w)
		})
	}
	wg.Wait()
	return out
}

func (c *checker) checkWorktree(w gitx.Worktree) wtCheck {
	it := Item{Kind: KindWorktree, Name: w.Path, Branch: w.Branch, Head: w.Head}
	if t, err := c.git("log", "-1", "--format=%ct", w.Head); err == nil {
		if sec, err := strconv.ParseInt(t, 10, 64); err == nil {
			it.LastActive = time.Unix(sec, 0)
		}
	}
	if w.Locked {
		reason := c.t.Locked
		if w.LockReason != "" {
			reason = fmt.Sprintf(c.t.LockedWhy, w.LockReason)
		}
		it.Basis, it.Reasons = Keep, []string{reason}
		return wtCheck{item: it, done: true}
	}
	if w.Prunable {
		it.Basis, it.Removable = Prunable, true
		it.Evidence, it.Short = c.t.EvPrunable, c.t.EvPrunableShort
		return wtCheck{item: it, done: true}
	}

	var reasons []string
	gitDir, err := gitx.Run(c.ctx, w.Path, "rev-parse", "--absolute-git-dir")
	if err != nil {
		it.Basis, it.Reasons = Keep, []string{fmt.Sprintf(c.t.CantRead, err)}
		return wtCheck{item: it, done: true}
	}
	// 多久没动：HEAD 提交时间、index 修改时间、reflog 最后一条的时间，取最新。
	// reflog 文件本身的修改时间会被批量操作刷新，不可信，读记录里的时间戳。
	for _, t := range []time.Time{modTime(filepath.Join(gitDir, "index")), lastReflogTime(filepath.Join(gitDir, "logs", "HEAD"))} {
		if t.After(it.LastActive) {
			it.LastActive = t
		}
	}
	reasons = append(reasons, c.busyReasons(w.Path, gitDir)...)
	for _, op := range inProgress {
		if _, err := os.Stat(filepath.Join(gitDir, op.file)); err == nil {
			name := op.name
			if name == "" {
				name = c.t.RebaseOrAm
			}
			reasons = append(reasons, fmt.Sprintf(c.t.InProgress, name))
		}
	}

	dirty, ignored, err := worktreeStatus(c, w.Path)
	if err != nil {
		it.Basis, it.Reasons = Keep, []string{fmt.Sprintf(c.t.CantStatus, err)}
		return wtCheck{item: it, done: true}
	}
	if len(dirty) > 0 {
		reasons = append(reasons, fmt.Sprintf(c.t.Dirty, len(dirty), c.t.List(dirty, 3)))
	}
	var keepIgnored []string
	for _, p := range ignored {
		if !c.ignoredIsSafe(w.Path, p) {
			keepIgnored = append(keepIgnored, p)
		}
	}
	if len(keepIgnored) > 0 {
		reasons = append(reasons, fmt.Sprintf(c.t.IgnoredKeep, c.t.List(keepIgnored, 3)))
	}
	return wtCheck{item: it, reasons: reasons, dirty: len(dirty) > 0}
}

// worktreeItem 把目录里查到的情况和提交判定合成结论。
func (c *checker) worktreeItem(chk wtCheck, statusOf func(sha, branch string) proof) Item {
	it := chk.item
	if chk.done {
		return it
	}
	p := statusOf(it.Head, it.Branch)
	if len(chk.reasons) > 0 {
		it.Basis, it.Reasons = Keep, chk.reasons
		if p.basis == Keep {
			it.Reasons = append(it.Reasons, p.reasons...)
		}
		it.Unmerged = chk.dirty || p.unmerged
		return it
	}
	it.apply(p)
	return it
}

// worktreeStatus 返回未提交改动（含未跟踪文件）和被忽略的路径。
func worktreeStatus(c *checker, dir string) (dirty, ignored []string, err error) {
	out, _, err := gitx.Exec(c.ctx, dir, gitx.Opts{}, "status", "--porcelain=v1", "-z", "--ignored", "--untracked-files=normal")
	if err != nil {
		return nil, nil, err
	}
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		xy, path := f[:2], f[3:]
		if xy == "!!" {
			ignored = append(ignored, path)
			continue
		}
		dirty = append(dirty, path)
		if xy[0] == 'R' || xy[0] == 'C' {
			i++ // 改名记录后面跟着原路径
		}
	}
	return dirty, ignored, nil
}

// ignoredIsSafe：被忽略的路径删了没关系——在可再生清单里、是编译出来的可执行文件，
// 或者主工作区同一路径有一模一样的文件。
// 只看这一项自己的名字：git 只在整个目录都被忽略时才报 `build/`，目录里有被跟踪的文件时
// 报的是 `build/prod.env` 这样的文件本身，上级目录叫 build 不代表这个文件能重新生成。
func (c *checker) ignoredIsSafe(wt, rel string) bool {
	clean := strings.TrimSuffix(rel, "/")
	if regenerable[filepath.Base(clean)] {
		return true
	}
	if strings.HasSuffix(clean, ".pyc") || strings.HasSuffix(clean, ".pyo") {
		return true
	}
	if strings.HasSuffix(rel, "/") {
		return false
	}
	full := filepath.Join(wt, clean)
	if isCompiledBinary(full) {
		return true
	}
	return sameFile(full, filepath.Join(c.root, clean))
}

func isCompiledBinary(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 4)
	if _, err := io.ReadFull(f, head); err != nil {
		return false
	}
	for _, magic := range [][]byte{
		{0x7f, 'E', 'L', 'F'},    // ELF
		{0xcf, 0xfa, 0xed, 0xfe}, // Mach-O 64 位
		{0xce, 0xfa, 0xed, 0xfe}, // Mach-O 32 位
		{0xca, 0xfe, 0xba, 0xbe}, // Mach-O 通用
	} {
		if bytes.Equal(head, magic) {
			return true
		}
	}
	return false
}

func sameFile(a, b string) bool {
	ia, err := os.Stat(a)
	if err != nil || !ia.Mode().IsRegular() {
		return false
	}
	ib, err := os.Stat(b)
	if err != nil || !ib.Mode().IsRegular() || ia.Size() != ib.Size() {
		return false
	}
	da, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	db, err := os.ReadFile(b)
	if err != nil {
		return false
	}
	return bytes.Equal(da, db)
}

func modTime(path string) time.Time {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// lastReflogTime 读 reflog 最后一条记录的时间：「旧 新 名字 <邮箱> 秒数 时区\t说明」。
func lastReflogTime(path string) time.Time {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	head, _, _ := strings.Cut(lines[len(lines)-1], "\t")
	i := strings.LastIndex(head, "> ")
	if i < 0 {
		return time.Time{}
	}
	f := strings.Fields(head[i+2:])
	if len(f) == 0 {
		return time.Time{}
	}
	sec, err := strconv.ParseInt(f[0], 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}
