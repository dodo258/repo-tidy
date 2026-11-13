// repo-tidy 扫描仓库里的附加 worktree 和本地分支，判定哪些能证明安全可删，
// 并给每个仓库做体检（占用空间、未提交改动、stash、上游、备份远程是否推到最新）。
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
	"github.com/dodo258/repo-tidy/internal/msg"
	"github.com/dodo258/repo-tidy/internal/report"
)

var version = "0.2.0"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	// 先按环境变量定语言，--help 和参数错误也用它；解析完 --lang 再覆盖
	lang := msg.FromEnv(os.Getenv)
	t := msg.For(lang)
	fail := func(text string) { fmt.Fprintln(stderr, t.ErrPrefix+text) }
	fs := flag.NewFlagSet("repo-tidy", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, t.Usage) }
	doApply := fs.Bool("apply", false, "")
	online := fs.Bool("online", false, "")
	asJSON := fs.Bool("json", false, "")
	verbose := fs.Bool("verbose", false, "")
	progress := fs.Bool("progress", false, "")
	selectFrom := fs.String("select", "", "")
	undo := fs.Bool("undo", false, "")
	showVersion := fs.Bool("version", false, "")
	minIdle := fs.Duration("min-idle", judge.DefaultMinIdle, "")
	backupRemote := fs.String("backup-remote", health.DefaultBackupRemote, "")
	depth := fs.Int("depth", discover.DefaultDepth, "")
	olderThan := fs.String("older-than", "", "")
	only := fs.String("only", "", "")
	fs.Func("lang", "", func(s string) error {
		l, ok := msg.Parse(s)
		if !ok {
			return fmt.Errorf(t.BadLang, s)
		}
		lang, t = l, msg.For(l)
		return nil
	})
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
	home, _ := os.UserHomeDir()
	if *undo {
		if *selectFrom != "" {
			fail(t.UndoWithSelect)
			return 2
		}
		if *olderThan != "" || *only != "" {
			fail(t.UndoWithFilter)
			return 2
		}
		if len(paths) > 1 {
			fail(fmt.Sprintf(t.UndoOneLog, len(paths)))
			return 2
		}
		return runUndo(ctx, paths, *doApply, *asJSON, lang, home, stdout, stderr)
	}
	if *minIdle < 0 {
		fail(t.MinIdleNegative)
		return 2
	}
	if *depth < 1 || *depth > discover.MaxDepth {
		fail(fmt.Sprintf(t.BadDepth, discover.MaxDepth, *depth))
		return 2
	}
	if *backupRemote == "" {
		fail(t.EmptyBackup)
		return 2
	}
	filter, err := parseFilter(t, *olderThan, *only)
	if err != nil {
		fail(err.Error())
		return 2
	}
	hopts := health.Options{Online: *online, BackupRemote: *backupRemote}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "backup-remote" {
			hopts.RequireBackup = true // 显式指定的：仓库里没有也要报
		}
	})
	var wants map[string][]apply.Want // 仓库 → 勾选的项；nil 表示删全部可删项
	if *selectFrom != "" {
		if !*doApply {
			fail(t.SelectNeedsApply)
			return 2
		}
		var repos []string
		var err error
		if wants, repos, err = readWants(*selectFrom, lang); err != nil {
			fail(err.Error())
			return 2
		}
		if len(paths) == 0 {
			paths = repos
		}
	}
	if len(paths) == 0 {
		root := os.Getenv("REPO_TIDY_ROOT")
		if root == "" {
			root = filepath.Join(home, "projects")
		}
		paths = []string{root}
	}
	dirs, err := discover.Repos(paths, *depth)
	if err != nil {
		fail(err.Error())
		return 2
	}
	if len(dirs) == 0 {
		fail(t.NoRepos)
		return 1
	}

	now := time.Now()
	// 扫描时进程只查一次，所有仓库共用；删除前的重新判定每个仓库现查（Procs 为 nil）。
	opts := judge.Options{MinIdle: *minIdle, Lang: lang}
	scanOpts := opts
	scanOpts.Procs = onceProcs()
	var show func(i, n int, root string)
	prog := newProgress(stderr, t, *progress, !*asJSON)
	if prog != nil {
		show = prog.show
	}
	// 筛掉的项从这里起就不在报告里：不显示、不计入总览，--apply 也不会删
	reps := filter.Apply(inspectAll(ctx, dirs, hopts, scanOpts, show), now)
	if prog != nil {
		prog.done()
	}
	failed := false
	for _, r := range reps {
		failed = failed || r.Err != ""
	}
	if *asJSON && !*doApply {
		if err := report.JSON(stdout, reps, now, filter); err != nil {
			fail(fmt.Sprintf(t.WriteJSON, err))
			return 1
		}
		return exitCode(failed)
	}
	if !*asJSON {
		mode := t.ModeReadOnly
		if *doApply {
			mode = t.ModeApply
		}
		fmt.Fprintf(stdout, t.Header, mode, now.Format("2006-01-02 15:04"), t.Repos.N(len(reps)))
		report.Text(stdout, reps, now, home, *verbose, lang, filter)
	}
	if !*doApply {
		if n := countRemovable(reps); n > 0 {
			hint := t.HintApply
			if filter.On() {
				hint = t.HintApplyFilter
			}
			fmt.Fprintf(stdout, hint, t.Items.N(n))
		}
		return exitCode(failed)
	}

	res, err := applyAll(ctx, reps, wants, opts, now, home)
	if err != nil {
		fail(err.Error())
		return 1
	}
	if *asJSON {
		if err := writeApplyJSON(stdout, res); err != nil {
			fail(fmt.Sprintf(t.WriteJSON, err))
			return 1
		}
	} else {
		writeApplyText(stdout, t, res)
	}
	return exitCode(failed || res.failed || res.done < res.target)
}

