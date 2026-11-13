package judge

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/dodo258/repo-tidy/internal/msg"
)

// DefaultMinIdle 是 worktree 最近活动离现在至少要多久，才不当作还在用。
const DefaultMinIdle = 30 * time.Minute

// procTimeout 是列一次进程工作目录的超时。
const procTimeout = 10 * time.Second

// Options 控制「worktree 是不是还在用」的两条检查。
type Options struct {
	// MinIdle：worktree 的 index、HEAD、reflog 在这么久之内改过，就当作还在用、留着；0 关掉这条。
	MinIdle time.Duration
	// Lang 是证据、原因和出错说明用的语言；零值是中文。
	Lang msg.Lang
	// Procs 列出进程和它们的工作目录；nil 用 ListProcs。出错时每个 worktree 都当作可能在用。
	Procs func(ctx context.Context) ([]Proc, error)
}

// DefaultOptions 是命令行不加参数时的检查。
func DefaultOptions() Options { return Options{MinIdle: DefaultMinIdle} }

// Proc 是一个进程和它的工作目录。
type Proc struct {
	PID  int
	Name string
	Cwd  string
}

// ListProcs 列出能看到的进程的工作目录：Linux 读 /proc/*/cwd，其他系统用 lsof。
func ListProcs(ctx context.Context) ([]Proc, error) {
	ctx, cancel := context.WithTimeout(ctx, procTimeout)
	defer cancel()
	if runtime.GOOS == "linux" {
		return procfsProcs(ctx, "/proc")
	}
	return lsofProcs(ctx)
}

func lsofProcs(ctx context.Context) ([]Proc, error) {
	cmd := exec.CommandContext(ctx, "lsof", "-n", "-P", "-w", "-d", "cwd", "-Fpcn")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("lsof timed out: %w", ctx.Err())
	}
	procs := parseLsof(out)
	// lsof 看不到某些进程时也可能退出码非 0；只要列出了东西就用，一个都没列出才算没查成。
	if len(procs) == 0 {
		if err != nil {
			return nil, fmt.Errorf("lsof: %v %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil, errors.New("lsof listed no processes")
	}
	return procs, nil
}

// parseLsof 解析 `lsof -Fpcn` 的输出：每行一个字段，p 开头是 pid，c 是进程名，n 是路径。
func parseLsof(out []byte) []Proc {
	var procs []Proc
	var cur Proc
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		l := sc.Text()
		if l == "" {
			continue
		}
		v := l[1:]
		switch l[0] {
		case 'p':
			pid, _ := strconv.Atoi(v)
			cur = Proc{PID: pid}
		case 'c':
			cur.Name = v
		case 'n':
			if cur.PID > 0 && strings.HasPrefix(v, "/") {
				p := cur
				p.Cwd = v
				procs = append(procs, p)
			}
		}
	}
	return procs
}

func procfsProcs(ctx context.Context, root string) ([]Proc, error) {
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", root, err)
	}
	var procs []Proc
	for _, e := range ents {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("reading %s timed out: %w", root, err)
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		cwd, err := os.Readlink(filepath.Join(root, e.Name(), "cwd"))
		if err != nil {
			continue // 进程已退出，或别的用户的进程看不了
		}
		name, _ := os.ReadFile(filepath.Join(root, e.Name(), "comm"))
		procs = append(procs, Proc{PID: pid, Name: strings.TrimSpace(string(name)), Cwd: cwd})
	}
	if len(procs) == 0 {
		return nil, fmt.Errorf("no processes read from %s", root)
	}
	return procs, nil
}

// busyReasons 返回 worktree 还在用的原因：有进程的工作目录在里面，或 index、HEAD、reflog 刚改过。
func (c *checker) busyReasons(path, gitDir string) []string {
	var reasons []string
	if c.procErr != nil {
		reasons = append(reasons, fmt.Sprintf(c.t.ProcsUnknown, c.procErr))
	} else if ps := procsIn(c.procs, path); len(ps) > 0 {
		names := make([]string, len(ps))
		for i, p := range ps {
			names[i] = fmt.Sprintf(c.t.Proc, p.Name, p.PID)
		}
		reasons = append(reasons, fmt.Sprintf(c.t.ProcsHere, c.t.List(names, 3)))
	}
	if c.opts.MinIdle > 0 {
		var last time.Time
		for _, f := range []string{"index", "HEAD", filepath.Join("logs", "HEAD")} {
			if t := modTime(filepath.Join(gitDir, f)); t.After(last) {
				last = t
			}
		}
		if ago := c.now.Sub(last); !last.IsZero() && ago < c.opts.MinIdle {
			n := int(ago / time.Minute)
			if n < 0 {
				n = 0
			}
			reasons = append(reasons, c.t.RecentlyTouched.N(n))
		}
	}
	return reasons
}

// procsIn 找工作目录在 dir 里（含 dir 本身）的进程；dir 经过符号链接时两种写法都认。
func procsIn(procs []Proc, dir string) []Proc {
	dirs := []string{filepath.Clean(dir)}
	if real, err := filepath.EvalSymlinks(dir); err == nil && real != dirs[0] {
		dirs = append(dirs, real)
	}
	var out []Proc
	for _, p := range procs {
		for _, d := range dirs {
			if p.Cwd == d || strings.HasPrefix(p.Cwd, d+string(filepath.Separator)) {
				out = append(out, p)
				break
			}
		}
	}
	return out
}
