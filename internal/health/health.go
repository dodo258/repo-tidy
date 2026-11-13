// Package health 给一个仓库做体检：占用空间、主工作区状态、stash 数、
// 当前分支相对上游领先落后、最后一次提交时间、备份远程（默认叫 backup）是不是推到最新。
// 只报不动：不 fetch、不刷新 index、不写 .git；--online 时只多跑一条 ls-remote。
package health

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dodo258/repo-tidy/internal/gitx"
	"github.com/dodo258/repo-tidy/internal/msg"
)

// DefaultBackupRemote 是没指定时比对的备份远程名。
const DefaultBackupRemote = "backup"

// Options 是体检的选项。
type Options struct {
	Online        bool   // 联网（ls-remote）查备份远程的实际状态
	BackupRemote  string // 要比对的备份远程名；空表示 DefaultBackupRemote
	RequireBackup bool   // 用户显式指定了备份远程：仓库里没有时也要报
}

// lsRemoteTimeout 是 --online 时 ls-remote 的超时；测试里调短。
var lsRemoteTimeout = gitx.NetTimeout

// Size 是一个目录的占用（文件大小之和，不跟随符号链接）。
type Size struct {
	Path    string
	Bytes   int64
	Missing bool // 目录已不在（worktree 只剩登记）
}

// Report 是一个仓库的体检结果。某一项没查成时记进 Errs，其余照报。
type Report struct {
	Root string
	Note string // 整个跳过时的说明（如裸仓库）

	t *msg.Text // 写说明和出错原因用的文案

	GitDir    Size   // 公共 git 目录（通常是 .git）
	Main      Size   // 主工作区，不含 .git 和放在里面的附加 worktree
	Worktrees []Size // 附加 worktree，顺序同 git worktree list

	Branch       string // 主工作区当前分支；分离头指针时为空
	Upstream     string // 上游，没设为空
	UpstreamGone bool   // 设了上游但上游引用已不在
	Ahead        int    // 当前分支有、上游没有的提交数
	Behind       int    // 上游有、当前分支没有的提交数
	Changes      int    // 主工作区未提交改动（含未跟踪）
	Untracked    int    // 其中未跟踪的
	Stashes      int

	LastCommit       time.Time // 本地分支里最新的提交时间
	LastCommitBranch string

	Backup Backup
	Errs   []string
}

// Backup 是备份远程的同步情况。
type Backup struct {
	Remote    string // 比对的远程名
	Required  bool   // 显式指定的：没有这个远程也要报；否则没有就不提
	Present   bool
	Online    bool   // 这次联网用 ls-remote 查的
	OnlineErr string // 联网查失败的原因；此时按本地远程跟踪引用比对
	Checked   int    // 比对了几个本地分支
	Stale     []BranchSync
}

// SyncState 是一个本地分支在 backup 上的状态。
type SyncState string

const (
	NotPushed SyncState = "not-pushed" // backup 上没有这个分支
	Behind    SyncState = "behind"     // backup 上的是本地的旧版本
	Diverged  SyncState = "diverged"   // 两边各有对方没有的提交
	Unknown   SyncState = "unknown"    // backup 上的提交本地没有，比不了
)

// BranchSync 是一个没推到最新的本地分支。
type BranchSync struct {
	Branch     string
	State      SyncState
	Local      string // 本地提交
	Remote     string // backup 上的提交；没推时为空
	LocalOnly  int    // 本地有、backup 没有的提交数
	RemoteOnly int    // backup 有、本地没有的提交数
}

// Inspect 给 dir 所在的仓库体检。说明和出错原因用 lang 这种语言写。
func Inspect(ctx context.Context, dir string, opts Options, lang msg.Lang) (Report, error) {
	t := msg.For(lang)
	wts, err := gitx.Worktrees(ctx, dir)
	if err != nil {
		return Report{}, fmt.Errorf(t.ListWorktrees, err)
	}
	if len(wts) == 0 {
		return Report{}, fmt.Errorf(t.NoWorkTree, dir)
	}
	if opts.BackupRemote == "" {
		opts.BackupRemote = DefaultBackupRemote
	}
	r := &Report{Root: wts[0].Path, t: t, Backup: Backup{Remote: opts.BackupRemote, Required: opts.RequireBackup}}
	if wts[0].Bare {
		r.Note = t.Bare
		return *r, nil
	}
	common, err := gitx.Run(ctx, r.Root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return *r, fmt.Errorf(t.FindGitDir, err)
	}
	r.sizes(ctx, common, wts)
	r.workdir(ctx)
	r.upstream(ctx)
	r.lastCommit(ctx)
	r.backup(ctx, opts.Online)
	return *r, nil
}

