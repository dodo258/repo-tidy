//go:build unix

package judge

import (
	"errors"
	"syscall"
)

// pidAlive 用 kill(pid, 0) 问进程还在不在：只有明确回「没有这个进程」（ESRCH）才算不在；
// 别的用户的进程回 EPERM，算在。判错成「不在」会删掉别人正在用的 worktree，所以拿不准都算在。
func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || !errors.Is(err, syscall.ESRCH)
}
