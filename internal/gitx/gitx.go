// Package gitx 跑 git 子命令并解析输出。所有命令都带 GIT_OPTIONAL_LOCKS=0，
// 扫描时不刷新 index、不抢锁；每条命令都有超时。
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// DefaultTimeout 是单条 git 命令的默认超时。
const DefaultTimeout = 60 * time.Second

// NetTimeout 是联网命令（ls-remote）的超时。
const NetTimeout = 20 * time.Second

// Opts 是一次调用的可选项。
type Opts struct {
	Env     []string      // 追加的环境变量
	Stdin   string        // 标准输入
	Timeout time.Duration // 0 表示 DefaultTimeout
}

// Error 是 git 以非 0 退出或没能启动时的错误，带上命令和 stderr 方便定位。
type Error struct {
	Dir    string
	Args   []string
	Code   int
	Stderr string
	Err    error
}

func (e *Error) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" && e.Err != nil {
		msg = e.Err.Error()
	}
	return fmt.Sprintf("git %s (in %s): %s", strings.Join(e.Args, " "), e.Dir, msg)
}

func (e *Error) Unwrap() error { return e.Err }

// Exec 跑一条 git 命令，返回标准输出和退出码。退出码非 0 时 err 是 *Error，
// 调用方需要区分退出码（比如 merge-base --is-ancestor）时看 code。
func Exec(ctx context.Context, dir string, o Opts, args ...string) (string, int, error) {
	timeout := o.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_TERMINAL_PROMPT=0",
		"LC_ALL=C",
	)
	cmd.Env = append(cmd.Env, o.Env...)
	cmd.WaitDelay = 5 * time.Second
	if o.Stdin != "" {
		cmd.Stdin = strings.NewReader(o.Stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.String(), 0, nil
	}
	code := -1
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	}
	if ctx.Err() != nil {
		err = fmt.Errorf("timed out after %s: %w", timeout, ctx.Err())
	}
	return stdout.String(), code, &Error{Dir: dir, Args: args, Code: code, Stderr: stderr.String(), Err: err}
}

// Run 跑一条 git 命令，返回去掉末尾换行的输出；退出码非 0 就是错误。
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	out, _, err := Exec(ctx, dir, Opts{}, args...)
	return strings.TrimRight(out, "\n"), err
}

// Lines 把输出按行切开，去掉空行。
func Lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// Worktree 是 `git worktree list --porcelain -z` 的一条记录。
type Worktree struct {
	Path        string
	Head        string
	Branch      string // 不含 refs/heads/；分离头指针时为空
	Bare        bool
	Detached    bool
	Locked      bool
	LockReason  string
	Prunable    bool
	PruneReason string
	Main        bool // 列表第一条是主工作区
}

// Worktrees 列出仓库的全部工作区，第一条是主工作区。
func Worktrees(ctx context.Context, dir string) ([]Worktree, error) {
	out, _, err := Exec(ctx, dir, Opts{}, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	return ParseWorktrees(out), nil
}

// ParseWorktrees 解析 `git worktree list --porcelain -z` 的输出：
// 字段之间是一个 NUL，记录之间是两个 NUL。
func ParseWorktrees(out string) []Worktree {
	var list []Worktree
	for _, rec := range strings.Split(out, "\x00\x00") {
		if strings.Trim(rec, "\x00") == "" {
			continue
		}
		var w Worktree
		for _, f := range strings.Split(rec, "\x00") {
			key, val, _ := strings.Cut(f, " ")
			switch key {
			case "worktree":
				w.Path = val
			case "HEAD":
				w.Head = val
			case "branch":
				w.Branch = strings.TrimPrefix(val, "refs/heads/")
			case "bare":
				w.Bare = true
			case "detached":
				w.Detached = true
			case "locked":
				w.Locked, w.LockReason = true, val
			case "prunable":
				w.Prunable, w.PruneReason = true, val
			}
		}
		if w.Path == "" {
			continue
		}
		w.Main = len(list) == 0
		list = append(list, w)
	}
	return list
}