// parseFilter 校验 --older-than、--only，不对时返回给人看的参数错误。
func parseFilter(t *msg.Text, olderThan, only string) (report.Filter, error) {
	f := report.Filter{OlderThan: olderThan}
	if olderThan != "" {
		age, ok := report.ParseAge(olderThan)
		if !ok {
			return f, fmt.Errorf(t.BadOlderThan, olderThan)
		}
		f.Age = age
	}
	if only != "" {
		o, ok := report.ParseOnly(only)
		if !ok {
			return f, fmt.Errorf(t.BadOnly, only)
		}
		f.Only = o
	}
	return f, nil
}

// onceProcs 返回只真正查一次进程的 Procs，后面的调用复用第一次的结果。
func onceProcs() func(context.Context) ([]judge.Proc, error) {
	var once sync.Once
	var procs []judge.Proc
	var err error
	return func(ctx context.Context) ([]judge.Proc, error) {
		once.Do(func() { procs, err = judge.ListProcs(ctx) })
		return procs, err
	}
}

func countRemovable(reps []report.Repo) int {
	n := 0
	for _, r := range reps {
		n += report.Removable(r)
	}
	return n
}

// logDir 是恢复日志的目录：${XDG_STATE_HOME:-~/.local/state}/repo-tidy。
func logDir(home string) string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "repo-tidy")
}

func openLog(t *msg.Text, now time.Time, home string) (string, *os.File, error) {
	dir := logDir(home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, fmt.Errorf(t.MakeLogDir, err)
	}
	p := filepath.Join(dir, "apply-"+now.Format("20060102-150405")+".log")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", nil, fmt.Errorf(t.OpenLog, err)
	}
	return p, f, nil
}

func kindName(t *msg.Text, k string) string {
	if k == judge.KindBranch {
		return t.Kinds.Branch
	}
	return t.Kinds.Worktree
}

func exitCode(failed bool) int {
	if failed {
		return 1
	}
	return 0
}
