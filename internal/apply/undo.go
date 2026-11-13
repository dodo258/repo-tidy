package apply

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dodo258/repo-tidy/internal/gitx"
	"github.com/dodo258/repo-tidy/internal/judge"
	"github.com/dodo258/repo-tidy/internal/msg"
)

// Restore 是恢复日志里的一条恢复命令：把删掉的一个分支或 worktree 建回来。
type Restore struct {
	Repo   string
	Kind   string // judge.KindBranch 或 judge.KindWorktree
	Name   string // 分支名，或 worktree 的路径
	Head   string // 分支、分离头指针 worktree 指向的提交；检出分支的 worktree 为空
	Branch string // worktree 检出的分支
}

// 撤销一项的结果。
const (
	UndoReady    = "ready"    // 只看模式：能恢复
	UndoRestored = "restored" // 恢复了
	UndoExists   = "exists"   // 分支或目录已经在了，跳过
	UndoLost     = "lost"     // 提交对象已不在仓库里（多半被 gc 清掉），恢复不了
	UndoFailed   = "failed"   // 仓库不在、worktree 要的分支不在，或 git 报错
)

// UndoResult 是一项撤销的结果。
type UndoResult struct {
	Restore
	Status string
	Msg    string
}

// ReadLog 从 --apply 写的恢复日志里读出恢复命令（「恢复：」或「restore: 」开头的行，两种语言的日志都认）。
// 恢复命令是 Apply 用 quote 写的 git 命令，这里按同样的引号规则拆开，不经过 shell。
func ReadLog(r io.Reader, lang msg.Lang) ([]Restore, error) {
	t := msg.For(lang)
	var prefixes []string
	for _, l := range []msg.Lang{msg.ZH, msg.EN} {
		prefixes = append(prefixes, strings.TrimSuffix(fmt.Sprintf(msg.For(l).LogRestore, ""), "\n"))
	}
	var out []Restore
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		for _, p := range prefixes {
			cmd, ok := strings.CutPrefix(line, p)
			if !ok {
				continue
			}
			it, err := parseRestore(cmd)
			if err != nil {
				return nil, fmt.Errorf(t.UndoBadLine, n, err)
			}
			out = append(out, it)
			break
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf(t.UndoReadLog, err)
	}
	if len(out) == 0 {
		return nil, errors.New(t.UndoEmptyLog)
	}
	return out, nil
}

// parseRestore 认 Apply 写的三种恢复命令：
//
//	git -C <仓库> branch <分支> <提交>
//	git -C <仓库> worktree add <路径> <分支>
//	git -C <仓库> worktree add --detach <路径> <提交>
func parseRestore(cmd string) (Restore, error) {
	w, err := shellWords(cmd)
	if err != nil {
		return Restore{}, err
	}
	// Apply 写的仓库和 worktree 路径都是绝对路径，分支名不会以 - 开头（git 不允许）：
	// 不合这些的不认，免得被 git 当成选项。
	if len(w) < 4 || w[0] != "git" || w[1] != "-C" || !filepath.IsAbs(w[2]) {
		return Restore{}, fmt.Errorf("%q", cmd)
	}
	repo, rest := w[2], w[3:]
	switch {
	case len(rest) == 3 && rest[0] == "branch" && isBranch(rest[1]) && isCommit(rest[2]):
		return Restore{Repo: repo, Kind: judge.KindBranch, Name: rest[1], Head: rest[2]}, nil
	case len(rest) == 5 && rest[0] == "worktree" && rest[1] == "add" && rest[2] == "--detach" && filepath.IsAbs(rest[3]) && isCommit(rest[4]):
		return Restore{Repo: repo, Kind: judge.KindWorktree, Name: rest[3], Head: rest[4]}, nil
	case len(rest) == 4 && rest[0] == "worktree" && rest[1] == "add" && filepath.IsAbs(rest[2]) && isBranch(rest[3]):
		return Restore{Repo: repo, Kind: judge.KindWorktree, Name: rest[2], Branch: rest[3]}, nil
	}
	return Restore{}, fmt.Errorf("%q", cmd)
}

// shellWords 按 POSIX shell 的单引号和反斜杠规则拆词（quote 写出的就是这种），不展开变量。
func shellWords(s string) ([]string, error) {
	var words []string
	var cur strings.Builder
	inWord := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return nil, errors.New("unterminated quote")
			}
			cur.WriteString(s[i+1 : i+1+j])
			i += j + 1
			inWord = true
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			inWord = true
		case c == ' ' || c == '\t':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words, nil
}

func isBranch(s string) bool { return s != "" && !strings.HasPrefix(s, "-") }