func (r *Report) fail(what string, err error) {
	r.Errs = append(r.Errs, fmt.Sprintf(r.t.Colon, what, brief(r.t, err)))
}

// sizes 量 .git、主工作区和各附加 worktree。放在主工作区里的 worktree（如 .claude/worktrees/x）
// 只算在它自己头上，不重复算进主工作区。
func (r *Report) sizes(ctx context.Context, common string, wts []gitx.Worktree) {
	skip := map[string]bool{canon(common): true, canon(filepath.Join(r.Root, ".git")): true}
	for _, w := range wts[1:] {
		skip[canon(w.Path)] = true
	}
	// .git、主工作区、各附加 worktree 一起并行量（同时读目录的协程数有上限，见 readSlots）
	paths := []string{common, r.Root}
	roots := []sizeRoot{{path: canon(common)}, {path: canon(r.Root), useSkip: true}}
	for _, w := range wts[1:] {
		paths = append(paths, w.Path)
		roots = append(roots, sizeRoot{path: canon(w.Path), useSkip: true})
	}
	sizes, errs := dirSizes(ctx, roots, skip)
	for i, p := range paths {
		sizes[i].Path = p
		switch {
		case errs[i] == nil:
		case i == 0:
			r.fail(r.t.MeasureGitDir, errs[i])
		default:
			r.fail(fmt.Sprintf(r.t.Measure, p), errs[i])
		}
	}
	r.GitDir, r.Main, r.Worktrees = sizes[0], sizes[1], sizes[2:]
}

func canon(p string) string {
	if c, err := filepath.EvalSymlinks(p); err == nil {
		return c
	}
	return filepath.Clean(p)
}

// workdir 数主工作区的未提交改动和 stash。
func (r *Report) workdir(ctx context.Context) {
	out, err := gitx.Run(ctx, r.Root, "status", "--porcelain=v1", "-z", "--no-renames")
	if err != nil {
		r.fail(r.t.CheckChanges, err)
	} else {
		for _, e := range strings.Split(out, "\x00") {
			if e == "" {
				continue
			}
			r.Changes++
			if strings.HasPrefix(e, "??") {
				r.Untracked++
			}
		}
	}
	out, err = gitx.Run(ctx, r.Root, "stash", "list", "--format=%H")
	if err != nil {
		r.fail(r.t.CountStashes, err)
		return
	}
	r.Stashes = len(gitx.Lines(out))
}

// upstream 算主工作区当前分支相对上游领先落后多少个提交。
func (r *Report) upstream(ctx context.Context) {
	br, code, err := gitx.Exec(ctx, r.Root, gitx.Opts{}, "symbolic-ref", "-q", "--short", "HEAD")
	if code == 1 {
		return // 分离头指针
	}
	if err != nil {
		r.fail(r.t.FindBranch, err)
		return
	}
	r.Branch = strings.TrimSpace(br)
	out, err := gitx.Run(ctx, r.Root, "for-each-ref", "--format=%(upstream)%00%(upstream:short)%00%(upstream:track)", "refs/heads/"+r.Branch)
	if err != nil {
		r.fail(r.t.FindUpstream, err)
		return
	}
	f := strings.Split(out, "\x00")
	if len(f) != 3 || f[0] == "" {
		return
	}
	r.Upstream = f[1]
	if f[2] == "[gone]" {
		r.UpstreamGone = true
		return
	}
	a, b, err := leftRight(ctx, r.Root, "refs/heads/"+r.Branch, f[0])
	if err != nil {
		r.fail(r.t.CompareUpstream, err)
		return
	}
	r.Ahead, r.Behind = a, b
}

// leftRight 返回 a 有 b 没有、b 有 a 没有的提交数。
func leftRight(ctx context.Context, dir, a, b string) (int, int, error) {
	out, err := gitx.Run(ctx, dir, "rev-list", "--left-right", "--count", a+"..."+b)
	if err != nil {
		return 0, 0, err
	}
	l, rr, ok := strings.Cut(out, "\t")
	x, err1 := strconv.Atoi(l)
	y, err2 := strconv.Atoi(rr)
	if !ok || err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("unexpected rev-list output %q", out)
	}
	return x, y, nil
}

func (r *Report) lastCommit(ctx context.Context) {
	out, err := gitx.Run(ctx, r.Root, "for-each-ref", "--sort=-committerdate", "--count=1", "--format=%(committerdate:unix)%00%(refname:short)", "refs/heads")
	if err != nil {
		r.fail(r.t.FindLastCommit, err)
		return
	}
	sec, name, ok := strings.Cut(out, "\x00")
	if !ok {
		return // 还没有提交
	}
	n, err := strconv.ParseInt(sec, 10, 64)
	if err != nil {
		r.fail(r.t.FindLastCommit, err)
		return
	}
	r.LastCommit, r.LastCommitBranch = time.Unix(n, 0), name
}

