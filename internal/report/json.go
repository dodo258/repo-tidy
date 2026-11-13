package report

import (
	"encoding/json"
	"io"
	"time"

	"github.com/dodo258/repo-tidy/internal/health"
	"github.com/dodo258/repo-tidy/internal/judge"
)

// JSONVersion 是 JSON 输出的格式版本；字段只增不改，改了不兼容的地方才加一。
const JSONVersion = 1

// 结论
const (
	VerdictRemovable = "removable"
	VerdictKeep      = "keep"
)

type jsonDoc struct {
	Version     int        `json:"version"`
	GeneratedAt time.Time  `json:"generated_at"`
	Summary     jsonTotals `json:"summary"`
	Repos       []jsonRepo `json:"repos"`
}

type jsonTotals struct {
	Repos              int         `json:"repos"`
	Failed             int         `json:"failed"`
	Clean              int         `json:"clean"`
	Worktrees          int         `json:"worktrees"`
	Branches           int         `json:"branches"`
	RemovableWorktrees int         `json:"removable_worktrees"`
	RemovableBranches  int         `json:"removable_branches"`
	KeepWorktrees      int         `json:"keep_worktrees"`
	KeepBranches       int         `json:"keep_branches"`
	Unmerged           int         `json:"unmerged"`
	FreeBytes          int64       `json:"free_bytes"`
	Unmeasured         int         `json:"unmeasured"`
	Filter             *jsonFilter `json:"filter"` // 没有筛选条件时为 null
}

type jsonFilter struct {
	OlderThan        string `json:"older_than"`         // 用户写的时长，如 30d；没按时间筛为空
	OlderThanSeconds int64  `json:"older_than_seconds"` // 同一时长换成秒；没按时间筛为 0
	Only             string `json:"only"`               // worktrees、branches，或空（两种都要）
}

type jsonRepo struct {
	Root          string      `json:"root"`
	DefaultBranch string      `json:"default_branch"`
	Note          string      `json:"note"`
	Error         string      `json:"error"`
	Mess          int         `json:"mess"`
	Clean         bool        `json:"clean"`
	Worktrees     []jsonItem  `json:"worktrees"`
	Branches      []jsonItem  `json:"branches"`
	Health        *jsonHealth `json:"health"`
}

type jsonItem struct {
	Repo       string     `json:"repo"`
	Kind       string     `json:"kind"`
	Name       string     `json:"name"`
	Branch     string     `json:"branch"`
	Head       string     `json:"head"`
	Verdict    string     `json:"verdict"`
	Basis      string     `json:"basis"`
	Evidence   string     `json:"evidence"`
	Reasons    []string   `json:"reasons"`
	Unmerged   bool       `json:"unmerged"`
	LastActive *time.Time `json:"last_active"`
	Remote     string     `json:"remote,omitempty"`
	RemoteRef  string     `json:"remote_ref,omitempty"`
	SizeBytes  *int64     `json:"size_bytes,omitempty"`
}

type jsonHealth struct {
	Note             string     `json:"note"`
	GitDirBytes      int64      `json:"git_dir_bytes"`
	MainBytes        int64      `json:"main_bytes"`
	Branch           string     `json:"branch"`
	Upstream         string     `json:"upstream"`
	UpstreamGone     bool       `json:"upstream_gone"`
	Ahead            int        `json:"ahead"`
	Behind           int        `json:"behind"`
	Changes          int        `json:"changes"`
	Untracked        int        `json:"untracked"`
	Stashes          int        `json:"stashes"`
	LastCommit       *time.Time `json:"last_commit"`
	LastCommitBranch string     `json:"last_commit_branch"`
	Backup           jsonBackup `json:"backup"`
	Errors           []string   `json:"errors"`
}

type jsonBackup struct {
	Remote      string           `json:"remote"`
	Required    bool             `json:"required"`
	Present     bool             `json:"present"`
	Online      bool             `json:"online"`
	OnlineError string           `json:"online_error"`
	Checked     int              `json:"checked"`
	Stale       []jsonBranchSync `json:"stale"`
}

