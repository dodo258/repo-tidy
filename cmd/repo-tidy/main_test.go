package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dodo258/repo-tidy/internal/health"
	"github.com/dodo258/repo-tidy/internal/msg"
	"github.com/dodo258/repo-tidy/internal/report"
	tr "github.com/dodo258/repo-tidy/internal/testrepo"
)

// TestMain 把语言固定成中文，原有测试按中文断言；测英文的用例自己 t.Setenv 或加 --lang。
func TestMain(m *testing.M) {
	os.Unsetenv("LC_MESSAGES")
	os.Unsetenv("LANG")
	os.Setenv("LC_ALL", "zh_CN.UTF-8")
	os.Exit(m.Run())
}

func branches(t *testing.T, dir string) string {
	t.Helper()
	return tr.Git(t, dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
}

func TestReportThenApply(t *testing.T) {
	tr.Isolate(t)
	root := t.TempDir()
	one := filepath.Join(root, "one")
	tr.Init(t, one)
	tr.Git(t, one, "switch", "-q", "-c", "done")
	tr.Commit(t, one, "d.txt", "d\n", "done work")
	tr.Git(t, one, "switch", "-q", "main")
	tr.Git(t, one, "merge", "-q", "--squash", "done")
	tr.Git(t, one, "commit", "-q", "-m", "squash done")
	tr.Git(t, one, "switch", "-q", "-c", "wip")
	tr.Commit(t, one, "w.txt", "w\n", "wip work")
	tr.Git(t, one, "switch", "-q", "main")
	two := filepath.Join(root, "two")
	tr.Init(t, two)
	tr.Git(t, two, "branch", "same")

	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{root}, &out, &errb); code != 0 {
		t.Fatalf("报告模式退出码 %d：%s%s", code, out.String(), errb.String())
	}
	s := out.String()
	for _, want := range []string{"只读报告，什么都没删", "2 个仓库", "squash 进 main ", "有 1 个提交既不在 main 也没推到任何远程", "等于 main ", "加 --apply 删除标「可删」的 2 项"} {
		if !strings.Contains(s, want) {
			t.Fatalf("报告缺 %q：\n%s", want, s)
		}
	}
	if b := branches(t, one); !strings.Contains(b, "done") {
		t.Fatal("报告模式不该删东西")
	}

	out.Reset()
	if code := run(context.Background(), []string{one, "--apply", two}, &out, &errb); code != 0 {
		t.Fatalf("--apply 退出码 %d：%s%s", code, out.String(), errb.String())
	}
	if b := branches(t, one); b != "main\nwip" {
		t.Fatalf("one 应只剩 main 和 wip，实际：%q\n%s", b, out.String())
	}
	if b := branches(t, two); b != "main" {
		t.Fatalf("two 应只剩 main，实际：%q", b)
	}
	if !strings.Contains(out.String(), "删了 2 项。恢复命令在 ") {
		t.Fatalf("缺删除汇总：\n%s", out.String())
	}
}

func TestVersionAndBadPath(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"--version"}, &out, &errb); code != 0 || !strings.HasPrefix(out.String(), "repo-tidy ") {
		t.Fatalf("--version：%d %q", code, out.String())
	}
	if code := run(context.Background(), []string{filepath.Join(t.TempDir(), "nope")}, &out, &errb); code != 2 {
		t.Fatalf("不存在的路径应退出 2，实际 %d", code)
	}
}

