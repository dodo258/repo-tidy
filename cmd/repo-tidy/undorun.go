package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dodo258/repo-tidy/internal/apply"
	"github.com/dodo258/repo-tidy/internal/msg"
	"github.com/dodo258/repo-tidy/internal/report"
)

// runUndo 按恢复日志恢复 --apply 删掉的项；doIt 为 false 时只列出来。
// 退出码：都能恢复（或已经在了）是 0，有恢复不了或失败的是 1，日志找不到、读不懂是 1。
func runUndo(ctx context.Context, paths []string, doIt, asJSON bool, lang msg.Lang, home string, stdout, stderr io.Writer) int {
	t := msg.For(lang)
	fail := func(text string) int { fmt.Fprintln(stderr, t.ErrPrefix+text); return 1 }
	var path string
	if len(paths) == 1 {
		path = paths[0]
	} else {
		p, err := latestLog(t, logDir(home))
		if err != nil {
			return fail(err.Error())
		}
		path = p
	}
	f, err := os.Open(path)
	if err != nil {
		return fail(fmt.Sprintf(t.OpenUndoLog, err))
	}
	items, err := apply.ReadLog(f, lang)
	f.Close() // 只读，关闭出错不影响结果
	if err != nil {
		return fail(err.Error())
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	res := apply.Undo(ctx, items, doIt, lang)
	if asJSON {
		if err := writeUndoJSON(stdout, path, doIt, res); err != nil {
			return fail(fmt.Sprintf(t.WriteJSON, err))
		}
	} else {
		writeUndoText(stdout, t, path, doIt, res)
	}
	for _, r := range res {
		if r.Status == apply.UndoLost || r.Status == apply.UndoFailed {
			return 1
		}
	}
	return 0
}

// latestLog 找 dir 里最新的 apply-<时间>.log（文件名里的时间按字典序就是先后）。
func latestLog(t *msg.Text, dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf(t.FindLog, err)
	}
	var names []string
	for _, e := range entries {
		if n := e.Name(); e.Type().IsRegular() && strings.HasPrefix(n, "apply-") && strings.HasSuffix(n, ".log") {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf(t.NoUndoLog, dir)
	}
	sort.Strings(names)
	return filepath.Join(dir, names[len(names)-1]), nil
}

func undoMark(t *msg.Text, status string) string {
	switch status {
	case apply.UndoReady:
		return t.UndoMarks.Ready
	case apply.UndoRestored:
		return t.UndoMarks.Restored
	case apply.UndoExists:
		return t.UndoMarks.Exists
	case apply.UndoLost:
		return t.UndoMarks.Lost
	}
	return t.UndoMarks.Failed
}

func writeUndoText(w io.Writer, t *msg.Text, path string, doIt bool, res []apply.UndoResult) {
	mode := t.UndoModeView
	if doIt {
		mode = t.UndoModeApply
	}
	fmt.Fprintf(w, t.UndoHeader, mode, path)
	repo, n := "", 0
	for _, r := range res {
		if r.Repo != repo {
			repo = r.Repo
			fmt.Fprintf(w, t.UndoRepo, repo)
		}
		fmt.Fprintf(w, t.ApplyLine, undoMark(t, r.Status), kindName(t, r.Kind), r.Name, r.Msg)
		if r.Status == apply.UndoReady || r.Status == apply.UndoRestored {
			n++
		}
	}
	switch {
	case n == 0:
		fmt.Fprint(w, t.UndoNothing)
	case doIt:
		fmt.Fprintf(w, t.UndoDone, t.Items.N(n))
	default:
		fmt.Fprintf(w, t.UndoHint, t.Items.N(n))
	}
}

type undoJSONDoc struct {
	Version  int              `json:"version"`
	Log      string           `json:"log"`
	Apply    bool             `json:"apply"`
	Restored int              `json:"restored"`
	Results  []undoJSONResult `json:"results"`
}

type undoJSONResult struct {
	Repo    string `json:"repo"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Head    string `json:"head"`
	Branch  string `json:"branch"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func writeUndoJSON(w io.Writer, path string, doIt bool, res []apply.UndoResult) error {
	doc := undoJSONDoc{Version: report.JSONVersion, Log: path, Apply: doIt, Results: []undoJSONResult{}}
	for _, r := range res {
		if r.Status == apply.UndoRestored {
			doc.Restored++
		}
		doc.Results = append(doc.Results, undoJSONResult{Repo: r.Repo, Kind: r.Kind, Name: r.Name, Head: r.Head,
			Branch: r.Branch, Status: r.Status, Message: r.Msg})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(doc)
}