// backup 逐个本地分支比对备份远程上的同名分支。默认用本地远程跟踪引用
// refs/remotes/<远程>/*（上次 fetch/push 时的状态）；online 时用 ls-remote 查实际状态，
// 联不上就记下原因、退回本地记录。
func (r *Report) backup(ctx context.Context, online bool) {
	remotes, err := gitx.Run(ctx, r.Root, "remote")
	if err != nil {
		r.fail(r.t.ListingRemotes, err)
		return
	}
	for _, name := range gitx.Lines(remotes) {
		if name == r.Backup.Remote {
			r.Backup.Present = true
		}
	}
	if !r.Backup.Present {
		return
	}
	var remote map[string]string
	if online {
		r.Backup.Online = true
		if remote, err = lsRemote(ctx, r.Root, r.Backup.Remote); err != nil {
			r.Backup.OnlineErr = brief(r.t, err)
		}
	}
	if remote == nil {
		if remote, err = trackingRefs(ctx, r.Root, r.Backup.Remote); err != nil {
			r.fail(fmt.Sprintf(r.t.ReadBackupRefs, r.Backup.Remote), err)
			return
		}
	}
	out, err := gitx.Run(ctx, r.Root, "for-each-ref", "--format=%(refname:short)%00%(objectname)", "refs/heads")
	if err != nil {
		r.fail(r.t.ListingBranches, err)
		return
	}
	for _, l := range gitx.Lines(out) {
		name, local, ok := strings.Cut(l, "\x00")
		if !ok {
			continue
		}
		r.Backup.Checked++
		s, err := compare(ctx, r.Root, name, local, remote[name])
		if err != nil {
			r.fail(fmt.Sprintf(r.t.CompareBackup, r.Backup.Remote, name), err)
			continue
		}
		if s != nil {
			r.Backup.Stale = append(r.Backup.Stale, *s)
		}
	}
}

// compare 返回 nil 表示 backup 上的已包含本地的全部提交。
func compare(ctx context.Context, dir, name, local, remote string) (*BranchSync, error) {
	s := &BranchSync{Branch: name, Local: local, Remote: remote}
	switch {
	case remote == "":
		s.State = NotPushed
		return s, nil
	case remote == local:
		return nil, nil
	}
	if _, code, _ := gitx.Exec(ctx, dir, gitx.Opts{}, "cat-file", "-e", remote+"^{commit}"); code != 0 {
		s.State = Unknown
		return s, nil
	}
	a, b, err := leftRight(ctx, dir, local, remote)
	if err != nil {
		return nil, err
	}
	s.LocalOnly, s.RemoteOnly = a, b
	switch {
	case a == 0:
		return nil, nil // backup 上更新，本地的都在
	case b == 0:
		s.State = Behind
	default:
		s.State = Diverged
	}
	return s, nil
}

func lsRemote(ctx context.Context, dir, name string) (map[string]string, error) {
	out, _, err := gitx.Exec(ctx, dir, gitx.Opts{Timeout: lsRemoteTimeout}, "ls-remote", "--heads", "--", name)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, l := range gitx.Lines(out) {
		sha, ref, ok := strings.Cut(l, "\t")
		if ok {
			m[strings.TrimPrefix(ref, "refs/heads/")] = sha
		}
	}
	return m, nil
}

func trackingRefs(ctx context.Context, dir, name string) (map[string]string, error) {
	prefix := "refs/remotes/" + name + "/"
	out, err := gitx.Run(ctx, dir, "for-each-ref", "--format=%(refname)%00%(objectname)", prefix)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, l := range gitx.Lines(out) {
		ref, sha, ok := strings.Cut(l, "\x00")
		if ok && ref != prefix+"HEAD" {
			m[strings.TrimPrefix(ref, prefix)] = sha
		}
	}
	return m, nil
}

// brief 取 git 自己报的第一行错误（或超时），不带命令和目录。
func brief(t *msg.Text, err error) string {
	var ge *gitx.Error
	if errors.As(err, &ge) {
		if errors.Is(ge.Err, context.DeadlineExceeded) {
			return fmt.Sprintf(t.NetTimeout, lsRemoteTimeout)
		}
		if msg := firstLine(ge.Stderr); msg != "" {
			return msg
		}
		if ge.Err != nil {
			return firstLine(ge.Err.Error())
		}
	}
	return firstLine(err.Error())
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