func TestReportHasCheckupPerRepo(t *testing.T) {
	tr.Isolate(t)
	root := t.TempDir()
	bare := filepath.Join(t.TempDir(), "backup.git")
	tr.Git(t, root, "init", "-q", "--bare", "-b", "main", bare)
	synced := filepath.Join(root, "synced")
	tr.Init(t, synced)
	tr.Git(t, synced, "remote", "add", "backup", bare)
	tr.Git(t, synced, "branch", "side")
	tr.Git(t, synced, "push", "-q", "backup", "main", "side")
	tr.Git(t, synced, "branch", "fresh")
	tr.Write(t, synced, "README.md", "dirty\n")
	plain := filepath.Join(root, "plain")
	tr.Init(t, plain)
	tr.Git(t, plain, "branch", "extra") // 有可删项，不折叠，才有完整体检段

	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{root}, &out, &errb); code != 0 {
		t.Fatalf("退出码 %d：%s%s", code, out.String(), errb.String())
	}
	s := out.String()
	if n := strings.Count(s, "\n  体检\n"); n != 2 {
		t.Fatalf("每个仓库应有一段体检，实际 %d 段：\n%s", n, s)
	}
	for _, want := range []string{"占用      .git ", "主工作区  main，没设上游 · 1 个未提交改动 · 没有 stash", "最后提交  ", "backup    3 个本地分支里 1 个没推到最新（按本地记录", "没推  fresh"} {
		if !strings.Contains(s, want) {
			t.Fatalf("报告缺 %q：\n%s", want, s)
		}
	}
	if strings.Contains(s, "没有 backup 远程") {
		t.Fatalf("没指定 --backup-remote 时，没有 backup 远程的仓库不该提：\n%s", s)
	}

	tr.Git(t, synced, "push", "-q", "backup", "fresh")
	out.Reset()
	if code := run(context.Background(), []string{"--online", synced}, &out, &errb); code != 0 {
		t.Fatalf("--online 退出码 %d：%s%s", code, out.String(), errb.String())
	}
	if s := out.String(); !strings.Contains(s, "backup    全部 3 个本地分支已推到最新（联网查的）") {
		t.Fatalf("全推了之后应报推到最新：\n%s", s)
	}

	tr.Git(t, synced, "branch", "newer")
	tr.Git(t, synced, "remote", "set-url", "backup", filepath.Join(t.TempDir(), "gone.git"))
	out.Reset()
	if code := run(context.Background(), []string{"--online", synced}, &out, &errb); code != 0 {
		t.Fatalf("--online 退出码 %d：%s%s", code, out.String(), errb.String())
	}
	if s := out.String(); !strings.Contains(s, "backup    联不上：") || !strings.Contains(s, "没推  newer") {
		t.Fatalf("--online 联不上时应说联不上并按本地记录比对：\n%s", s)
	}
}

