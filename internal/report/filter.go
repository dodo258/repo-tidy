package report

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/dodo258/repo-tidy/internal/judge"
)

// --only 的取值
const (
	OnlyWorktrees = "worktrees"
	OnlyBranches  = "branches"
)

// Filter 是 --older-than、--only 给的筛选条件。零值表示不筛。
// 筛掉的项不显示、不计入总览，--apply 也不删；仓库的体检不受影响。
type Filter struct {
	OlderThan string        // 用户写的时长，如 30d；空表示不按时间筛
	Age       time.Duration // OlderThan 解析出来的时长
	Only      string        // OnlyWorktrees、OnlyBranches，或空（两种都要）
}

// On 说明有没有筛选条件。
func (f Filter) On() bool { return f.OlderThan != "" || f.Only != "" }

// ParseAge 解析 --older-than 的时长：正整数加单位 d（天）或 w（周），
// 也认 Go 的时长写法（如 36h、90m）。必须大于 0。
func ParseAge(s string) (time.Duration, bool) {
	unit := map[byte]time.Duration{'d': 24 * time.Hour, 'w': 7 * 24 * time.Hour}
	if n := len(s); n >= 2 {
		if u, ok := unit[s[n-1]]; ok {
			k, err := strconv.Atoi(s[:n-1])
			if err != nil || k <= 0 || strings.HasPrefix(s, "+") || int64(k) > math.MaxInt64/int64(u) {
				return 0, false
			}
			return time.Duration(k) * u, true
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
}

// ParseOnly 认 --only 的值：worktrees 或 branches。
func ParseOnly(s string) (string, bool) {
	switch s {
	case OnlyWorktrees, OnlyBranches:
		return s, true
	}
	return "", false
}

// keep 说明一项在不在筛选结果里。最后活动时间不知道的项证明不了「多久没动」，按时间筛时不要。
func (f Filter) keep(it judge.Item, now time.Time) bool {
	if f.Only == OnlyWorktrees && it.Kind != judge.KindWorktree || f.Only == OnlyBranches && it.Kind != judge.KindBranch {
		return false
	}
	if f.OlderThan != "" {
		return !it.LastActive.IsZero() && !it.LastActive.After(now.Add(-f.Age))
	}
	return true
}

// Apply 返回只含筛选后各项的仓库列表，不改 reps。仓库本身（含体检、扫描失败的）都留着。
// 检出在附加 worktree 里的可删分支要和 worktree 一起删（git 不让删还检出着的分支），
// 所以那个 worktree 被筛掉时，这个分支也筛掉。
func (f Filter) Apply(reps []Repo, now time.Time) []Repo {
	if !f.On() {
		return reps
	}
	out := make([]Repo, len(reps))
	for i, r := range reps {
		hidden := map[string]bool{} // 被筛掉的 worktree 检出的分支
		var wts []judge.Item
		for _, it := range r.Worktrees {
			if f.keep(it, now) {
				wts = append(wts, it)
			} else if it.Branch != "" {
				hidden[it.Branch] = true
			}
		}
		var brs []judge.Item
		for _, it := range r.Branches {
			if f.keep(it, now) && !(it.Removable && hidden[it.Name]) {
				brs = append(brs, it)
			}
		}
		r.Worktrees, r.Branches = wts, brs
		out[i] = r
	}
	return out
}
