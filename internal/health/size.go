package health

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// readSlots 限制整个进程同时读目录的协程数（多个仓库一起扫时也共用）。量大小几乎全是逐个文件
// lstat 的系统调用，并行能把时间压下来；但 macOS 上超过 4 路就互相抢文件系统的锁、反而变慢
// （同一台机器量过：1 路 3.8 秒，4 路 1.3 秒，8 路 1.8 秒，16 路 3.4 秒）。
var readSlots = make(chan struct{}, 4)

// sizeWorkers 是一次量大小起几个协程；真正同时读目录的不超过 readSlots。
const sizeWorkers = 4

// sizeRoot 是一个要量的目录：useSkip 为真时跳过 skip 里的子目录（root 自己不跳）。
type sizeRoot struct {
	path    string
	useSkip bool
}

// dirSizes 量每个 root 下所有文件的大小之和（不跟随符号链接），读不了的条目跳过。
// 所有 root 共用一个目录队列并行读，每个 root 的结果和逐个遍历相同。
// 返回的错误按 root 对应：root 自己 lstat 失败，或扫描被取消。
func dirSizes(ctx context.Context, roots []sizeRoot, skip map[string]bool) ([]Size, []error) {
	sizes := make([]Size, len(roots))
	errs := make([]error, len(roots))
	w := &walker{ctx: ctx, skip: skip, totals: make([]int64, len(roots))}
	w.cond = sync.NewCond(&w.mu)
	for i, r := range roots {
		info, err := os.Lstat(r.path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			sizes[i].Missing = true
		case err != nil:
			errs[i] = err
		case !info.IsDir():
			sizes[i].Bytes = info.Size()
		default:
			w.queue = append(w.queue, dirJob{path: r.path, root: i, isRoot: true, useSkip: r.useSkip})
		}
	}
	walked := append([]dirJob(nil), w.queue...)
	w.pending = len(w.queue)
	var wg sync.WaitGroup
	for range min(sizeWorkers, max(w.pending, 1)) {
		wg.Go(w.work)
	}
	wg.Wait()
	for _, j := range walked {
		sizes[j.root].Bytes = w.totals[j.root]
		errs[j.root] = ctx.Err()
	}
	return sizes, errs
}

type dirJob struct {
	path    string
	root    int // 算在第几个 root 头上
	isRoot  bool
	useSkip bool
}

// walker 是一次并行遍历：待读的目录排在 queue 里，pending 是排着和正在读的目录数，归零就结束。
type walker struct {
	ctx  context.Context
	skip map[string]bool

	mu      sync.Mutex
	cond    *sync.Cond
	queue   []dirJob
	pending int
	totals  []int64
}

func (w *walker) work() {
	for {
		w.mu.Lock()
		for len(w.queue) == 0 && w.pending > 0 {
			w.cond.Wait()
		}
		if w.pending == 0 {
			w.mu.Unlock()
			return
		}
		job := w.queue[len(w.queue)-1] // 后进先出：先读深处，排队的目录少
		w.queue = w.queue[:len(w.queue)-1]
		w.mu.Unlock()

		bytes, subdirs := w.readDir(job)

		w.mu.Lock()
		w.totals[job.root] += bytes
		w.queue = append(w.queue, subdirs...)
		w.pending += len(subdirs) - 1
		w.mu.Unlock()
		w.cond.Broadcast()
	}
}

// readDir 读一个目录：返回其中文件的大小之和和要接着读的子目录。
// 和 filepath.WalkDir 一样：子目录读出错就整个跳过，root 读出错时用已经读到的条目。
func (w *walker) readDir(job dirJob) (int64, []dirJob) {
	if w.ctx.Err() != nil {
		return 0, nil
	}
	readSlots <- struct{}{}
	defer func() { <-readSlots }()
	entries, err := os.ReadDir(job.path)
	if err != nil && !job.isRoot {
		return 0, nil
	}
	var total int64
	var subdirs []dirJob
	for _, d := range entries {
		p := filepath.Join(job.path, d.Name())
		if d.IsDir() {
			if !job.useSkip || !w.skip[p] {
				subdirs = append(subdirs, dirJob{path: p, root: job.root, useSkip: job.useSkip})
			}
			continue
		}
		if fi, err := d.Info(); err == nil {
			total += fi.Size()
		}
	}
	return total, subdirs
}
