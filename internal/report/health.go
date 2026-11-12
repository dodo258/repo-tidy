package report

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dodo258/repo-tidy/internal/health"
)

// checkup 输出一个仓库的体检段。
func checkup(w io.Writer, h health.Report, now time.Time) {
	fmt.Fprintln(w, "  体检")
	if h.Note != "" {
		fmt.Fprintf(w, "    %s\n", h.Note)
		return
	}
	row := func(label, text string) { fmt.Fprintf(w, "    %s%s\n", pad(label, 10), text) }

	usage := fmt.Sprintf(".git %s · 主工作区 %s", Bytes(h.GitDir.Bytes), Bytes(h.Main.Bytes))
	if n := len(h.Worktrees); n > 0 {
		var total int64
		missing := 0
		for _, s := range h.Worktrees {
			total += s.Bytes
			if s.Missing {
				missing++
			}
		}
		usage += fmt.Sprintf(" · 附加 worktree %d 个共 %s", n, Bytes(total))
		if missing > 0 {
			usage += fmt.Sprintf("（%d 个目录已不在）", missing)
		}
	}
	row("占用", usage)

	parts := []string{upstream(h)}
	if h.Changes == 0 {
		parts = append(parts, "没有未提交改动")
	} else if h.Untracked > 0 {
		parts = append(parts, fmt.Sprintf("%d 个未提交改动（含 %d 个未跟踪）", h.Changes, h.Untracked))
	} else {
		parts = append(parts, fmt.Sprintf("%d 个未提交改动", h.Changes))
	}
	if h.Stashes == 0 {
		parts = append(parts, "没有 stash")
	} else {
		parts = append(parts, fmt.Sprintf("%d 个 stash", h.Stashes))
	}
	row("主工作区", strings.Join(parts, " · "))

	if h.LastCommit.IsZero() {
		row("最后提交", "还没有提交")
	} else {
		row("最后提交", fmt.Sprintf("%s（%s，%s）", h.LastCommit.Format("2006-01-02 15:04"), daysAgo(h.LastCommit, now), h.LastCommitBranch))
	}

	backup(row, h.Backup)
	for _, e := range h.Errs {
		row("没查成", e)
	}
}

func upstream(h health.Report) string {
	switch {
	case h.Branch == "":
		return "分离头指针"
	case h.Upstream == "":
		return h.Branch + "，没设上游"
	case h.UpstreamGone:
		return fmt.Sprintf("%s，上游 %s 已不在", h.Branch, h.Upstream)
	case h.Ahead == 0 && h.Behind == 0:
		return fmt.Sprintf("%s，和 %s 一致", h.Branch, h.Upstream)
	}
	return fmt.Sprintf("%s，领先 %s %d 个提交、落后 %d 个", h.Branch, h.Upstream, h.Ahead, h.Behind)
}

func backup(row func(label, text string), b health.Backup) {
	const label = health.BackupRemote
	if !b.Present {
		row(label, "没有 backup 远程")
		return
	}
	source := "按本地记录，即上次推送或 fetch 时的状态"
	if b.Online && b.OnlineErr == "" {
		source = "联网查的"
	}
	if b.OnlineErr != "" {
		row(label, "联不上："+b.OnlineErr)
	}
	if len(b.Stale) == 0 {
		row(label, fmt.Sprintf("全部 %d 个本地分支已推到最新（%s）", b.Checked, source))
		return
	}
	row(label, fmt.Sprintf("%d 个本地分支里 %d 个没推到最新（%s）：", b.Checked, len(b.Stale), source))
	for _, s := range b.Stale {
		var text string
		switch s.State {
		case health.NotPushed:
			text = "没推  " + s.Branch
		case health.Behind:
			text = fmt.Sprintf("落后  %s：本地多 %d 个提交", s.Branch, s.LocalOnly)
		case health.Diverged:
			text = fmt.Sprintf("分叉  %s：本地多 %d 个提交、backup 多 %d 个", s.Branch, s.LocalOnly, s.RemoteOnly)
		default:
			text = fmt.Sprintf("比不了  %s：backup 上是 %s，本地没有这个提交（fetch 后再看）", s.Branch, shortSHA(s.Remote))
		}
		row("", "  "+text)
	}
}

// Bytes 把字节数写成 KB、MB、GB（1024 进位）。
func Bytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	v := float64(n)
	for _, unit := range []string{"KB", "MB", "GB"} {
		v /= 1024
		if v < 1024 {
			return fmt.Sprintf("%.1f %s", v, unit)
		}
	}
	return fmt.Sprintf("%.1f TB", v/1024)
}

func daysAgo(t, now time.Time) string {
	d := now.Sub(t)
	if d < 24*time.Hour {
		return "今天"
	}
	return fmt.Sprintf("%d 天前", int(d.Hours()/24))
}

func shortSHA(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// pad 按终端显示宽度（中日韩字符占两格）把 s 补到 width 格。
func pad(s string, width int) string {
	n := 0
	for _, r := range s {
		if r >= 0x2E80 {
			n += 2
		} else {
			n++
		}
	}
	if n >= width {
		return s + " "
	}
	return s + strings.Repeat(" ", width-n)
}

// attention 列出体检里值得注意的几项，给折叠成一行的干净仓库用。
func attention(h health.Report) []string {
	var out []string
	if h.Changes > 0 {
		out = append(out, fmt.Sprintf("主工作区 %d 个未提交改动", h.Changes))
	}
	if h.Stashes > 0 {
		out = append(out, fmt.Sprintf("%d 个 stash", h.Stashes))
	}
	if h.Upstream != "" && !h.UpstreamGone && (h.Ahead > 0 || h.Behind > 0) {
		out = append(out, fmt.Sprintf("%s 和上游不一致（领先 %d，落后 %d）", h.Branch, h.Ahead, h.Behind))
	}
	switch b := h.Backup; {
	case h.Note != "":
	case !b.Present:
		out = append(out, "没有 backup 远程")
	case len(b.Stale) > 0:
		out = append(out, fmt.Sprintf("backup 有 %d 个分支没推到最新", len(b.Stale)))
	case b.OnlineErr != "":
		out = append(out, "backup 联不上")
	}
	if len(h.Errs) > 0 {
		out = append(out, fmt.Sprintf("体检有 %d 项没查成", len(h.Errs)))
	}
	return out
}
