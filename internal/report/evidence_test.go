package report

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dodo258/repo-tidy/internal/judge"
	"github.com/dodo258/repo-tidy/internal/msg"
)

func TestShortEvidence(t *testing.T) {
	it := judge.Item{Basis: judge.Merged, Evidence: "内容已 squash 进 main：提交 3702a43d「合并 导出 CSV」", Short: "squash 进 main 3702a43d"}
	if got := shortEvidence(it); got != it.Short {
		t.Fatalf("有短形式就用短形式，实际 %q", got)
	}
	it.Short = ""
	if got := shortEvidence(it); got != it.Evidence {
		t.Fatalf("没有短形式就原样给完整证据，不丢信息，实际 %q", got)
	}
}

// lines 按开头统计报告里的行：可删项行、缩进的证据/原因行、按依据计数的那一行。
func lines(s string) (removable, detail, counts int) {
	for _, l := range strings.Split(s, "\n") {
		switch {
		case strings.HasPrefix(l, "    可删  "):
			removable++
		case strings.HasPrefix(l, "          "):
			detail++
		case strings.HasPrefix(l, "  可删 ") && strings.Contains(l, " 项："):
			counts++
		}
	}
	return
}

func TestRemovableOneLinePerItem(t *testing.T) {
	now := time.Now()
	var brs []judge.Item
	for i := range 12 {
		it := judge.Item{Kind: judge.KindBranch, Name: fmt.Sprintf("agent/b%02d", i), Removable: true, LastActive: now.Add(-72 * time.Hour)}
		if i < 8 {
			it.Basis, it.Evidence = judge.Merged, fmt.Sprintf("内容已 squash 进 main：提交 %08x「第 %d 件」", 0xa0000000+i, i)
			it.Short = fmt.Sprintf("squash 进 main %08x", 0xa0000000+i)
		} else {
			it.Basis, it.Remote, it.RemoteRef = judge.Pushed, "backup", "refs/heads/"+it.Name
			it.Evidence = "已推到 backup/" + it.Name + "（本地记录的远程提交 7aab07ec 包含它）"
			it.Short = "已推 backup/" + it.Name
		}
		brs = append(brs, it)
	}
	brs = append(brs, judge.Item{Kind: judge.KindBranch, Name: "wip", Basis: judge.Keep, Unmerged: true,
		Reasons: []string{"有 1 个提交既不在 main 也没推到任何远程：f90c4e69「试一下」", "第二条原因"}})
	big := repo("/p/big", nil, brs)
	small := repo("/p/small", nil, brs[:3]) // 3 个可删，不写计数行

	var def, verbose bytes.Buffer
	Text(&def, []Repo{big, small}, now, "", false, msg.ZH, Filter{})
	Text(&verbose, []Repo{big, small}, now, "", true, msg.ZH, Filter{})

	r, d, c := lines(def.String())
	if r != 15 || d != 2 || c != 1 {
		t.Fatalf("默认：可删项应各占一行（15 行）、只有留着的项写原因（2 行）、只有可删超过 10 项的仓库有计数行（1 行），实际 %d、%d、%d：\n%s", r, d, c, def.String())
	}
	for _, want := range []string{
		"  可删 12 项：已合并 8、已推送 4\n",
		"    可删  agent/b00  3 天没动  squash 进 main a0000000\n",
		"    可删  agent/b11  3 天没动  已推 backup/agent/b11\n",
		"    留着  wip  \n          有 1 个提交既不在 main 也没推到任何远程：f90c4e69「试一下」\n          第二条原因\n",
	} {
		if !strings.Contains(def.String(), want) {
			t.Fatalf("默认报告缺 %q：\n%s", want, def.String())
		}
	}

	r, d, c = lines(verbose.String())
	if r != 15 || d != 15+2 || c != 0 {
		t.Fatalf("--verbose：可删项各两行（名字一行、证据一行），不写计数行，实际 可删 %d、证据/原因 %d、计数 %d：\n%s", r, d, c, verbose.String())
	}
	if !strings.Contains(verbose.String(), "    可删  agent/b00  3 天没动\n          内容已 squash 进 main：提交 a0000000「第 0 件」\n") {
		t.Fatalf("--verbose 应恢复完整两行：\n%s", verbose.String())
	}
}