// 三个乱度不同的仓库：总览数字对得上，仓库按乱的程度从多到少排，干净的折成一行放最后。
// 仓库名按字母顺序和乱度顺序相反，排序靠的不是路径。
func TestOverviewAndMessOrder(t *testing.T) {
	tr.Isolate(t)
	root, _ := filepath.EvalSymlinks(t.TempDir()) // macOS 的临时目录在符号链接后面，git 报的是真实路径
	wtRoot, _ := filepath.EvalSymlinks(t.TempDir())

	calm := filepath.Join(root, "a-calm") // 乱度 0：只有 main
	tr.Init(t, calm)

	mid := filepath.Join(root, "b-mid") // 乱度 2：可删分支 done + 未合并分支 wip
	tr.Init(t, mid)
	tr.Git(t, mid, "branch", "done")
	tr.Git(t, mid, "switch", "-q", "-c", "wip")
	tr.Commit(t, mid, "w.txt", "w\n", "wip work")
	tr.Git(t, mid, "switch", "-q", "main")

	wild := filepath.Join(root, "c-wild") // 乱度 5
	tr.Init(t, wild)
	w1 := filepath.Join(wtRoot, "w1") // 可删 worktree + 可删分支 w1
	tr.Git(t, wild, "worktree", "add", "-q", "-b", "w1", w1)
	tr.Commit(t, w1, "big.txt", strings.Repeat("x", 50000), "big")
	tr.Git(t, wild, "merge", "-q", "w1")
	w2 := filepath.Join(wtRoot, "w2") // 未合并：worktree 和分支 w2 都留着
	tr.Git(t, wild, "worktree", "add", "-q", "-b", "w2", w2)
	tr.Commit(t, w2, "x.txt", "x\n", "unmerged")
	lk := filepath.Join(wtRoot, "lk") // 锁定：worktree 和分支 lk 留着，但不算未合并
	tr.Git(t, wild, "worktree", "add", "-q", "-b", "lk", lk)
	tr.Git(t, wild, "worktree", "lock", lk)
	tr.Git(t, wild, "branch", "old") // 可删分支
	for _, w := range []string{w1, w2, lk} {
		tr.Idle(t, w)
	}

	h, err := health.Inspect(context.Background(), wild, health.Options{}, msg.ZH)
	if err != nil {
		t.Fatal(err)
	}
	var w1Bytes int64
	for _, s := range h.Worktrees {
		if s.Path == w1 {
			w1Bytes = s.Bytes
		}
	}
	if w1Bytes < 50000 {
		t.Fatalf("w1 的大小没量到：%d", w1Bytes)
	}

	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{root}, &out, &errb); code != 0 {
		t.Fatalf("退出码 %d：%s%s", code, out.String(), errb.String())
	}
	s := out.String()
	for _, want := range []string{
		"总览  扫了 3 个仓库（1 个干净） · 附加 worktree 3 个 · 本地分支 6 个\n",
		"可删 4 项（worktree 1、分支 3），能腾出 " + report.Bytes(w1Bytes) + "\n",
		"留着 5 项（worktree 2、分支 3），其中 3 项有未合并内容\n",
		"干净的仓库 1 个（没有可删项，也没有未合并的内容）：\n  " + calm + "  0 个附加 worktree · 0 个本地分支\n",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("报告缺 %q：\n%s", want, s)
		}
	}
	iWild := strings.Index(s, "\n"+wild+"  ")
	iMid := strings.Index(s, "\n"+mid+"  ")
	iClean := strings.Index(s, "干净的仓库")
	iOverview := strings.Index(s, "总览  ")
	if !(iOverview >= 0 && iOverview < iWild && iWild < iMid && iMid < iClean) {
		t.Fatalf("顺序应是 总览 → c-wild → b-mid → 干净的 a-calm，实际位置 %d %d %d %d：\n%s", iOverview, iWild, iMid, iClean, s)
	}
	if strings.Contains(s, "\n"+calm+"  ") || strings.Count(s, "\n  体检\n") != 2 {
		t.Fatalf("干净的仓库应只占一行，不展开：\n%s", s)
	}
	if strings.Contains(s, "合计") {
		t.Fatalf("合计已挪到开头的总览：\n%s", s)
	}
	if os.Getenv("REPO_TIDY_SHOW") != "" {
		t.Log("\n" + s)
	}
}

// 可删项默认一行，行尾的短证据带提交号或远程引用；--verbose 恢复名字一行、证据一行。
func TestCompactAndVerbose(t *testing.T) {
	tr.Isolate(t)
	root := t.TempDir()
	bare := filepath.Join(t.TempDir(), "backup.git")
	tr.Git(t, root, "init", "-q", "--bare", "-b", "main", bare)
	r := filepath.Join(root, "r")
	tr.Init(t, r)
	tr.Git(t, r, "remote", "add", "backup", bare)
	tr.Git(t, r, "switch", "-q", "-c", "done")
	tr.Commit(t, r, "d.txt", "d\n", "done work")
	tr.Git(t, r, "switch", "-q", "main")
	tr.Git(t, r, "merge", "-q", "--squash", "done")
	tr.Git(t, r, "commit", "-q", "-m", "squash done")
	squash := tr.Git(t, r, "rev-parse", "--short=8", "main")
	tr.Git(t, r, "switch", "-q", "-c", "agent/x")
	tr.Commit(t, r, "x.txt", "x\n", "pushed work")
	tr.Git(t, r, "push", "-q", "backup", "agent/x")
	tr.Git(t, r, "switch", "-q", "main")

	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{r}, &out, &errb); code != 0 {
		t.Fatalf("退出码 %d：%s%s", code, out.String(), errb.String())
	}
	s := out.String()
	for _, want := range []string{"  done  今天动过  squash 进 main " + squash + "\n", "  agent/x  今天动过  已推 backup/agent/x\n"} {
		if !strings.Contains(s, want) {
			t.Fatalf("默认报告缺 %q：\n%s", want, s)
		}
	}
	ls := strings.Split(s, "\n")
	for i, l := range ls {
		if strings.HasPrefix(l, "    可删  ") && strings.HasPrefix(ls[i+1], "          ") {
			t.Fatalf("可删项默认不该另起一行写证据：\n%s", s)
		}
	}

	out.Reset()
	if code := run(context.Background(), []string{"--verbose", r}, &out, &errb); code != 0 {
		t.Fatalf("--verbose 退出码 %d：%s%s", code, out.String(), errb.String())
	}
	s = out.String()
	for _, want := range []string{
		"    可删  done  今天动过\n          内容已 squash 进 main：提交 " + squash + "「squash done」\n",
		"    可删  agent/x  今天动过\n          已推到 backup/agent/x（本地记录的远程提交 ",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("--verbose 报告缺 %q：\n%s", want, s)
		}
	}
}

