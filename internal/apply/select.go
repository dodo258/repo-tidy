package apply

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/dodo258/repo-tidy/internal/judge"
	"github.com/dodo258/repo-tidy/internal/msg"
)

// Want 是要删的一项：哪个仓库、worktree 还是分支、名字，以及挑选时看到的提交。
// 字段名和 --json 输出里的每一项一致，界面可以把勾选的项原样送回来。
type Want struct {
	Repo string `json:"repo"`
	Kind string `json:"kind"`
	Name string `json:"name"`
	Head string `json:"head"`
}

// wantDoc 是 --select 文件的格式。每项除了 Want 的四个字段，还认得 --json 报告里每项的其余字段
// （读了不用），这样从 --json 挑出来的项能原样送回；别的字段（比如以为有用的 force）一律拒绝。
type wantDoc struct {
	Items []struct {
		Want
		Branch     json.RawMessage `json:"branch"`
		Verdict    json.RawMessage `json:"verdict"`
		Basis      json.RawMessage `json:"basis"`
		Evidence   json.RawMessage `json:"evidence"`
		Reasons    json.RawMessage `json:"reasons"`
		Unmerged   json.RawMessage `json:"unmerged"`
		LastActive json.RawMessage `json:"last_active"`
		Remote     json.RawMessage `json:"remote"`
		RemoteRef  json.RawMessage `json:"remote_ref"`
		SizeBytes  json.RawMessage `json:"size_bytes"`
	} `json:"items"`
}

// ReadWants 读 {"items":[Want...]}；每项四个字段都要有，kind 只能是 worktree 或 branch。
// 出错说明用 lang 这种语言。
func ReadWants(r io.Reader, lang msg.Lang) ([]Want, error) {
	t := msg.For(lang)
	var doc wantDoc
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf(t.ReadSelected, err)
	}
	wants := make([]Want, 0, len(doc.Items))
	for i, it := range doc.Items {
		w := it.Want
		if w.Repo == "" || w.Name == "" || w.Head == "" || (w.Kind != judge.KindWorktree && w.Kind != judge.KindBranch) {
			return nil, fmt.Errorf(t.BadWant, i+1, w)
		}
		wants = append(wants, w)
	}
	return wants, nil
}

// Select 只保留 r 里勾选了的可删项，其余一律当作留着；勾选了但这次不能删的
// （已经不在、现在判定要留着、提交和挑选时不一样、分支还检出在没勾选的 worktree 里）记一条「没删」。
// 返回的报告交给 Apply，Apply 删前还会再判定一次。
func Select(r judge.RepoReport, wants []Want, lang msg.Lang) (judge.RepoReport, []Result) {
	t := msg.For(lang)
	picked := map[string]bool{}
	var skipped []Result
	out := r
	out.Worktrees = append([]judge.Item(nil), r.Worktrees...)
	out.Branches = append([]judge.Item(nil), r.Branches...)
	items := map[string][]judge.Item{judge.KindWorktree: out.Worktrees, judge.KindBranch: out.Branches}
	for _, w := range wants {
		it, ok := byName(items[w.Kind], w.Name)
		switch {
		case !ok:
			skipped = append(skipped, Result{w.Kind, w.Name, false, t.Gone})
		case !it.Removable:
			skipped = append(skipped, Result{w.Kind, w.Name, false, fmt.Sprintf(t.NowKeep, strings.Join(it.Reasons, t.ReasonSep))})
		case it.Head != w.Head:
			skipped = append(skipped, Result{w.Kind, w.Name, false, t.HeadChanged})
		default:
			picked[w.Kind+"\x00"+w.Name] = true
		}
	}
	// 分支还检出在某个 worktree 里，而那个 worktree 没一起删，git 会拒绝删分支
	for _, wt := range out.Worktrees {
		key := judge.KindBranch + "\x00" + wt.Branch
		if wt.Branch != "" && picked[key] && !picked[judge.KindWorktree+"\x00"+wt.Name] {
			delete(picked, key)
			skipped = append(skipped, Result{judge.KindBranch, wt.Branch, false, fmt.Sprintf(t.StillCheckedIn, wt.Name)})
		}
	}
	for _, list := range [][]judge.Item{out.Worktrees, out.Branches} {
		for i := range list {
			if !picked[list[i].Kind+"\x00"+list[i].Name] {
				list[i].Removable = false
			}
		}
	}
	return out, skipped
}
