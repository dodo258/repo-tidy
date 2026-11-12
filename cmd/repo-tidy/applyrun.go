package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/dodo258/repo-tidy/internal/apply"
	"github.com/dodo258/repo-tidy/internal/judge"
	"github.com/dodo258/repo-tidy/internal/report"
)

// applyEntry 是删除阶段的一行：一项的结果，或整个仓库出的错（Err 非空）。
type applyEntry struct {
	Repo string
	Err  string
	Res  apply.Result
}

type applyRun struct {
	logPath string
	entries []applyEntry
	target  int // 打算删几项：没给 --select 是全部可删项，给了是勾选的项数
	done    int
	failed  bool
}

// readWants 读 --select 的文件（- 是标准输入），按仓库分组，并按出现顺序返回仓库列表。
func readWants(from string) (map[string][]apply.Want, []string, error) {
	r := io.Reader(os.Stdin)
	if from != "-" {
		f, err := os.Open(from)
		if err != nil {
			return nil, nil, fmt.Errorf("打开 --select 文件：%w", err)
		}
		defer f.Close()
		r = f
	}
	list, err := apply.ReadWants(r)
	if err != nil {
		return nil, nil, err
	}
	if len(list) == 0 {
		return nil, nil, fmt.Errorf("--select 里没有要删的项")
	}
	wants := map[string][]apply.Want{}
	var repos []string
	for _, w := range list {
		if _, ok := wants[w.Repo]; !ok {
			repos = append(repos, w.Repo)
		}
		wants[w.Repo] = append(wants[w.Repo], w)
	}
	return wants, repos, nil
}

// applyAll 删除 reps 里可删的项；wants 不为 nil 时只删勾选的。日志只在真有要删的项时才建。
func applyAll(ctx context.Context, reps []report.Repo, wants map[string][]apply.Want, now time.Time, home string) (applyRun, error) {
	var run applyRun
	type job struct {
		rep     judge.RepoReport
		skipped []apply.Result
	}
	var jobs []job
	scanned := map[string]bool{}
	for _, r := range reps {
		scanned[r.Root] = true
		j := job{rep: r.RepoReport}
		if wants != nil {
			ws := wants[r.Root]
			if len(ws) == 0 {
				continue
			}
			run.target += len(ws)
			if r.Err != "" {
				for _, w := range ws {
					j.skipped = append(j.skipped, apply.Result{Kind: w.Kind, Name: w.Name, Msg: "仓库没扫成：" + r.Err})
				}
				j.rep = judge.RepoReport{Root: r.Root}
			} else {
				j.rep, j.skipped = apply.Select(r.RepoReport, ws)
			}
		} else {
			if r.Err != "" {
				continue
			}
			run.target += countRemovable([]report.Repo{r})
		}
		jobs = append(jobs, j)
	}
	var missing []string
	for repo := range wants {
		if !scanned[repo] {
			missing = append(missing, repo)
		}
	}
	sort.Strings(missing)
	for _, repo := range missing {
		run.target += len(wants[repo])
		for _, w := range wants[repo] {
			run.entries = append(run.entries, applyEntry{Repo: repo, Res: apply.Result{Kind: w.Kind, Name: w.Name, Msg: "不在这次扫描的仓库里"}})
		}
	}

	var log *os.File
	for _, j := range jobs {
		for _, s := range j.skipped {
			run.entries = append(run.entries, applyEntry{Repo: j.rep.Root, Res: s})
		}
		if countRemovable([]report.Repo{{RepoReport: j.rep}}) == 0 {
			continue
		}
		if log == nil {
			p, f, err := openLog(now, home)
			if err != nil {
				return run, err
			}
			defer f.Close()
			fmt.Fprintf(f, "# repo-tidy --apply %s\n", now.Format(time.RFC3339))
			run.logPath, log = p, f
		}
		results, err := apply.Apply(ctx, j.rep, log)
		if err != nil {
			run.entries = append(run.entries, applyEntry{Repo: j.rep.Root, Err: err.Error()})
			run.failed = true
			continue
		}
		for _, res := range results {
			if res.Done {
				run.done++
			}
			run.entries = append(run.entries, applyEntry{Repo: j.rep.Root, Res: res})
		}
	}
	if log != nil {
		if err := log.Sync(); err != nil {
			return run, fmt.Errorf("写日志：%w", err)
		}
	}
	return run, nil
}

func writeApplyText(w io.Writer, run applyRun) {
	if run.target == 0 {
		fmt.Fprintln(w, "没有可删的项。")
		return
	}
	fmt.Fprintln(w, "\n开始删除：")
	for _, e := range run.entries {
		if e.Err != "" {
			fmt.Fprintf(w, "  %s：%s\n", e.Repo, e.Err)
			continue
		}
		mark := "没删"
		if e.Res.Done {
			mark = "已删"
		}
		fmt.Fprintf(w, "  %s  %s %s：%s\n", mark, kindName(e.Res.Kind), e.Res.Name, e.Res.Msg)
	}
	if run.logPath == "" {
		fmt.Fprintf(w, "\n删了 %d 项。\n", run.done)
		return
	}
	fmt.Fprintf(w, "\n删了 %d 项。恢复命令在 %s\n", run.done, run.logPath)
}

type applyJSONDoc struct {
	Version int               `json:"version"`
	Log     string            `json:"log"`
	Target  int               `json:"target"`
	Deleted int               `json:"deleted"`
	Results []applyJSONResult `json:"results"`
	Errors  []applyJSONError  `json:"errors"`
}

type applyJSONResult struct {
	Repo    string `json:"repo"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Done    bool   `json:"done"`
	Message string `json:"message"`
}

type applyJSONError struct {
	Repo  string `json:"repo"`
	Error string `json:"error"`
}

func writeApplyJSON(w io.Writer, run applyRun) error {
	doc := applyJSONDoc{Version: report.JSONVersion, Log: run.logPath, Target: run.target, Deleted: run.done,
		Results: []applyJSONResult{}, Errors: []applyJSONError{}}
	for _, e := range run.entries {
		if e.Err != "" {
			doc.Errors = append(doc.Errors, applyJSONError{Repo: e.Repo, Error: e.Err})
			continue
		}
		doc.Results = append(doc.Results, applyJSONResult{Repo: e.Repo, Kind: e.Res.Kind, Name: e.Res.Name, Done: e.Res.Done, Message: e.Res.Msg})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(doc)
}
