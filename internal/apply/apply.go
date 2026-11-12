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
)

// Result 是一项删除的结果。
type Result struct {
	Kind string
	Name string
	Done bool
	Msg  string // 做了什么，或为什么没做
}

// Apply 对 prev（之前给用户看的报告）里可删的项重新判定后删除，恢复命令写进 log。
func Apply(ctx context.Context, prev judge.RepoReport, log io.Writer) ([]Result, error) {
	fresh, err := judge.Inspect(ctx, prev.Root)
	if err != nil {
		return nil, fmt.Errorf("重新判定 %s：%w", prev.Root, err)
	}
	root := fresh.Root
	fmt.Fprintf(log, "\n# 仓库 %s（默认分支 %s）\n# 恢复时先跑分支的恢复命令，再跑 worktree 的。\n", root, fresh.DefaultBranch)

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
				results = append(results, Result{kind, was.Name, false, "已经不在了"})
				continue
			}
			if !it.Removable || it.Head != was.Head {
				results = append(results, Result{kind, was.Name, false, "状态和报告时不一样了，这次不动"})
				continue
			}
			if it.Basis == judge.Pushed {
				key := it.Remote + " " + it.RemoteRef + " " + it.Head
				why, done := confirmed[key]
				if !done {
					why = confirmRemote(ctx, root, it)
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
			fmt.Fprintf(log, "worktree %s（目录已不存在）%s\n  恢复：%s\n", w.Name, refDesc(w), restoreWorktree(root, w))
		}
		_, err := gitx.Run(ctx, root, "worktree", "prune")
		for _, w := range prunable {
			results = append(results, outcome(judge.KindWorktree, w.Name, err, "清掉了登记"))
		}
	}
	// 2. 其余 worktree：不加 --force，git 自己还会再拦一次有改动的
	for _, w := range worktrees {
		fmt.Fprintf(log, "worktree %s%s — %s\n  恢复：%s\n", w.Name, refDesc(w), w.Evidence, restoreWorktree(root, w))
		_, err := gitx.Run(ctx, root, "worktree", "remove", w.Name)
		results = append(results, outcome(judge.KindWorktree, w.Name, err, "删了"))
	}
	// 3. 分支：检出它的 worktree 没删成时，git 会拒绝删分支
	for _, b := range pick(judge.KindBranch, prev.Branches, fresh.Branches) {
		fmt.Fprintf(log, "branch %s @ %s — %s\n  恢复：git -C %s branch %s %s\n", b.Name, b.Head, b.Evidence, quote(root), quote(b.Name), b.Head)
		_, err := gitx.Run(ctx, root, "branch", "-D", "--", b.Name)
		results = append(results, outcome(judge.KindBranch, b.Name, err, "删了"))
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
func confirmRemote(ctx context.Context, root string, it judge.Item) string {
	out, _, err := gitx.Exec(ctx, root, gitx.Opts{Timeout: gitx.NetTimeout}, "ls-remote", "--", it.Remote, it.RemoteRef)
	if err != nil {
		return fmt.Sprintf("联网确认 %s 失败，不能证明远程还有这个提交：%v", it.Remote, firstLine(err.Error()))
	}
	sha, _, _ := strings.Cut(strings.TrimSpace(out), "\t")
	if sha == "" {
		return fmt.Sprintf("远程 %s 上已经没有 %s，不能证明安全", it.Remote, it.RemoteRef)
	}
	if sha == it.Head {
		return ""
	}
	if _, err := gitx.Run(ctx, root, "cat-file", "-e", sha+"^{commit}"); err != nil {
		return fmt.Sprintf("远程 %s 的 %s 已变成本地没有的提交 %s，先 fetch 再来", it.Remote, it.RemoteRef, sha[:8])
	}
	_, code, err := gitx.Exec(ctx, root, gitx.Opts{}, "merge-base", "--is-ancestor", it.Head, sha)
	if code == 0 {
		return ""
	}
	if code == 1 {
		return fmt.Sprintf("远程 %s 的 %s 现在是 %s，已不包含这个提交（可能被强推过）", it.Remote, it.RemoteRef, sha[:8])
	}
	return fmt.Sprintf("确认远程提交失败：%v", err)
}

func outcome(kind, name string, err error, ok string) Result {
	if err != nil {
		return Result{kind, name, false, "失败：" + firstLine(err.Error())}
	}
	return Result{kind, name, true, ok}
}

func refDesc(w judge.Item) string {
	if w.Branch != "" {
		return fmt.Sprintf("（%s @ %s）", w.Branch, w.Head)
	}
	return fmt.Sprintf("（分离头指针 @ %s）", w.Head)
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
