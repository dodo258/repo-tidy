package report

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dodo258/repo-tidy/internal/health"
	"github.com/dodo258/repo-tidy/internal/msg"
)

// checkup 输出一个仓库的体检段。
func checkup(w io.Writer, t *msg.Text, h health.Report, now time.Time) {
	fmt.Fprintln(w, t.Checkup)
	if h.Note != "" {
		fmt.Fprintf(w, "    %s\n", h.Note)
		return
	}
	// 标签列宽：最长的标签再空两格
	labelWidth := 0
	for _, l := range []string{t.RowUsage, t.RowMain, t.RowLastCommit, h.Backup.Remote, t.RowNotChecked} {
		labelWidth = max(labelWidth, width(l)+2)
	}
	row := func(label, text string) { fmt.Fprintf(w, "    %s%s\n", padTo(label, labelWidth), text) }

	usage := fmt.Sprintf(t.Usage1, Bytes(h.GitDir.Bytes), Bytes(h.Main.Bytes))
	if n := len(h.Worktrees); n > 0 {
		var total int64
		missing := 0
		for _, s := range h.Worktrees {
			total += s.Bytes
			if s.Missing {
				missing++
			}
		}
		usage += fmt.Sprintf(t.UsageWorktrees, n, Bytes(total))
		if missing > 0 {
			usage += t.DirsGone.N(missing)
		}
	}
	row(t.RowUsage, usage)

	parts := []string{upstream(t, h)}
	if h.Changes == 0 {
		parts = append(parts, t.NoChanges)
	} else if h.Untracked > 0 {
		parts = append(parts, fmt.Sprintf(t.ChangesUntrack, h.Changes, h.Untracked))
	} else {
		parts = append(parts, fmt.Sprintf(t.Changes, h.Changes))
	}
	if h.Stashes == 0 {
		parts = append(parts, t.NoStash)
	} else {
		parts = append(parts, fmt.Sprintf(t.Stashes, h.Stashes))
	}
	row(t.RowMain, strings.Join(parts, " · "))

	if h.LastCommit.IsZero() {
		row(t.RowLastCommit, t.NoCommits)
	} else {
		row(t.RowLastCommit, fmt.Sprintf(t.LastCommit, h.LastCommit.Format("2006-01-02 15:04"), daysAgo(t, h.LastCommit, now), h.LastCommitBranch))
	}

	backup(t, func(text string) { row(h.Backup.Remote, text) }, func(text string) { row("", text) }, h.Backup)
	for _, e := range h.Errs {
		row(t.RowNotChecked, e)
	}
}

func upstream(t *msg.Text, h health.Report) string {
	switch {
	case h.Branch == "":
		return t.Detached
	case h.Upstream == "":
		return fmt.Sprintf(t.NoUpstream, h.Branch)
	case h.UpstreamGone:
		return fmt.Sprintf(t.UpstreamGone, h.Branch, h.Upstream)
	case h.Ahead == 0 && h.Behind == 0:
		return fmt.Sprintf(t.UpstreamSame, h.Branch, h.Upstream)
	}
	return fmt.Sprintf(t.UpstreamDiff, h.Branch, h.Upstream, h.Ahead, h.Behind)
}

// backup 写体检里备份远程那几行（标签是远程名）：row 写带标签的行，more 写下面缩进的明细。
// 仓库里没有这个远程时，只有显式指定了（--backup-remote）才写。
func backup(t *msg.Text, row, more func(text string), b health.Backup) {
	if !b.Present {
		if b.Required {
			row(fmt.Sprintf(t.NoBackup, b.Remote))
		}
		return
	}
	source := t.SourceLocal
	if b.Online && b.OnlineErr == "" {
		source = t.SourceOnline
	}
	if b.OnlineErr != "" {
		row(fmt.Sprintf(t.Unreachable, b.OnlineErr))
	}
	if len(b.Stale) == 0 {
		row(fmt.Sprintf(t.BackupAllPushed, b.Checked, source))
		return
	}
	row(fmt.Sprintf(t.BackupStale, b.Checked, len(b.Stale), source))
	for _, s := range b.Stale {
		var text string
		switch s.State {
		case health.NotPushed:
			text = fmt.Sprintf(t.StaleNotPushed, s.Branch)
		case health.Behind:
			text = fmt.Sprintf(t.StaleBehind, s.Branch, s.LocalOnly)
		case health.Diverged:
			text = fmt.Sprintf(t.StaleDiverged, s.Branch, s.LocalOnly, b.Remote, s.RemoteOnly)
		default:
			text = fmt.Sprintf(t.StaleUnknown, s.Branch, b.Remote, shortSHA(s.Remote))
		}
		more("  " + text)
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

func daysAgo(t *msg.Text, at, now time.Time) string {
	d := now.Sub(at)
	if d < 24*time.Hour {
		return t.Today
	}
	return t.DaysAgo.N(int(d.Hours() / 24))
}

func shortSHA(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// width 是 s 在终端里占几格（中日韩字符占两格）。
func width(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x2E80 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// padTo 按终端显示宽度在 s 后面补空格到 w 格；已经够宽就原样返回。
func padTo(s string, w int) string {
	return s + strings.Repeat(" ", max(0, w-width(s)))
}

// attention 列出体检里值得注意的几项，给折叠成一行的干净仓库用。
func attention(t *msg.Text, h health.Report) []string {
	var out []string
	if h.Changes > 0 {
		out = append(out, fmt.Sprintf(t.AttnChanges, h.Changes))
	}
	if h.Stashes > 0 {
		out = append(out, fmt.Sprintf(t.AttnStashes, h.Stashes))
	}
	if h.Upstream != "" && !h.UpstreamGone && (h.Ahead > 0 || h.Behind > 0) {
		out = append(out, fmt.Sprintf(t.AttnUpstream, h.Branch, h.Ahead, h.Behind))
	}
	switch b := h.Backup; {
	case h.Note != "":
	case !b.Present:
		if b.Required {
			out = append(out, fmt.Sprintf(t.NoBackup, b.Remote))
		}
	case len(b.Stale) > 0:
		out = append(out, fmt.Sprintf(t.AttnBackupStale, b.Remote, len(b.Stale)))
	case b.OnlineErr != "":
		out = append(out, fmt.Sprintf(t.AttnBackupDown, b.Remote))
	}
	if len(h.Errs) > 0 {
		out = append(out, fmt.Sprintf(t.AttnNotChecked, len(h.Errs)))
	}
	return out
}