type jsonBranchSync struct {
	Branch     string `json:"branch"`
	State      string `json:"state"`
	Local      string `json:"local"`
	Remote     string `json:"remote"`
	LocalOnly  int    `json:"local_only"`
	RemoteOnly int    `json:"remote_only"`
}

// JSON 把和 Text 同样的判定、体检结果写成一个 JSON 文档（字段说明见 docs/reference.md「JSON 输出」）。
// reps 是已经按 f 筛过的结果，f 写进 summary.filter。
func JSON(w io.Writer, reps []Repo, now time.Time, f Filter) error {
	o := Summarize(reps)
	doc := jsonDoc{
		Version: JSONVersion, GeneratedAt: now.Truncate(time.Second), Repos: []jsonRepo{},
		Summary: jsonTotals{
			Repos: o.Repos, Failed: o.Failed, Clean: o.Clean, Worktrees: o.Worktrees, Branches: o.Branches,
			RemovableWorktrees: o.RemovableWT, RemovableBranches: o.RemovableBr, KeepWorktrees: o.KeptWT, KeepBranches: o.KeptBr,
			Unmerged: o.Unmerged, FreeBytes: o.FreeBytes, Unmeasured: o.Unmeasured,
		},
	}
	if f.On() {
		doc.Summary.Filter = &jsonFilter{OlderThan: f.OlderThan, OlderThanSeconds: int64(f.Age / time.Second), Only: f.Only}
	}
	for _, r := range Sort(reps) {
		jr := jsonRepo{Root: r.Root, DefaultBranch: r.DefaultBranch, Note: r.Note, Error: r.Err, Mess: Mess(r), Clean: clean(r)}
		if r.Health != nil {
			jr.Health = healthJSON(*r.Health)
		}
		sizes := worktreeSizes(r)
		jr.Worktrees = itemsJSON(r.Root, r.Worktrees, sizes)
		jr.Branches = itemsJSON(r.Root, r.Branches, nil)
		doc.Repos = append(doc.Repos, jr)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(doc)
}

func itemsJSON(root string, items []judge.Item, sizes map[string]health.Size) []jsonItem {
	out := make([]jsonItem, 0, len(items))
	for _, it := range items {
		ji := jsonItem{
			Repo: root, Kind: it.Kind, Name: it.Name, Branch: it.Branch, Head: it.Head,
			Verdict: VerdictKeep, Basis: string(it.Basis), Evidence: it.Evidence,
			Reasons: append([]string{}, it.Reasons...), Unmerged: it.Unmerged, LastActive: timeOrNil(it.LastActive),
			Remote: it.Remote, RemoteRef: it.RemoteRef,
		}
		if it.Removable {
			ji.Verdict = VerdictRemovable
		}
		if sz, ok := sizes[it.Name]; ok && !sz.Missing {
			ji.SizeBytes = &sz.Bytes
		}
		out = append(out, ji)
	}
	return out
}

func healthJSON(h health.Report) *jsonHealth {
	jh := &jsonHealth{
		Note: h.Note, GitDirBytes: h.GitDir.Bytes, MainBytes: h.Main.Bytes,
		Branch: h.Branch, Upstream: h.Upstream, UpstreamGone: h.UpstreamGone, Ahead: h.Ahead, Behind: h.Behind,
		Changes: h.Changes, Untracked: h.Untracked, Stashes: h.Stashes,
		LastCommit: timeOrNil(h.LastCommit), LastCommitBranch: h.LastCommitBranch,
		Backup: jsonBackup{
			Remote: h.Backup.Remote, Required: h.Backup.Required, Present: h.Backup.Present, Online: h.Backup.Online, OnlineError: h.Backup.OnlineErr,
			Checked: h.Backup.Checked, Stale: []jsonBranchSync{},
		},
		Errors: append([]string{}, h.Errs...),
	}
	for _, s := range h.Backup.Stale {
		jh.Backup.Stale = append(jh.Backup.Stale, jsonBranchSync{
			Branch: s.Branch, State: string(s.State), Local: s.Local, Remote: s.Remote,
			LocalOnly: s.LocalOnly, RemoteOnly: s.RemoteOnly,
		})
	}
	return jh
}

func timeOrNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
