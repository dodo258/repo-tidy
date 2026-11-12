package gitx

import "testing"

func TestParseWorktrees(t *testing.T) {
	out := "worktree /r\x00HEAD aaa\x00branch refs/heads/main\x00\x00" +
		"worktree /w1\x00HEAD bbb\x00detached\x00locked agent running\x00\x00" +
		"worktree /w2\x00HEAD ccc\x00branch refs/heads/agent/x\x00prunable gitdir file points to non-existent location\x00\x00"
	ws := ParseWorktrees(out)
	if len(ws) != 3 {
		t.Fatalf("应有 3 条，实际 %d：%+v", len(ws), ws)
	}
	if !ws[0].Main || ws[0].Branch != "main" || ws[1].Main {
		t.Fatalf("主工作区识别不对：%+v", ws)
	}
	if !ws[1].Detached || !ws[1].Locked || ws[1].LockReason != "agent running" {
		t.Fatalf("锁定/分离头指针解析不对：%+v", ws[1])
	}
	if ws[2].Branch != "agent/x" || !ws[2].Prunable {
		t.Fatalf("分支/prunable 解析不对：%+v", ws[2])
	}
	if len(ParseWorktrees("")) != 0 {
		t.Fatal("空输出应解析出 0 条")
	}
}
