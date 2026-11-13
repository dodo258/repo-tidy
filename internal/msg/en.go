package msg

var en = Text{
	ErrPrefix: "repo-tidy: ",
	Colon:     "%s: %s",
	ListSep:   ", ",
	AndTotal:  " (%d in total)",
	Paren:     " (%s)",
	Quoted:    "%s \"%s\"",
	Detached:  "detached HEAD",
	Kinds:     struct{ Worktree, Branch string }{"worktree", "branch"},

	Usage: `Usage: repo-tidy [--apply | --json] [--verbose] [--online] [--backup-remote NAME] [--progress] [--min-idle DURATION] [--depth N] [--older-than DURATION] [--only worktrees|branches] [--lang zh|en] [PATH...]
       repo-tidy --undo [LOG] [--apply] [--json] [--lang zh|en]

Scans the extra worktrees and local branches in your repos and sorts them into
"removable" (with evidence) and "keep" (with reasons). Each repo also gets a
checkup: disk usage, uncommitted changes in the main worktree, stashes, the
current branch vs. its upstream, the last commit, and whether the backup remote
(named backup unless set; skipped in repos that don't have it) is up to date
(report only, nothing is changed).
A PATH can be a repo or a directory (the repos below it are scanned; see --depth).
With no PATH, scans $REPO_TIDY_ROOT, or ~/projects if that isn't set.

  --apply    Delete the items marked removable. Each item is re-checked first; items that are
             removable because they were pushed are confirmed online first.
             Restore commands go to ${XDG_STATE_HOME:-~/.local/state}/repo-tidy/apply-<time>.log
  --undo [LOG]
             Restore what an --apply deleted, from its restore log (default: the newest log in
             ${XDG_STATE_HOME:-~/.local/state}/repo-tidy/). Only lists what would be restored;
             add --apply to do it. Branches and directories that already exist are skipped;
             commits that are no longer in the repo (removed by git gc) are reported.
  --json     Print the same verdicts and checkup as JSON (fields in docs/reference.md); with
             --apply, print the deletion results as JSON.
  --verbose  Put the full evidence for removable items on its own line (by default each item
             takes one line with short evidence, like "squashed into main 3702a43d").
  --online   During the checkup, ask the backup remote for its real state (git ls-remote);
             by default compares against the local remote-tracking refs.
  --backup-remote NAME
             Which remote the checkup compares against (default backup). Repos without it
             skip this line; if you pass this flag explicitly (even as backup), a missing
             remote is reported as "no NAME remote" and flagged.
  --progress Write a "Scanning 3/13 name" line to stderr as each repo starts (for callers such
             as the Mac app). Without it, progress is shown on one line only for the text report
             when stderr is a terminal, and erased when the scan is done.
  --min-idle DURATION
             Keep a worktree whose index, HEAD or reflog changed within this long, as it may
             still be in use (default 30m; e.g. 45m, 2h; 0 turns this off). A worktree that is
             some process's working directory is always kept.
  --depth N  How many levels below a directory to look for repos (default 1, at most 4).
             For ~/code/<org>/<repo>, use --depth 2. Repos found are not searched further;
             hidden directories, node_modules and symlinks are skipped.
  --older-than DURATION
             Only show worktrees and branches last active more than this long ago
             (e.g. 30d, 2w, 36h).
  --only worktrees|branches
             Only show extra worktrees, or only local branches.
             Items left out by these two aren't shown or counted in the overview (which says
             "filtered"), and --apply won't delete them.
  --lang zh|en
             Output language. Defaults to LC_ALL, LC_MESSAGES or LANG (zh* is Chinese, anything
             else English). JSON field names and enum values never change; evidence and
             reasons follow the language.
  --version  Show the version
`,
	BadLang:          "--lang must be zh or en, got %q",
	MinIdleNegative:  "--min-idle can't be negative",
	EmptyBackup:      "--backup-remote can't be empty",
	BadDepth:         "--depth must be between 1 and %d, got %d",
	BadOlderThan:     "--older-than must be a positive number with a unit, like 30d, 2w or 36h, got %q",
	BadOnly:          "--only must be worktrees or branches, got %q",
	UndoWithFilter:   "--undo can't be used with --older-than or --only",
	SelectNeedsApply: "--select only works with --apply",
	NoRepos:          "no git repositories found under these paths",
	WriteJSON:        "writing JSON: %v",
	ModeReadOnly:     "read-only report, nothing deleted",
	ModeApply:        "report (--apply: each item is re-checked before it is deleted below)",
	Header:           "repo-tidy %s · %s · %s\n",
	HintApply:        "\nAdd --apply to delete the %s marked removable (each is re-checked first; those removable because they were pushed are confirmed online).\n",
	HintApplyFilter:  "\nAdd --apply with the same filters to delete the %s marked removable (each is re-checked first; those removable because they were pushed are confirmed online). Without the filters, every removable item is deleted.\n",
	Scanning:         "Scanning %d/%d %s",
	CheckupFailed:    "checkup failed: %s",
	OpenSelect:       "opening the --select file: %w",
	SelectEmpty:      "--select lists nothing to delete",
	RepoNotScanned:   "repo couldn't be scanned: %s",
	NotInScan:        "not in any repo scanned this time",
	WriteLog:         "writing the log: %w",
	MakeLogDir:       "creating the log directory: %w",
	OpenLog:          "opening the log: %w",
	NothingToDelete:  "Nothing to delete.",
	Deleting:         "\nDeleting:",
	Deleted:          "deleted",
	Skipped:          "skipped",
	ApplyLine:        "  %s  %s %s: %s\n",
	ApplyDone:        "\nDeleted %s.\n",
	ApplyDoneLog:     "\nDeleted %s. Restore commands are in %s\n",
	Repos:            Count{"%d repo", "%d repos"},
	Items:            Count{"%d item", "%d items"},

	UndoOneLog:     "--undo takes one log path, got %d",
	UndoWithSelect: "--undo can't be used with --select",
	NoUndoLog:      "no restore log in %s (one is written when repo-tidy --apply deletes something)",
	FindLog:        "finding the restore log: %w",
	OpenUndoLog:    "opening the restore log: %w",
	UndoHeader:     "repo-tidy undo · %s · log %s\n",
	UndoModeView:   "preview, nothing changed",
	UndoModeApply:  "restoring",
	UndoRepo:       "\nrepo %s\n",
	UndoMarks:      struct{ Ready, Restored, Exists, Lost, Failed string }{"will restore", "restored", "skipped", "can't restore", "failed"},
	UndoHint:       "\n%s can be restored; add --apply to do it.\n",
	UndoNothing:    "\nNothing to restore.\n",
	UndoDone:       "\nRestored %s.\n",

	OverviewScanned: Count{"scanned %d repo", "scanned %d repos"},
	OverviewClean:   "%d clean",
	OverviewFailed:  "%d failed",
	Overview:        "\nOverview  %s · extra worktrees: %d · local branches: %d\n",
	Frees:           "frees %s",
	Unmeasured:      Count{" (%d more worktree not measured)", " (%d more worktrees not measured)"},
	RemovableLine:   "          removable: %d (worktrees %d, branches %d), %s\n",
	KeptLine:        "          keep: %d (worktrees %d, branches %d)",
	KeptUnmerged:    ", %d with unmerged work",
	FilterLine:      "          filtered: %s (other items aren't shown or counted above, and --apply won't delete them)\n",
	FilterOlder:     "last active more than %s ago",
	FilterOnlyWT:    "extra worktrees only",
	FilterOnlyBr:    "local branches only",

	ScanFailed:       "%s  scan failed: %s\n",
	ExtraWorktrees:   Count{"%d extra worktree", "%d extra worktrees"},
	LocalBranches:    Count{"%d local branch", "%d local branches"},
	DefaultBranch:    " · default branch %s",
	ManyRemovable:    "  removable %s: %s\n",
	SectionWorktrees: "worktrees",
	SectionBranches:  "local branches",
	SectionHead:      "  %s  removable %d · keep %d\n",
	Removable:        "remove",
	Keep:             "keep",
	ActiveToday:      "active today",
	IdleDays:         Count{"idle %d day", "idle %d days"},
	CleanRepos:       "\nClean repos: %d (nothing removable, nothing unmerged):\n",
	Attention:        " · note: %s",
	BasisMerged:      "merged %d",
	BasisPushed:      "pushed %d",
	BasisPrunable:    "dir gone %d",

	Checkup:         "  Checkup",
	RowUsage:        "Disk",
	RowMain:         "Main tree",
	RowLastCommit:   "Last commit",
	RowNotChecked:   "Not checked",
	Usage1:          ".git %s · main tree %s",
	UsageWorktrees:  " · extra worktrees: %d, %s in total",
	DirsGone:        Count{" (%d dir gone)", " (%d dirs gone)"},
	NoUpstream:      "%s, no upstream",
	UpstreamGone:    "%s, upstream %s is gone",
	UpstreamSame:    "%s, in sync with %s",
	UpstreamDiff:    "%s, vs %s: %d ahead, %d behind",
	NoChanges:       "no uncommitted changes",
	ChangesUntrack:  "uncommitted changes: %d (%d untracked)",
	Changes:         "uncommitted changes: %d",
	NoStash:         "no stashes",
	Stashes:         "stashes: %d",
	NoCommits:       "no commits yet",
	LastCommit:      "%s (%s, %s)",
	Today:           "today",
	DaysAgo:         Count{"%d day ago", "%d days ago"},
	NoBackup:        "no %s remote",
	SourceLocal:     "from local records, i.e. as of the last push or fetch",
	SourceOnline:    "checked online",
	Unreachable:     "unreachable: %s",
	BackupAllPushed: "all local branches up to date: %d (%s)",
	BackupStale:     "of %d local branches, %d not up to date (%s):",
	StaleNotPushed:  "not pushed  %s",
	StaleBehind:     "behind  %s: %d local commits not pushed",
	StaleDiverged:   "diverged  %s: %d commits only local, %[4]d only on %[3]s",
	StaleUnknown:    "unknown  %s: %s has %s, which isn't here locally (fetch and check again)",
	AttnChanges:     "uncommitted changes in main tree: %d",
	AttnStashes:     "stashes: %d",
	AttnUpstream:    "%s differs from upstream (%d ahead, %d behind)",
	AttnBackupStale: "branches not up to date on %s: %d",
	AttnBackupDown:  "%s unreachable",
	AttnNotChecked:  "checkup items not checked: %d",

	ListWorktrees:     "listing worktrees: %w",
	NoWorkTree:        "%s has no working tree",
	Bare:              "bare repository, skipped",
	FindGitDir:        "finding the git directory: %w",
	MakeTempObj:       "creating a temporary object directory: %w",
	NoDefault:         "no default branch found (neither locally nor on origin: the branch origin/HEAD points to, main, master); listed without verdicts",
	ListRemotes:       "listing remotes: %w",
	NoDefaultToComp:   "no default branch to compare with",
	CheckFailed:       "check failed, keeping it to be safe: %s",
	ListBranches:      "listing local branches: %w",
	ListDefHistory:    "listing the default branch history: %w",
	ReadDefGraph:      "reading the default branch commit graph: %w",
	NotInHistory:      "commit %s is not in the history of %s",
	PatchID:           "computing patch-ids: %w",
	ReadDefChanges:    "reading default branch changes: %w",
	ReadBranchChanges: "reading branch changes: %w",
	ReadBranchCommits: "reading branch commits: %w",
	ListTracking:      "listing remote-tracking refs: %w",
	CountUnique:       "counting unique commits: %w",

	EvSame:          "points at the same commit as %s: %s",
	EvSameShort:     "same as %s %s",
	EvOnDef:         "already on %s: commit %s",
	EvOnDefShort:    "on %s %s",
	EvMerged:        "merged into %s: commit %s",
	EvMergedShort:   "merged into %s %s",
	EvSquash:        "content squashed into %s: commit %s",
	EvSquashShort:   "squashed into %s %s",
	EvRebase:        "all commits rebased onto %s: %s",
	EvRebaseShort:   "rebased onto %s %s",
	EvNoNet:         "no net changes relative to %s",
	EvContent:       "content is all in %s: from commit %s on, merging it adds nothing",
	EvContentShort:  "content in %s %s",
	EvPushed:        "pushed to %s/%s (the remote commit on record, %s, contains it)",
	EvPushedShort:   "pushed to %s/%s",
	EvPrunable:      "directory no longer exists; only git's record of it is cleared",
	EvPrunableShort: "dir gone, only clears git's record",
	EvRemoveWTFirst: "; remove the worktree that has it checked out first",

	MainBranch:      "checked out in the main worktree",
	InKeptWorktree:  "checked out in a kept worktree: %s",
	NotContained:    "content is not in %s, and no remote ref fully contains it",
	UniqueCommits:   Count{"%d commit is", "%d commits are"},
	NotOnDefault:    "%s neither in %s nor pushed to any remote: %s",
	Locked:          "locked",
	LockedWhy:       "locked: %s",
	CantRead:        "can't read this worktree, keeping it to be safe: %s",
	InProgress:      "has a %s in progress",
	RebaseOrAm:      "rebase or am",
	CantStatus:      "can't read its change status, keeping it to be safe: %s",
	Dirty:           "uncommitted changes or untracked files (%d): %s",
	IgnoredKeep:     "has ignored files that can't be regenerated (no identical copy in the main worktree): %s",
	ProcsUnknown:    "couldn't check for processes working in this directory (%v); assuming it may be in use",
	Proc:            "%s (pid %d)",
	ProcsHere:       "processes are working in this directory, may be in use: %s",
	RecentlyTouched: Count{"changed %d minute ago, may still be in use", "changed %d minutes ago, may still be in use"},

	Recheck:        "re-checking %s: %w",
	LogRepo:        "\n# repo %s (default branch %s)\n# To restore, run the branch commands first, then the worktree ones.\n",
	LogWorktree:    "worktree %s%s — %s\n",
	LogPruned:      "worktree %s (directory gone)%s\n",
	LogBranch:      "branch %s @ %s — %s\n",
	LogRestore:     "  restore: %s\n",
	RefDesc:        " (%s @ %s)",
	RefDetached:    " (detached HEAD @ %s)",
	Gone:           "no longer exists",
	ChangedKeep:    "changed since the report, left alone: now kept, %s",
	Changed:        "changed since the report, left alone",
	Pruned:         "cleared git's record",
	Removed:        "deleted",
	Failed:         "failed: %s",
	ConfirmFailed:  "couldn't confirm %s online, so can't prove the remote still has this commit: %s",
	RemoteGone:     "remote %s no longer has %s, can't prove it's safe",
	RemoteUnknown:  "on remote %s, %s now points at %s, which isn't here locally; fetch first",
	RemoteMoved:    "on remote %s, %s is now %s and no longer contains this commit (force-pushed?)",
	ConfirmErr:     "failed to confirm the remote commit: %v",
	ReadSelected:   "reading the selected items: %w",
	BadWant:        "item %d is missing repo, name or head, or its kind isn't worktree/branch: %+v",
	NowKeep:        "now judged keep, not deleting: %s",
	HeadChanged:    "commit changed since it was selected, left alone",
	StillCheckedIn: "still checked out in worktree %s, which wasn't selected; not deleting",
	ReasonSep:      "; ",

	UndoBadLine:      "can't read line %d of the restore log: %v",
	UndoReadLog:      "reading the restore log: %w",
	UndoEmptyLog:     "the restore log has no restore commands",
	UndoNoRepo:       "the repo is gone: %s",
	UndoBranchSame:   "the branch is already there, at the original commit",
	UndoBranchMoved:  "a branch with this name is already there (now at %s); left it alone",
	UndoLostCommit:   "commit %s is no longer in the repo (most likely removed by git gc); can't restore",
	UndoWillBranch:   "will recreate the branch at %s",
	UndoDidBranch:    "recreated the branch at %s",
	UndoDirExists:    "the directory is already there; left it alone",
	UndoDetached:     "detached HEAD @ %s",
	UndoOnBranch:     "on branch %s",
	UndoNoBranch:     "branch %s is gone and isn't in this log, so this worktree can't be recreated",
	UndoWillWorktree: "will recreate it at its original path (%s)",
	UndoDidWorktree:  "recreated it at its original path (%s)",

	MeasureGitDir:   "measuring the git directory",
	Measure:         "measuring %s",
	CheckChanges:    "checking uncommitted changes",
	CountStashes:    "counting stashes",
	FindBranch:      "finding the current branch",
	FindUpstream:    "finding the upstream",
	CompareUpstream: "comparing with the upstream",
	FindLastCommit:  "finding the last commit",
	ReadBackupRefs:  "reading %s's remote-tracking refs",
	CompareBackup:   "comparing %[2]s with %[1]s",
	ListingRemotes:  "listing remotes",
	ListingBranches: "listing local branches",
	NetTimeout:      "network timeout (%s)",
}
