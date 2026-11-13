// Package msg 是命令行所有给人看的文案：中文一套（zh.go）、英文一套（en.go），按语言取。
// 各模块只从这里取文案，不自己写；JSON 的字段名和枚举值不在这里，不随语言变。
package msg

import (
	"fmt"
	"strings"
)

// Lang 是输出语言。零值是中文。
type Lang int

const (
	ZH Lang = iota
	EN
)

// String 返回 --lang 的写法。
func (l Lang) String() string {
	if l == EN {
		return "en"
	}
	return "zh"
}

// Parse 认 --lang 的值：zh 或 en。
func Parse(s string) (Lang, bool) {
	switch strings.ToLower(s) {
	case "zh":
		return ZH, true
	case "en":
		return EN, true
	}
	return EN, false
}

// FromEnv 按 POSIX 的优先级取 LC_ALL、LC_MESSAGES、LANG 里第一个非空的：zh 开头用中文，其余（含没设）用英文。
func FromEnv(getenv func(string) string) Lang {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := getenv(k); v != "" {
			if strings.HasPrefix(strings.ToLower(v), "zh") {
				return ZH
			}
			return EN
		}
	}
	return EN
}

// For 返回一种语言的文案。
func For(l Lang) *Text {
	if l == EN {
		return &en
	}
	return &zh
}

// Count 是带数量的说法，One 和 Many 各含一个 %d：英文单复数不同，中文两个一样。
type Count struct{ One, Many string }

// N 按数量选说法并填进数字。
func (c Count) N(n int) string {
	if n == 1 {
		return fmt.Sprintf(c.One, n)
	}
	return fmt.Sprintf(c.Many, n)
}

func same(s string) Count { return Count{s, s} }

// List 用这种语言的顿号/逗号连起来，超过 max 项只列前 max 项再注明总数。
func (t *Text) List(xs []string, max int) string {
	if len(xs) <= max {
		return strings.Join(xs, t.ListSep)
	}
	return strings.Join(xs[:max], t.ListSep) + fmt.Sprintf(t.AndTotal, len(xs))
}

