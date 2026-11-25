package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/dodo258/repo-tidy/internal/discover"
	"github.com/dodo258/repo-tidy/internal/gitx"
	"github.com/dodo258/repo-tidy/internal/health"
	"github.com/dodo258/repo-tidy/internal/judge"
	"github.com/dodo258/repo-tidy/internal/msg"
	"github.com/dodo258/repo-tidy/internal/report"
)

// repoWorkers 是同时扫几个仓库。每个仓库里判定和体检并行；量大小的读目录另有全局上限（health.readSlots）。
const repoWorkers = 4

// inspectAll 判定并体检每个仓库，dirs 是 pickRepos 选好的。每开始扫一个仓库调一次 progress（可以为 nil）。
func inspectAll(ctx context.Context, dirs []string, hopts health.Options, opts judge.Options, progress func(i, n int, root string)) []report.Repo {
	reps := make([]report.Repo, len(dirs))
	var mu sync.Mutex
	started := 0
	var wg sync.WaitGroup
	sem := make(chan struct{}, repoWorkers)
	for i, d := range dirs {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			if progress != nil {
				mu.Lock()
				started++
				progress(started, len(dirs), d)
				mu.Unlock()
			}
			var rep judge.RepoReport
			var h health.Report
			var inner sync.WaitGroup
			inner.Go(func() {
				var err error
				if rep, err = judge.Inspect(ctx, d, opts); err != nil {
					rep = judge.RepoReport{Root: d, Err: err.Error()}
				}
			})
			inner.Go(func() {
				var err error
				if h, err = health.Inspect(ctx, d, hopts, opts.Lang); err != nil {
					h = health.Report{Root: d, Errs: []string{fmt.Sprintf(msg.For(opts.Lang).CheckupFailed, err)}}
				}
			})
			inner.Wait()
			reps[i] = report.Repo{RepoReport: rep, Health: &h}
		})
	}
	wg.Wait()
	return reps
}

// picked 是 pickRepos 的结果。
type picked struct {
	dirs    []string        // 要扫的目录：每个仓库一个，保留第一次出现的位置
	skipped map[string]bool // 被排除的候选目录和这些仓库的工作区路径（--select 用它认出被排除的仓库）
	nSkip   int             // 跳过了几个仓库，同一个仓库只计一次
}

// pickRepos 按主工作区去重，并去掉被排除的仓库。discover 会把另一个仓库的附加 worktree
// （目录里有 .git 文件）也当成候选，不去重的话同一个仓库要整个扫两遍。候选目录本身，
// 或这个仓库的任何一个工作区（主工作区、附加 worktree）落在排除路径里，整个仓库都不扫，
// 这样排除路径下的东西不会被报告，也不会被 --apply 删。列不了 worktree 的目录原样留着，扫的时候报错。
func pickRepos(ctx context.Context, dirs []string, ex discover.Excludes) picked {
	roots := make([]string, len(dirs))
	paths := make([][]string, len(dirs)) // 每个候选的所有工作区路径（含候选目录本身）
	var wg sync.WaitGroup
	sem := make(chan struct{}, repoWorkers)
	for i, d := range dirs {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			roots[i], paths[i] = d, []string{d}
			if wts, err := gitx.Worktrees(ctx, d); err == nil && len(wts) > 0 {
				roots[i] = wts[0].Path
				for _, w := range wts {
					paths[i] = append(paths[i], w.Path)
				}
			}
		})
	}
	wg.Wait()
	excluded := map[string]bool{} // 主工作区 → 被排除
	if ex.Any() {
		for i := range dirs {
			for _, p := range paths[i] {
				if ex.Match(p) {
					excluded[roots[i]] = true
					break
				}
			}
		}
	}
	out := picked{skipped: map[string]bool{}}
	seen := map[string]bool{}
	for i, d := range dirs {
		if excluded[roots[i]] {
			for _, p := range paths[i] {
				out.skipped[p] = true
			}
		}
		if seen[roots[i]] {
			continue
		}
		seen[roots[i]] = true
		if excluded[roots[i]] {
			out.nSkip++
			continue
		}
		out.dirs = append(out.dirs, d)
	}
	return out
}

// newProgress 决定扫描时怎么显示进度：lines 为真时每个仓库往 w 写一行（给 App 读）；
// 否则只在文字报告（text）且 w 是终端时在同一行刷新，扫完调 done 擦掉。不显示时返回 nil。
// 进度行的格式是「<一个词> 当前/总数 名字」，App 按这个格式认（不看前面那个词）。
func newProgress(w io.Writer, t *msg.Text, lines, text bool) *progressOut {
	switch {
	case lines:
		return &progressOut{w: w, format: t.Scanning}
	case text && isTerminal(w):
		return &progressOut{w: w, format: t.Scanning, inPlace: true}
	}
	return nil
}

type progressOut struct {
	w       io.Writer
	format  string
	inPlace bool
}

func (p *progressOut) show(i, n int, root string) {
	line := fmt.Sprintf(p.format, i, n, filepath.Base(root))
	if p.inPlace {
		fmt.Fprint(p.w, "\r\033[K"+line)
		return
	}
	fmt.Fprintln(p.w, line)
}

func (p *progressOut) done() {
	if p.inPlace {
		fmt.Fprint(p.w, "\r\033[K")
	}
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
