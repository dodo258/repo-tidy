package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tr "github.com/dodo258/repo-tidy/internal/testrepo"
)

// orphanDirs 造两个仓库：messy 有可删分支和一个登记没了的 worktree 目录；tidy 干净，只剩一个连 .git 都没有的目录。
func orphanDirs(t *testing.T) (root, lost, left string) {
	t.Helper()
	tr.Isolate(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	messy, tidy := filepath.Join(root, "messy"), filepath.Join(root, "tidy")
	tr.Init(t, messy)
	tr.Init(t, tidy)
	tr.Git(t, messy, "branch", "done")
	lost = filepath.Join(messy, ".claude", "worktrees", "lost")
	tr.Git(t, messy, "worktree", "add", "-q", "--detach", lost)
	if err := os.RemoveAll(tr.Git(t, lost, "rev-parse", "--absolute-git-dir")); err != nil {
		t.Fatal(err)
	}
	tr.Write(t, lost, "draft.txt", "只此一份\n")
	left = filepath.Join(tidy, ".claude", "worktrees", "left")
	tr.Write(t, left, "a.txt", strings.Repeat("x", 2048))
	return root, lost, left
}

// 总览、仓库的体检段、折成一行的干净仓库都写孤儿目录；JSON 里的数和文字对得上，也不混进 worktrees。
func TestOrphansReported(t *testing.T) {
	root, lost, left := orphanDirs(t)
	var doc struct {
		Summary struct {
			Worktrees   int   `json:"worktrees"`
			Orphans     int   `json:"orphans"`
			OrphanBytes int64 `json:"orphan_bytes"`
		} `json:"summary"`
		Repos []struct {
			Clean  bool `json:"clean"`
			Health struct {
				Orphans []struct {
					Path       string `json:"path"`
					SizeBytes  int64  `json:"size_bytes"`
					HasGitFile bool   `json:"has_git_file"`
				} `json:"orphans"`
			} `json:"health"`
		} `json:"repos"`
	}
	if err := json.Unmarshal([]byte(runOK(t, "--json", root)), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Repos) != 2 || len(doc.Repos[0].Health.Orphans) != 1 || len(doc.Repos[1].Health.Orphans) != 1 {
		t.Fatalf("两个仓库应各有一个孤儿目录：%+v", doc)
	}
	a, b := doc.Repos[0].Health.Orphans[0], doc.Repos[1].Health.Orphans[0]
	if a.Path != lost || !a.HasGitFile || a.SizeBytes == 0 || b.Path != left || b.HasGitFile || b.SizeBytes != 2048 {
		t.Fatalf("孤儿目录不对：%+v %+v", a, b)
	}
	if s := doc.Summary; s.Worktrees != 0 || s.Orphans != 2 || s.OrphanBytes != a.SizeBytes+b.SizeBytes {
		t.Fatalf("总览不对：%+v", s)
	}
	if doc.Repos[0].Clean || !doc.Repos[1].Clean {
		t.Fatalf("孤儿目录不改变仓库干不干净：%+v", doc.Repos)
	}

	for lang, want := range map[string][]string{
		"zh": {
			"      孤儿目录 2 个，共 2.1 KB（.claude/worktrees 下，git 里已没有登记；只报不删）\n",
			"    孤儿目录  1 个共 ",
			fmt.Sprintf("\n                .claude/worktrees/lost  %d B  .git 文件还在，登记没了\n", a.SizeBytes),
			" · 注意：孤儿目录 1 个共 2.0 KB",
			"\n    .claude/worktrees/left  2.0 KB  没有 .git 文件\n",
		},
		"en": {
			"          orphaned dirs: 2, 2.1 KB in total (under .claude/worktrees, no longer registered in git; reported only, never deleted)\n",
			"    Orphans      orphaned dirs: 1, ",
			fmt.Sprintf(".claude/worktrees/lost  %d B  has a .git file, registration gone\n", a.SizeBytes),
			" · note: orphaned dirs: 1, 2.0 KB in total",
			"\n    .claude/worktrees/left  2.0 KB  no .git file\n",
		},
	} {
		text := runOK(t, "--lang", lang, root)
		for _, w := range want {
			if !strings.Contains(text, w) {
				t.Errorf("缺 %q：\n%s", w, text)
			}
		}
	}
}

// 没有孤儿目录时一个字都不提。
func TestNoOrphansNotMentioned(t *testing.T) {
	tr.Isolate(t)
	repo := filepath.Join(t.TempDir(), "repo")
	tr.Init(t, repo)
	tr.Git(t, repo, "branch", "done")
	if s := runOK(t, "--lang", "zh", repo); strings.Contains(s, "孤儿") {
		t.Fatalf("不该提孤儿目录：\n%s", s)
	}
}

// 只报不删：--apply 不动它们；--select 点名要删也不删。
func TestOrphansNeverDeleted(t *testing.T) {
	root, lost, left := orphanDirs(t)
	snaps := map[string]string{lost: tr.Snapshot(t, lost), left: tr.Snapshot(t, left)}
	if s := runOK(t, "--lang", "zh", "--apply", root); !strings.Contains(s, "删了 1 项") {
		t.Fatalf("只该删 done 分支：\n%s", s)
	}
	messy := filepath.Join(root, "messy")
	sel := filepath.Join(t.TempDir(), "sel.json")
	body, err := json.Marshal(map[string]any{"items": []map[string]string{
		{"repo": messy, "kind": "worktree", "name": lost, "head": tr.Git(t, messy, "rev-parse", "HEAD")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sel, body, 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"--apply", "--select", sel}, &out, &errb); code != 1 {
		t.Fatalf("点名的孤儿目录不该删，退出码应为 1，实际 %d：%s%s", code, out.String(), errb.String())
	}
	for dir, before := range snaps {
		if d := tr.LineDiff(before, tr.Snapshot(t, dir)); d != "" {
			t.Fatalf("%s 被动过：\n%s", dir, d)
		}
	}
}