func isCommit(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

// Undo 按日志恢复删掉的项：每个仓库先建回分支，再建回 worktree（worktree 要检出的分支得先在）。
// doIt 为 false 时只看：查每一项现在能不能恢复，什么都不改。
// 分支或目录已经在了就跳过；提交对象已不在仓库里的报出来。
func Undo(ctx context.Context, items []Restore, doIt bool, lang msg.Lang) []UndoResult {
	t := msg.For(lang)
	var repos []string
	byRepo := map[string][]Restore{}
	for _, it := range items {
		if _, ok := byRepo[it.Repo]; !ok {
			repos = append(repos, it.Repo)
		}
		byRepo[it.Repo] = append(byRepo[it.Repo], it)
	}
	var out []UndoResult
	for _, repo := range repos {
		out = append(out, undoRepo(ctx, t, repo, byRepo[repo], doIt)...)
	}
	return out
}

func undoRepo(ctx context.Context, t *msg.Text, repo string, items []Restore, doIt bool) []UndoResult {
	var out []UndoResult
	if _, err := gitx.Run(ctx, repo, "rev-parse", "--git-dir"); err != nil {
		for _, kind := range []string{judge.KindBranch, judge.KindWorktree} {
			for _, it := range items {
				if it.Kind == kind {
					out = append(out, UndoResult{it, UndoFailed, fmt.Sprintf(t.UndoNoRepo, firstLine(err.Error()))})
				}
			}
		}
		return out
	}
	ready := map[string]bool{} // 只看模式下会建回的分支：它的 worktree 也算能恢复
	for _, it := range items {
		if it.Kind != judge.KindBranch {
			continue
		}
		r := undoBranch(ctx, t, it, doIt)
		if r.Status == UndoReady {
			ready[it.Name] = true
		}
		out = append(out, r)
	}
	for _, it := range items {
		if it.Kind == judge.KindWorktree {
			out = append(out, undoWorktree(ctx, t, it, doIt, ready))
		}
	}
	return out
}

func undoBranch(ctx context.Context, t *msg.Text, it Restore, doIt bool) UndoResult {
	short := it.Head[:8]
	if now, ok := branchHead(ctx, it.Repo, it.Name); ok {
		if now == it.Head {
			return UndoResult{it, UndoExists, t.UndoBranchSame}
		}
		return UndoResult{it, UndoExists, fmt.Sprintf(t.UndoBranchMoved, now[:8])}
	}
	if !hasCommit(ctx, it.Repo, it.Head) {
		return UndoResult{it, UndoLost, fmt.Sprintf(t.UndoLostCommit, short)}
	}
	if !doIt {
		return UndoResult{it, UndoReady, fmt.Sprintf(t.UndoWillBranch, short)}
	}
	if _, err := gitx.Run(ctx, it.Repo, "branch", "--", it.Name, it.Head); err != nil {
		return UndoResult{it, UndoFailed, fmt.Sprintf(t.Failed, firstLine(err.Error()))}
	}
	return UndoResult{it, UndoRestored, fmt.Sprintf(t.UndoDidBranch, short)}
}

func undoWorktree(ctx context.Context, t *msg.Text, it Restore, doIt bool, ready map[string]bool) UndoResult {
	if _, err := os.Lstat(it.Name); err == nil {
		return UndoResult{it, UndoExists, t.UndoDirExists}
	}
	var what string
	args := []string{"worktree", "add"}
	if it.Branch == "" {
		if !hasCommit(ctx, it.Repo, it.Head) {
			return UndoResult{it, UndoLost, fmt.Sprintf(t.UndoLostCommit, it.Head[:8])}
		}
		what = fmt.Sprintf(t.UndoDetached, it.Head[:8])
		args = append(args, "--detach", it.Name, it.Head)
	} else {
		if _, ok := branchHead(ctx, it.Repo, it.Branch); !ok && !ready[it.Branch] {
			return UndoResult{it, UndoFailed, fmt.Sprintf(t.UndoNoBranch, it.Branch)}
		}
		what = fmt.Sprintf(t.UndoOnBranch, it.Branch)
		args = append(args, it.Name, it.Branch)
	}
	if !doIt {
		return UndoResult{it, UndoReady, fmt.Sprintf(t.UndoWillWorktree, what)}
	}
	if _, err := gitx.Run(ctx, it.Repo, args...); err != nil {
		return UndoResult{it, UndoFailed, fmt.Sprintf(t.Failed, firstLine(err.Error()))}
	}
	return UndoResult{it, UndoRestored, fmt.Sprintf(t.UndoDidWorktree, what)}
}

// branchHead 返回本地分支现在指向的提交；分支不存在时 ok 为 false。
func branchHead(ctx context.Context, repo, name string) (string, bool) {
	out, err := gitx.Run(ctx, repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+name+"^{commit}")
	return out, err == nil && out != ""
}

// hasCommit 看仓库里还有没有这个提交对象。
func hasCommit(ctx context.Context, repo, sha string) bool {
	_, err := gitx.Run(ctx, repo, "cat-file", "-e", sha+"^{commit}")
	return err == nil
}