// --backup-remote：默认比对 backup，仓库里没有就不提；自定义名字比对那个远程；
// 显式指定（哪怕就是 backup）而仓库里没有，体检和干净仓库的行尾都要报；空名字是参数错。
func TestBackupRemoteFlag(t *testing.T) {
	tr.Isolate(t)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	bare := filepath.Join(t.TempDir(), "mirror.git")
	tr.Git(t, root, "init", "-q", "--bare", "-b", "main", bare)
	plain := filepath.Join(root, "a-plain") // 干净，没有任何远程
	tr.Init(t, plain)
	mirrored := filepath.Join(root, "b-other") // 有 mirror 远程，fresh 没推；extra 可删，体检段展开
	tr.Init(t, mirrored)
	tr.Git(t, mirrored, "remote", "add", "mirror", bare)
	tr.Git(t, mirrored, "push", "-q", "mirror", "main")
	tr.Git(t, mirrored, "branch", "extra")
	tr.Git(t, mirrored, "switch", "-q", "-c", "fresh")
	tr.Commit(t, mirrored, "f.txt", "f\n", "fresh work")
	tr.Git(t, mirrored, "switch", "-q", "main")

	text := func(args ...string) string {
		t.Helper()
		var out, errb bytes.Buffer
		if code := run(context.Background(), append(args, root), &out, &errb); code != 0 {
			t.Fatalf("%v 退出码 %d：%s%s", args, code, out.String(), errb.String())
		}
		return out.String()
	}

	s := text()
	if strings.Contains(s, "backup") || strings.Contains(s, "mirror") || strings.Contains(s, "注意") {
		t.Fatalf("默认（backup）两个仓库都没有，不该提备份远程：\n%s", s)
	}
	if !strings.Contains(s, "\n  "+plain+"  0 个附加 worktree · 0 个本地分支\n") {
		t.Fatalf("干净仓库行尾不该多一句：\n%s", s)
	}

	s = text("--backup-remote", "backup")
	for _, want := range []string{
		"    backup    没有 backup 远程\n",
		"\n  " + plain + "  0 个附加 worktree · 0 个本地分支 · 注意：没有 backup 远程\n",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("显式指定 backup 时缺 %q：\n%s", want, s)
		}
	}

	s = text("--backup-remote", "mirror")
	for _, want := range []string{
		"    mirror    3 个本地分支里 2 个没推到最新（按本地记录",
		"没推  fresh",
		"注意：没有 mirror 远程\n",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("--backup-remote mirror 时缺 %q：\n%s", want, s)
		}
	}

	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"--json", plain}, &out, &errb); code != 0 {
		t.Fatalf("--json 退出码 %d：%s", code, errb.String())
	}
	var doc struct {
		Repos []struct {
			Health struct {
				Backup struct {
					Remote   string `json:"remote"`
					Required bool   `json:"required"`
					Present  *bool  `json:"present"`
				} `json:"backup"`
			} `json:"health"`
		} `json:"repos"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if b := doc.Repos[0].Health.Backup; b.Present == nil || *b.Present || b.Remote != "backup" || b.Required {
		t.Fatalf("JSON 照旧给 present=false，并写出比对的远程：%+v", b)
	}

	if code := run(context.Background(), []string{"--backup-remote", "", plain}, &out, &errb); code != 2 {
		t.Fatalf("空的 --backup-remote 应是参数错（2），实际 %d", code)
	}
}
