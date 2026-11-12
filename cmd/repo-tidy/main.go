// repo-tidy 扫描仓库里的附加 worktree 和本地分支，判定哪些能证明安全可删，
// 并给每个仓库做体检（占用空间、未提交改动、stash、上游、backup 是否推到最新）。
// 默认只报告；加 --apply 才删，删前逐项重新确认，恢复命令写进日志。
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"time"

	"github.com/dodo258/repo-tidy/internal/apply"
	"github.com/dodo258/repo-tidy/internal/discover"
	"github.com/dodo258/repo-tidy/internal/health"
	"github.com/dodo258/repo-tidy/internal/judge"
	"github.com/dodo258/repo-tidy/internal/report"
)

var version = "0.1.0"

const usage = `用法：repo-tidy [--apply | --json] [--online] [路径...]

扫描仓库里的附加 worktree 和本地分支，分成「可删」（附证据）和「留着」（附原因）；
每个仓库另有一段体检：占用空间、主工作区未提交改动、stash、当前分支相对上游、
最后一次提交、backup 远程是不是推到最新（只报不动）。
路径可以是一个仓库，也可以是一个目录（扫它下面一层的仓库）。
不给路径时扫 $REPO_TIDY_ROOT，没设就扫 ~/projects。

  --apply    删除标「可删」的项。删前逐项重新判定；靠「已推到远程」可删的先联网确认。
             恢复命令写进 ${XDG_STATE_HOME:-~/.local/state}/repo-tidy/apply-<时间>.log
  --json     把同样的判定和体检结果输出成 JSON（字段见 docs/reference.md），不能和 --apply 一起用。
  --online   体检时联网（git ls-remote）查 backup 的实际状态；默认按本地远程跟踪引用比对。
  --version  显示版本
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("repo-tidy", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	doApply := fs.Bool("apply", false, "")
	online := fs.Bool("online", false, "")
	asJSON := fs.Bool("json", false, "")
	selectFrom := fs.String("select", "", "")
	showVersion := fs.Bool("version", false, "")
	var paths []string
	for { // 允许参数和路径交替出现
		if err := fs.Parse(args); err != nil {
			if err == flag.ErrHelp {
				return 0
			}
			return 2
		}
		if fs.NArg() == 0 {
			break
		}
		paths = append(paths, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if *showVersion {
		fmt.Fprintln(stdout, "repo-tidy", version)
		return 0
	}
	var wants map[string][]apply.Want // 仓库 → 勾选的项；nil 表示删全部可删项
	if *selectFrom != "" {
		if !*doApply {
			fmt.Fprintln(stderr, "repo-tidy：--select 只能和 --apply 一起用")
			return 2
		}
		var repos []string
		var err error
		if wants, repos, err = readWants(*selectFrom); err != nil {
			fmt.Fprintln(stderr, "repo-tidy：", err)
			return 2
		}
		if len(paths) == 0 {
			paths = repos
		}
	}
	home, _ := os.UserHomeDir()
	if len(paths) == 0 {
		root := os.Getenv("REPO_TIDY_ROOT")
		if root == "" {
			root = filepath.Join(home, "projects")
		}
		paths = []string{root}
	}
	dirs, err := discover.Repos(paths)
	if err != nil {
		fmt.Fprintln(stderr, "repo-tidy：", err)
		return 2
	}
	if len(dirs) == 0 {
		fmt.Fprintln(stderr, "repo-tidy：这些路径下没有找到 git 仓库")
		return 1
	}

	now := time.Now()
	reps := inspectAll(ctx, dirs, *online)
	failed := false
	for _, r := range reps {
		failed = failed || r.Err != ""
	}
	if *asJSON && !*doApply {
		if err := report.JSON(stdout, reps, now); err != nil {
			fmt.Fprintln(stderr, "repo-tidy：写 JSON：", err)
			return 1
		}
		return exitCode(failed)
	}
	if !*asJSON {
		mode := "只读报告，什么都没删"
		if *doApply {
			mode = "报告（--apply：下面删除前会逐项重新确认）"
		}
		fmt.Fprintf(stdout, "repo-tidy %s · %s · %d 个仓库\n", mode, now.Format("2006-01-02 15:04"), len(reps))
		report.Text(stdout, reps, now, home)
	}
	if !*doApply {
		if n := countRemovable(reps); n > 0 {
			fmt.Fprintf(stdout, "\n加 --apply 删除标「可删」的 %d 项（删前逐项重新确认；靠「已推到远程」的会联网确认）。\n", n)
		}
		return exitCode(failed)
	}

	res, err := applyAll(ctx, reps, wants, now, home)
	if err != nil {
		fmt.Fprintln(stderr, "repo-tidy：", err)
		return 1
	}
	if *asJSON {
		if err := writeApplyJSON(stdout, res); err != nil {
			fmt.Fprintln(stderr, "repo-tidy：写 JSON：", err)
			return 1
		}
	} else {
		writeApplyText(stdout, res)
	}
	return exitCode(failed || res.failed || res.done < res.target)
}

func inspectAll(ctx context.Context, dirs []string, online bool) []report.Repo {
	reps := make([]report.Repo, len(dirs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, d := range dirs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			rep, err := judge.Inspect(ctx, d)
			if err != nil {
				rep = judge.RepoReport{Root: d, Err: err.Error()}
			}
			h, err := health.Inspect(ctx, d, online)
			if err != nil {
				h = health.Report{Root: d, Errs: []string{"体检失败：" + err.Error()}}
			}
			reps[i] = report.Repo{RepoReport: rep, Health: &h}
		}()
	}
	wg.Wait()
	// 同一个仓库从不同路径进来（比如给了 worktree 路径）只留一份
	seen := map[string]bool{}
	out := reps[:0]
	for _, r := range reps {
		if !seen[r.Root] {
			seen[r.Root] = true
			out = append(out, r)
		}
	}
	return out
}

func countRemovable(reps []report.Repo) int {
	n := 0
	for _, r := range reps {
		for _, it := range append(append([]judge.Item(nil), r.Worktrees...), r.Branches...) {
			if it.Removable {
				n++
			}
		}
	}
	return n
}

func openLog(now time.Time, home string) (string, *os.File, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		dir = filepath.Join(home, ".local", "state")
	}
	dir = filepath.Join(dir, "repo-tidy")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, fmt.Errorf("建日志目录：%w", err)
	}
	p := filepath.Join(dir, "apply-"+now.Format("20060102-150405")+".log")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", nil, fmt.Errorf("打开日志：%w", err)
	}
	return p, f, nil
}

func kindName(k string) string {
	if k == judge.KindBranch {
		return "分支"
	}
	return "worktree"
}

func exitCode(failed bool) int {
	if failed {
		return 1
	}
	return 0
}