// Text 是一种语言的全部文案。string 字段是 fmt 格式串，两种语言的参数个数、顺序和动词一致
// （msg_test 会查）；Usage 原样输出，不当格式串。
type Text struct {
	// 通用
	ErrPrefix string // 命令行报错的开头
	Colon     string // 「名字：说明」
	ListSep   string // 列举时的分隔
	AndTotal  string // 列举截断后注明总数
	Paren     string // 附加说明的括号
	Quoted    string // 提交号加标题
	Detached  string
	Kinds     struct{ Worktree, Branch string }

	// cmd：帮助、参数、报告头尾、删除结果
	Usage            string
	BadLang          string
	MinIdleNegative  string
	EmptyBackup      string
	BadDepth         string
	BadOlderThan     string
	BadOnly          string
	UndoWithFilter   string
	SelectNeedsApply string
	NoRepos          string
	WriteJSON        string
	ModeReadOnly     string
	ModeApply        string
	Header           string
	HintApply        string
	HintApplyFilter  string
	Scanning         string
	CheckupFailed    string
	OpenSelect       string
	SelectEmpty      string
	RepoNotScanned   string
	NotInScan        string
	WriteLog         string
	MakeLogDir       string
	OpenLog          string
	NothingToDelete  string
	Deleting         string
	Deleted, Skipped string
	ApplyLine        string
	ApplyDone        string
	ApplyDoneLog     string
	Repos, Items     Count

	// cmd：--undo
	UndoOneLog     string
	UndoWithSelect string
	NoUndoLog      string
	FindLog        string
	OpenUndoLog    string
	UndoHeader     string
	UndoModeView   string
	UndoModeApply  string
	UndoRepo       string
	UndoMarks      struct{ Ready, Restored, Exists, Lost, Failed string }
	UndoHint       string
	UndoNothing    string
	UndoDone       string

	// report：总览
	OverviewScanned Count
	OverviewClean   string
	OverviewFailed  string
	Overview        string
	Frees           string
	Unmeasured      Count
	RemovableLine   string
	KeptLine        string
	KeptUnmerged    string
	FilterLine      string
	FilterOlder     string
	FilterOnlyWT    string
	FilterOnlyBr    string

	// report：每个仓库
	ScanFailed       string
	ExtraWorktrees   Count
	LocalBranches    Count
	DefaultBranch    string
	ManyRemovable    string
	SectionWorktrees string
	SectionBranches  string
	SectionHead      string
	Removable, Keep  string
	ActiveToday      string
	IdleDays         Count
	CleanRepos       string
	Attention        string
	BasisMerged      string
	BasisPushed      string
	BasisPrunable    string

	// report：体检
	Checkup         string
	RowUsage        string
	RowMain         string
	RowLastCommit   string
	RowNotChecked   string
	Usage1          string
	UsageWorktrees  string
	DirsGone        Count
	NoUpstream      string
	UpstreamGone    string
	UpstreamSame    string
	UpstreamDiff    string
	NoChanges       string
	ChangesUntrack  string
	Changes         string
	NoStash         string
	Stashes         string
	NoCommits       string
	LastCommit      string
	Today           string
	DaysAgo         Count
	NoBackup        string
	SourceLocal     string
	SourceOnline    string
	Unreachable     string
	BackupAllPushed string
	BackupStale     string
	StaleNotPushed  string
	StaleBehind     string
	StaleDiverged   string
	StaleUnknown    string
	AttnChanges     string
	AttnStashes     string
	AttnUpstream    string
	AttnBackupStale string
	AttnBackupDown  string
	AttnNotChecked  string

	// judge：出错
	ListWorktrees     string
	NoWorkTree        string
	Bare              string
	FindGitDir        string
	MakeTempObj       string
	NoDefault         string
	ListRemotes       string
	NoDefaultToComp   string
	CheckFailed       string
	ListBranches      string
	ListDefHistory    string
	ReadDefGraph      string
	NotInHistory      string
	PatchID           string
	ReadDefChanges    string
	ReadBranchChanges string
	ReadBranchCommits string
	ListTracking      string
	CountUnique       string

	// judge：证据（长的给 --verbose 和 JSON，短的给默认一行）
	EvSame, EvSameShort         string
	EvOnDef, EvOnDefShort       string
	EvMerged, EvMergedShort     string
	EvSquash, EvSquashShort     string
	EvRebase, EvRebaseShort     string
	EvNoNet                     string
	EvContent, EvContentShort   string
	EvPushed, EvPushedShort     string
	EvPrunable, EvPrunableShort string
	EvRemoveWTFirst             string

	// judge：留着的原因
	MainBranch      string
	InKeptWorktree  string
	NotContained    string
	UniqueCommits   Count
	NotOnDefault    string
	Locked          string
	LockedWhy       string
	CantRead        string
	InProgress      string
	RebaseOrAm      string
	CantStatus      string
	Dirty           string
	IgnoredKeep     string
	ProcsUnknown    string
	Proc            string
	ProcsHere       string
	RecentlyTouched Count

	// apply
	Recheck        string
	LogRepo        string
	LogWorktree    string
	LogPruned      string
	LogBranch      string
	LogRestore     string
	RefDesc        string
	RefDetached    string
	Gone           string
	ChangedKeep    string
	Changed        string
	Pruned         string
	Removed        string
	Failed         string
	ConfirmFailed  string
	RemoteGone     string
	RemoteUnknown  string
	RemoteMoved    string
	ConfirmErr     string
	ReadSelected   string
	BadWant        string
	NowKeep        string
	HeadChanged    string
	StillCheckedIn string
	ReasonSep      string

	// apply：撤销
	UndoBadLine      string
	UndoReadLog      string
	UndoEmptyLog     string
	UndoNoRepo       string
	UndoBranchSame   string
	UndoBranchMoved  string
	UndoLostCommit   string
	UndoWillBranch   string
	UndoDidBranch    string
	UndoDirExists    string
	UndoDetached     string
	UndoOnBranch     string
	UndoNoBranch     string
	UndoWillWorktree string
	UndoDidWorktree  string

	// health
	MeasureGitDir   string
	Measure         string
	CheckChanges    string
	CountStashes    string
	FindBranch      string
	FindUpstream    string
	CompareUpstream string
	FindLastCommit  string
	ReadBackupRefs  string
	CompareBackup   string
	ListingRemotes  string
	ListingBranches string
	NetTimeout      string
}
