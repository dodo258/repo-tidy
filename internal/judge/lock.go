package judge

import (
	"regexp"
	"strconv"
	"strings"
)

// lockPID 认锁原因里的「pid N」：在开头，或前面是空格、左括号。
// Claude Code 等代理锁 worktree 时写「claude session <名字> (pid N ...)」；
// 「rapid 5 retries」这类原因里碰巧有 pid 字样的不算。
var lockPID = regexp.MustCompile(`(?:^|[ (])pid (\d+)\b`)

// lockPIDs 返回锁原因里写的全部 pid；没写或写的不是正常 pid（≤1、超范围）时返回 nil。
func lockPIDs(reason string) []int {
	var pids []int
	for _, m := range lockPID.FindAllStringSubmatch(reason, -1) {
		pid, err := strconv.Atoi(m[1])
		if err != nil || pid <= 1 {
			return nil
		}
		pids = append(pids, pid)
	}
	return pids
}

// staleLock 判断锁是不是已经没人持有：原因里写了 pid，且这些进程都已不在。
// 没写 pid 的锁（用户自己锁的、放在移动硬盘上的）一律当作有效。
// alive 是判断进程还在不在的函数；返回的 pids 用来写证据。
func staleLock(reason string, alive func(int) bool) (pids []int, stale bool) {
	pids = lockPIDs(reason)
	if len(pids) == 0 {
		return nil, false
	}
	for _, p := range pids {
		if alive(p) {
			return pids, false
		}
	}
	return pids, true
}

func joinPIDs(pids []int) string {
	s := make([]string, len(pids))
	for i, p := range pids {
		s[i] = strconv.Itoa(p)
	}
	return strings.Join(s, ", ")
}
