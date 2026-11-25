//go:build !unix

package judge

// pidAlive 在没有 kill(pid, 0) 的系统上查不了，一律当作进程还在，锁都当作有效。
func pidAlive(int) bool { return true }
