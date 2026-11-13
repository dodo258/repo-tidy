// Package report 把判定结果渲染成终端文字。
package report

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dodo258/repo-tidy/internal/health"
	"github.com/dodo258/repo-tidy/internal/judge"
	"github.com/dodo258/repo-tidy/internal/msg"
)

// Repo 是一个仓库的判定结果加体检结果。
type Repo struct {
	judge.RepoReport
	Health *health.Report // nil 表示没体检
}

// manyRemovable：一个仓库的可删项超过这么多，先写一行按依据分类的计数。
const manyRemovable = 10

// Text 先输出总览，再按乱的程度从多到少输出各仓库；干净的仓库每个折成一行放在最后。
// home 用来把路径缩写成 ~。可删项默认一行一个、证据写短形式；verbose 时证据另起一行写全。
// 文字用 lang 这种语言；判定里的证据和原因是 judge 按同一语言写好的。
// reps 是已经按 f 筛过的结果，f 只用来在总览里注明条件。
func Text(w io.Writer, reps []Repo, now time.Time, home string, verbose bool, lang msg.Lang, f Filter) {
	t := msg.For(lang)
	overview(w, t, Summarize(reps), f)
	var cleans []Repo
	for _, r := range Sort(reps) {
		if clean(r) {
			cleans = append(cleans, r)
			continue
		}
		fmt.Fprintln(w)
		if r.Err != "" {
			fmt.Fprintf(w, t.ScanFailed, tilde(r.Root, home), r.Err)
			continue
		}
		head := repoHead(t, r, home)
		if r.DefaultBranch != "" {
			head += fmt.Sprintf(t.DefaultBranch, r.DefaultBranch)
		}
		fmt.Fprintln(w, head)
		if r.Note != "" {
			fmt.Fprintf(w, "  %s\n", r.Note)
		}
		if r.Health != nil {
			checkup(w, t, *r.Health, now)
		}
		if n := Removable(r); !verbose && n > manyRemovable {
			fmt.Fprintf(w, t.ManyRemovable, t.Items.N(n), basisCounts(t, r))
		}
		section(w, t, t.SectionWorktrees, r.Worktrees, worktreeSizes(r), r, home, now, verbose)
		section(w, t, t.SectionBranches, r.Branches, nil, r, home, now, verbose)
	}
	if len(cleans) == 0 {
		return
	}
	fmt.Fprintf(w, t.CleanRepos, len(cleans))
	for _, r := range cleans {
		line := "  " + repoHead(t, r, home)
		if r.Health != nil {
			if flags := attention(t, *r.Health); len(flags) > 0 {
				line += fmt.Sprintf(t.Attention, strings.Join(flags, t.ListSep))
			}
		}
		fmt.Fprintln(w, line)
	}
}

// repoHead 是仓库那一行的开头：路径、附加 worktree 数、本地分支数。
func repoHead(t *msg.Text, r Repo, home string) string {
	return tilde(r.Root, home) + "  " + t.ExtraWorktrees.N(len(r.Worktrees)) + " · " + t.LocalBranches.N(len(r.Branches))
}

// worktreeSizes 按路径给出体检量到的 worktree 大小；没体检时为空。
func worktreeSizes(r Repo) map[string]health.Size {
	sizes := map[string]health.Size{}
	if r.Health != nil {
		for _, s := range r.Health.Worktrees {
			sizes[s.Path] = s
		}
	}
	return sizes
}

// section 列出一组 worktree 或分支；sizes 按路径给出 worktree 的占用，有就附在行尾。
// 可删项默认一行（行尾是短证据），verbose 时证据另起一行写全；留着的项总是逐条写原因。
func section(w io.Writer, t *msg.Text, title string, items []judge.Item, sizes map[string]health.Size, r Repo, home string, now time.Time, verbose bool) {
	if len(items) == 0 {
		return
	}
	var can, keep int
	for _, it := range items {
		if it.Removable {
			can++
		} else {
			keep++
		}
	}
	fmt.Fprintf(w, t.SectionHead, title, can, keep)
	labelWidth := max(width(t.Removable), width(t.Keep))
	// 可删的排前面，同类里按最后活动时间从旧到新
	for _, removable := range []bool{true, false} {
		for _, it := range sortedByAge(items) {
			if it.Removable != removable {
				continue
			}
			label := t.Keep
			if it.Removable {
				label = t.Removable
			}
			label = padTo(label, labelWidth)
			name := it.Name
			if it.Kind == judge.KindWorktree {
				name = shortPath(it.Name, r.Root, home)
				if it.Branch != "" {
					name += "  [" + it.Branch + "]"
				} else {
					name += "  [" + t.Detached + "]"
				}
			}
			tail := age(t, it.LastActive, now)
			if sz, ok := sizes[it.Name]; ok && !sz.Missing {
				tail = strings.TrimPrefix(tail+" · "+Bytes(sz.Bytes), " · ")
			}
			if it.Removable && !verbose {
				fmt.Fprintln(w, strings.Join(nonEmpty("    "+label, name, tail, shortEvidence(it)), "  "))
				continue
			}
			fmt.Fprintf(w, "    %s  %s  %s\n", label, name, tail)
			if it.Removable {
				fmt.Fprintf(w, "          %s\n", it.Evidence)
			}
			for _, reason := range it.Reasons {
				fmt.Fprintf(w, "          %s\n", reason)
			}
		}
	}
}

func nonEmpty(ss ...string) []string {
	out := ss[:0]
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func sortedByAge(items []judge.Item) []judge.Item {
	out := append([]judge.Item(nil), items...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastActive.Before(out[j].LastActive) })
	return out
}

func age(t *msg.Text, at, now time.Time) string {
	if at.IsZero() {
		return ""
	}
	d := now.Sub(at)
	if d < 24*time.Hour {
		return t.ActiveToday
	}
	return t.IdleDays.N(int(d.Hours() / 24))
}

// shortPath：在仓库里的写相对路径，否则把家目录缩成 ~。
func shortPath(p, root, home string) string {
	if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return tilde(p, home)
}

func tilde(p, home string) string {
	if home != "" && (p == home || strings.HasPrefix(p, home+string(filepath.Separator))) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
