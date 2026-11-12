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
)

// Repo 是一个仓库的判定结果加体检结果。
type Repo struct {
	judge.RepoReport
	Health *health.Report // nil 表示没体检
}

// Text 先输出总览，再按乱的程度从多到少输出各仓库；干净的仓库每个折成一行放在最后。
// home 用来把路径缩写成 ~。
func Text(w io.Writer, reps []Repo, now time.Time, home string) {
	overview(w, Summarize(reps))
	var cleans []Repo
	for _, r := range Sort(reps) {
		if clean(r) {
			cleans = append(cleans, r)
			continue
		}
		fmt.Fprintln(w)
		if r.Err != "" {
			fmt.Fprintf(w, "%s  扫描失败：%s\n", tilde(r.Root, home), r.Err)
			continue
		}
		head := fmt.Sprintf("%s  %d 个附加 worktree · %d 个本地分支", tilde(r.Root, home), len(r.Worktrees), len(r.Branches))
		if r.DefaultBranch != "" {
			head += " · 默认分支 " + r.DefaultBranch
		}
		fmt.Fprintln(w, head)
		if r.Note != "" {
			fmt.Fprintf(w, "  %s\n", r.Note)
		}
		if r.Health != nil {
			checkup(w, *r.Health, now)
		}
		section(w, "worktree", r.Worktrees, worktreeSizes(r), r.Root, home, now)
		section(w, "本地分支", r.Branches, nil, r.Root, home, now)
	}
	if len(cleans) == 0 {
		return
	}
	fmt.Fprintf(w, "\n干净的仓库 %d 个（没有可删项，也没有未合并的内容）：\n", len(cleans))
	for _, r := range cleans {
		line := fmt.Sprintf("  %s  %d 个附加 worktree · %d 个本地分支", tilde(r.Root, home), len(r.Worktrees), len(r.Branches))
		if r.Health != nil {
			if flags := attention(*r.Health); len(flags) > 0 {
				line += " · 注意：" + strings.Join(flags, "、")
			}
		}
		fmt.Fprintln(w, line)
	}
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
func section(w io.Writer, title string, items []judge.Item, sizes map[string]health.Size, root, home string, now time.Time) {
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
	fmt.Fprintf(w, "  %s  可删 %d · 留着 %d\n", title, can, keep)
	// 可删的排前面，同类里按最后活动时间从旧到新
	for _, removable := range []bool{true, false} {
		for _, it := range sortedByAge(items) {
			if it.Removable != removable {
				continue
			}
			label := "留着"
			if it.Removable {
				label = "可删"
			}
			name := it.Name
			if it.Kind == judge.KindWorktree {
				name = shortPath(it.Name, root, home)
				if it.Branch != "" {
					name += "  [" + it.Branch + "]"
				} else {
					name += "  [分离头指针]"
				}
			}
			tail := age(it.LastActive, now)
			if sz, ok := sizes[it.Name]; ok && !sz.Missing {
				tail = strings.TrimPrefix(tail+" · "+Bytes(sz.Bytes), " · ")
			}
			fmt.Fprintf(w, "    %s  %s  %s\n", label, name, tail)
			if it.Removable {
				fmt.Fprintf(w, "          %s\n", it.Evidence)
			}
			for _, r := range it.Reasons {
				fmt.Fprintf(w, "          %s\n", r)
			}
		}
	}
}

func sortedByAge(items []judge.Item) []judge.Item {
	out := append([]judge.Item(nil), items...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastActive.Before(out[j].LastActive) })
	return out
}

func age(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < 24*time.Hour:
		return "今天动过"
	default:
		return fmt.Sprintf("%d 天没动", int(d.Hours()/24))
	}
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
