// Package apply 删除判定为可删的 worktree 和本地分支。
// 删之前重新判定一遍，只删前后两次都判可删、提交没变的；靠「已推到远程」可删的，
// 先联网确认远程现在还有这个提交。每一项删之前把恢复命令写进日志。
package apply

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/dodo258/repo-tidy/internal/gitx"
	"github.com/dodo258/repo-tidy/internal/judge"
	"github.com/dodo258/repo-tidy/internal/msg"
)

// Result 是一项删除的结果。
type Result struct {
	Kind string
	Name string
	Done bool
	Msg  string // 做了什么，或为什么没做
}

// Apply 对 prev（之前给用户看的报告）里可删的项重新判定后删除，恢复命令写进 log。
// 重新判定用 opts，要现查进程，不复用扫描时的结果：报告之后才开始用的 worktree 也不删。
// 结果说明和日志里的注释用 opts.Lang 这种语言。
func Apply(ctx context.Context, prev judge.RepoReport, opts judge.Options, log io.Writer) ([]Result, error) {
	t := msg.For(opts.Lang)
	fresh, err := judge.Inspect(ctx, prev.Root, opts)
	if err != nil {
		return nil, fmt.Errorf(t.Recheck, prev.Root, err)
	}
	root := fresh.Root
	fmt.Fprintf(log, t.LogRepo, root, fresh.DefaultBranch)

	var results []Result
	confirmed := map[string]string{} // 「远程 引用 提交」→ 不删的原因（空串表示确认通过）
	// pick 取报告时可删、现在还可删且提交没变的项；其余记一条「没删」。
	pick := func(kind string, before, now []judge.Item) []judge.Item {
		var ok []judge.Item
		for _, was := range before {
			if !was.Removable {
				continue
			}
			it, found := byName(now, was.Name)
			if !found {
				results = append(results, Result{kind, was.Name, false, t.Gone})
				continue
			}
			if !it.Removable {
				results = append(results, Result{kind, was.Name, false, fmt.Sprintf(t.ChangedKeep, strings.Join(it.Reasons, t.ReasonSep))})
				continue
			}
			if it.Head != was.Head {
				results = append(results, Result{kind, was.Name, false, t.Changed})
				continue
			}
			if it.Basis == judge.Pushed {
				key := it.Remote + " " + it.RemoteRef + " " + it.Head
				why, done := confirmed[key]
				if !done {
					why = confirmRemote(ctx, t, root, it)
					confirmed[key] = why
				}
				if why != "" {
					results = append(results, Result{kind, it.Name, false, why})
					continue
				}
			}
			ok = append(ok, it)
		}
		return ok
	}

	var prunable, worktrees []judge.Item
	for _, w := range pick(judge.KindWorktree, prev.Worktrees, fresh.Worktrees) {
		if w.Basis == judge.Prunable {
			prunable = append(prunable, w)
		} else {
			worktrees = append(worktrees, w)
		}
	}
	// 1. 目录已不存在的 worktree：清登记（git worktree prune 只清这类，跳过锁定的）
	if len(prunable) > 0 {
		for _, w := range prunable {
			fmt.Fprintf(log, t.LogPruned, w.Name, refDesc(t, w))
			fmt.Fprintf(log, t.LogRestore, restoreWorktree(root, w))
		}
		_, err := gitx.Run(ctx, root, "worktree", "prune")
		for _, w := range prunable {
			results = append(results, outcome(t, judge.KindWorktree, w.Name, err, t.Pruned))
		}
	}
	// 2. 其余 worktree：不加 --force，git 自己还会再拦一次有改动的
	for _, w := range worktrees {
		fmt.Fprintf(log, t.LogWorktree, w.Name, refDesc(t, w), w.Evidence)
		fmt.Fprintf(log, t.LogRestore, restoreWorktree(root, w))
		_, err := gitx.Run(ctx, root, "worktree", "remove", w.Name)
		results = append(results, outcome(t, judge.KindWorktree, w.Name, err, t.Removed))
	}
	// 3. 分支：检出它的 worktree 没删成时，git 会拒绝删分支
	for _, b := range pick(judge.KindBranch, prev.Branches, fresh.Branches) {
		fmt.Fprintf(log, t.LogBranch, b.Name, b.Head, b.Evidence)
		fmt.Fprintf(log, t.LogRestore, fmt.Sprintf("git -C %s branch %s %s", quote(root), quote(b.Name), b.Head))
		_, err := gitx.Run(ctx, root, "branch", "-D", "--", b.Name)
		results = append(results, outcome(t, judge.KindBranch, b.Name, err, t.Removed))
	}
	return results, nil
}

func byName(list []judge.Item, name string) (judge.Item, bool) {
	for _, x := range list {
		if x.Name == name {
			return x, true
		}
	}
	return judge.Item{}, false
}

// confirmRemote 联网确认远程引用现在还包含这个提交；返回空串表示确认通过，否则是不删的原因。
func confirmRemote(ctx context.Context, t *msg.Text, root string, it judge.Item) string {
	out, _, err := gitx.Exec(ctx, root, gitx.Opts{Timeout: gitx.NetTimeout}, "ls-remote", "--", it.Remote, it.RemoteRef)
	if err != nil {
		return fmt.Sprintf(t.ConfirmFailed, it.Remote, firstLine(err.Error()))
	}
	sha, _, _ := strings.Cut(strings.TrimSpace(out), "\t")
	if sha == "" {
		return fmt.Sprintf(t.RemoteGone, it.Remote, it.RemoteRef)
	}
	if sha == it.Head {
		return ""
	}
	if _, err := gitx.Run(ctx, root, "cat-file", "-e", sha+"^{commit}"); err != nil {
		return fmt.Sprintf(t.RemoteUnknown, it.Remote, it.RemoteRef, sha[:8])
	}
	_, code, err := gitx.Exec(ctx, root, gitx.Opts{}, "merge-base", "--is-ancestor", it.Head, sha)
	if code == 0 {
		return ""
	}
	if code == 1 {
		return fmt.Sprintf(t.RemoteMoved, it.Remote, it.RemoteRef, sha[:8])
	}
	return fmt.Sprintf(t.ConfirmErr, err)
}

func outcome(t *msg.Text, kind, name string, err error, ok string) Result {
	if err != nil {
		return Result{kind, name, false, fmt.Sprintf(t.Failed, firstLine(err.Error()))}
	}
	return Result{kind, name, true, ok}
}

func refDesc(t *msg.Text, w judge.Item) string {
	if w.Branch != "" {
		return fmt.Sprintf(t.RefDesc, w.Branch, w.Head)
	}
	return fmt.Sprintf(t.RefDetached, w.Head)
}

func restoreWorktree(root string, w judge.Item) string {
	if w.Branch != "" {
		return fmt.Sprintf("git -C %s worktree add %s %s", quote(root), quote(w.Name), quote(w.Branch))
	}
	return fmt.Sprintf("git -C %s worktree add --detach %s %s", quote(root), quote(w.Name), w.Head)
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
