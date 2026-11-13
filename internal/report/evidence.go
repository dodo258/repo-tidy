package report

import (
	"fmt"
	"strings"

	"github.com/dodo258/repo-tidy/internal/judge"
	"github.com/dodo258/repo-tidy/internal/msg"
)

// shortEvidence 是可删项默认一行时写的证据：judge 给的短形式，保留能自己核对的部分
// （如「squash 进 main 3702a43d」「已推 backup/agent/x」）；没有短形式时原样给完整证据，不丢信息。
func shortEvidence(it judge.Item) string {
	if it.Short != "" {
		return it.Short
	}
	return it.Evidence
}

// basisCounts 把一个仓库的可删项按依据计数，如「已合并 15、已推送 6」。
func basisCounts(t *msg.Text, r Repo) string {
	n := map[judge.Basis]int{}
	for _, it := range items(r) {
		if it.Removable {
			n[it.Basis]++
		}
	}
	var parts []string
	for _, b := range []struct {
		basis judge.Basis
		label string
	}{{judge.Merged, t.BasisMerged}, {judge.Pushed, t.BasisPushed}, {judge.Prunable, t.BasisPrunable}} {
		if n[b.basis] > 0 {
			parts = append(parts, fmt.Sprintf(b.label, n[b.basis]))
		}
	}
	return strings.Join(parts, t.ListSep)
}
