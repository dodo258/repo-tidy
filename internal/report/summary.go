package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/dodo258/repo-tidy/internal/judge"
)

// Overview 是所有仓库的汇总数字。
type Overview struct {
	Repos, Failed, Clean     int
	Worktrees, Branches      int
	RemovableWT, RemovableBr int
	KeptWT, KeptBr           int
	Unmerged                 int   // 留着的项里有未合并内容的
	FreeBytes                int64 // 可删 worktree 的目录大小之和
	Unmeasured               int   // 可删 worktree 里没量到大小的个数
}

// Removable 是可删项总数。
func (o Overview) Removable() int { return o.RemovableWT + o.RemovableBr }

// Kept 是留着的项总数。
func (o Overview) Kept() int { return o.KeptWT + o.KeptBr }

// Mess 是一个仓库乱的程度：可删项数加留着的未合并项数。
func Mess(r Repo) int {
	n := 0
	for _, it := range items(r) {
		if it.Removable || it.Unmerged {
			n++
		}
	}
	return n
}

// clean：扫成功、判定过、没有可删项也没有未合并的留着项，报告里折叠成一行。
func clean(r Repo) bool { return r.Err == "" && r.Note == "" && Mess(r) == 0 }

// Sort 按乱的程度从多到少排；扫描失败的排在乱的后面、干净的前面；同级按路径。
func Sort(reps []Repo) []Repo {
	rank := func(r Repo) int {
		switch {
		case clean(r):
			return 0
		case r.Err != "":
			return 1
		}
		return 2
	}
	out := append([]Repo(nil), reps...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra > rb
		}
		if ma, mb := Mess(a), Mess(b); ma != mb {
			return ma > mb
		}
		return a.Root < b.Root
	})
	return out
}

// Summarize 汇总所有仓库。腾出的空间只算可删的 worktree 目录（体检量出的大小）；
// 删分支只删引用，对象要等 gc 且可能被别处引用，不算。
func Summarize(reps []Repo) Overview {
	o := Overview{Repos: len(reps)}
	for _, r := range reps {
		if r.Err != "" {
			o.Failed++
			continue
		}
		if clean(r) {
			o.Clean++
		}
		o.Worktrees += len(r.Worktrees)
		o.Branches += len(r.Branches)
		sizes := worktreeSizes(r)
		for _, it := range r.Worktrees {
			if !it.Removable {
				o.KeptWT++
				continue
			}
			o.RemovableWT++
			if it.Basis == judge.Prunable {
				continue // 目录已不在，不占空间
			}
			if sz, ok := sizes[it.Name]; ok && !sz.Missing {
				o.FreeBytes += sz.Bytes
			} else {
				o.Unmeasured++
			}
		}
		for _, it := range r.Branches {
			if it.Removable {
				o.RemovableBr++
			} else {
				o.KeptBr++
			}
		}
		for _, it := range items(r) {
			if !it.Removable && it.Unmerged {
				o.Unmerged++
			}
		}
	}
	return o
}

// overview 输出报告开头的总览。
func overview(w io.Writer, o Overview) {
	scanned := fmt.Sprintf("扫了 %d 个仓库", o.Repos)
	var notes []string
	if o.Clean > 0 {
		notes = append(notes, fmt.Sprintf("%d 个干净", o.Clean))
	}
	if o.Failed > 0 {
		notes = append(notes, fmt.Sprintf("%d 个扫描失败", o.Failed))
	}
	if len(notes) > 0 {
		scanned += "（" + strings.Join(notes, "、") + "）"
	}
	fmt.Fprintf(w, "\n总览  %s · 附加 worktree %d 个 · 本地分支 %d 个\n", scanned, o.Worktrees, o.Branches)

	free := "能腾出 " + Bytes(o.FreeBytes)
	if o.Unmeasured > 0 {
		free += fmt.Sprintf("（另有 %d 个 worktree 没量到大小）", o.Unmeasured)
	}
	fmt.Fprintf(w, "      可删 %d 项（worktree %d、分支 %d），%s\n", o.Removable(), o.RemovableWT, o.RemovableBr, free)
	kept := fmt.Sprintf("      留着 %d 项（worktree %d、分支 %d）", o.Kept(), o.KeptWT, o.KeptBr)
	if o.Unmerged > 0 {
		kept += fmt.Sprintf("，其中 %d 项有未合并内容", o.Unmerged)
	}
	fmt.Fprintln(w, kept)
}

func items(r Repo) []judge.Item {
	return append(append([]judge.Item(nil), r.Worktrees...), r.Branches...)
}
