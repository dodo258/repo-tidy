// Package judge 判定一个仓库里的附加 worktree 和本地分支能不能删：
// 能删的给出证据（内容在默认分支哪个提交里、推到了哪个远程），不能删的写明原因。
// 只读：不 fetch、不刷新 index；模拟合并产生的对象写进临时目录，不进仓库。
package judge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dodo258/repo-tidy/internal/gitx"
	"github.com/dodo258/repo-tidy/internal/msg"
)

// Basis 是一条判定的依据。
type Basis string

const (
	Merged   Basis = "merged"   // 内容已在默认分支
	Pushed   Basis = "pushed"   // 已推到远程，本地没有独有提交
	Prunable Basis = "prunable" // worktree 目录已不存在，只剩登记
	Keep     Basis = "keep"     // 不能证明安全，留着
)

// Kind 区分 worktree 和分支。
const (
	KindWorktree = "worktree"
	KindBranch   = "branch"
)

// Item 是一个 worktree 或本地分支的判定结果。
type Item struct {
	Kind       string
	Name       string // 分支名，或 worktree 的绝对路径
	Branch     string // worktree 检出的分支；分离头指针时为空
	Head       string // 提交号
	Removable  bool
	Basis      Basis
	Evidence   string   // 可删时的证据
	Short      string   // 证据的短形式，给文字报告默认一行用（如「squash 进 main 3702a43d」）
	Reasons    []string // 留着的原因
	Unmerged   bool     // 留着，且有不在默认分支、也没推到远程的内容（独有提交或未提交改动）
	LastActive time.Time
	Remote     string // Basis 为 Pushed 时：远程名
	RemoteRef  string // Basis 为 Pushed 时：远程上的引用，如 refs/heads/agent/x
}

// RepoReport 是一个仓库的判定结果。
type RepoReport struct {
	Root          string // 主工作区路径
	DefaultBranch string
	Note          string // 只列不判时的说明
	Err           string // 整个仓库没扫成时的错误
	Worktrees     []Item // 附加 worktree，不含主工作区
	Branches      []Item // 本地分支，不含默认分支
}

// Inspect 扫描 dir 所在的仓库。opts 决定怎么查 worktree 是不是还在用。
func Inspect(ctx context.Context, dir string, opts Options) (RepoReport, error) {
	t := msg.For(opts.Lang)
	wts, err := gitx.Worktrees(ctx, dir)
	if err != nil {
		return RepoReport{}, fmt.Errorf(t.ListWorktrees, err)
	}
	if len(wts) == 0 {
		return RepoReport{}, fmt.Errorf(t.NoWorkTree, dir)
	}
	rep := RepoReport{Root: wts[0].Path}
	if wts[0].Bare {
		rep.Note = t.Bare
		return rep, nil
	}
	root := rep.Root
	common, err := gitx.Run(ctx, root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return rep, fmt.Errorf(t.FindGitDir, err)
	}
	tmpObj, err := os.MkdirTemp("", "repo-tidy-obj-")
	if err != nil {
		return rep, fmt.Errorf(t.MakeTempObj, err)
	}
	defer os.RemoveAll(tmpObj)

	c := &checker{
		ctx:      ctx,
		root:     root,
		objEnv:   []string{"GIT_OBJECT_DIRECTORY=" + tmpObj, "GIT_ALTERNATE_OBJECT_DIRECTORIES=" + filepath.Join(common, "objects")},
		trees:    map[string]string{},
		subjects: map[string]string{},
		defPIDs:  map[string]map[string]string{},
		opts:     opts,
		t:        t,
		now:      time.Now(),
	}
	if len(wts) > 1 { // 有附加 worktree 才要查进程
		list := opts.Procs
		if list == nil {
			list = ListProcs
		}
		c.procs, c.procErr = list(ctx)
	}
	branches, err := c.localBranches()
	if err != nil {
		return rep, err
	}
	c.def, c.defRef, c.defTip = defaultBranch(ctx, root, branches)
	rep.DefaultBranch = c.def
	if c.def == "" {
		rep.Note = t.NoDefault
	} else {
		if c.remotes, err = gitx.Run(ctx, root, "remote"); err != nil {
			return rep, fmt.Errorf(t.ListRemotes, err)
		}
	}

	proofs := map[string]proof{} // 按分支名缓存
	statusOf := func(sha, branch string) proof {
		if c.def == "" {
			return proof{basis: Keep, reasons: []string{t.NoDefaultToComp}}
		}
		if p, ok := proofs[branch]; ok && branch != "" {
			return p
		}
		p, err := c.commitStatus(sha, branch)
		if err != nil {
			p = proof{basis: Keep, reasons: []string{fmt.Sprintf(t.CheckFailed, err)}}
		}
		if branch != "" {
			proofs[branch] = p
		}
		return p
	}

	checkedOut := map[string]Item{} // 分支名 → 检出它的附加 worktree
	mainBranch := wts[0].Branch
	checks := c.checkWorktrees(wts[1:])
	for i, w := range wts[1:] {
		it := c.worktreeItem(checks[i], statusOf)
		rep.Worktrees = append(rep.Worktrees, it)
		if w.Branch != "" {
			checkedOut[w.Branch] = it
		}
	}
	for _, name := range c.branchOrder {
		if "refs/heads/"+name == c.defRef {
			continue
		}
		b := branches[name]
		it := Item{Kind: KindBranch, Name: name, Branch: name, Head: b.sha, LastActive: b.when}
		p := statusOf(b.sha, name)
		wt, hasWT := checkedOut[name]
		switch {
		case name == mainBranch:
			it.Basis, it.Reasons = Keep, []string{t.MainBranch}
		case hasWT && !wt.Removable:
			it.Basis, it.Reasons = Keep, []string{fmt.Sprintf(t.InKeptWorktree, relTo(root, wt.Name))}
			if p.basis == Keep {
				it.Reasons = append(it.Reasons, p.reasons...)
				it.Unmerged = p.unmerged
			}
		default:
			it.apply(p)
			if it.Removable && hasWT {
				it.Evidence += t.EvRemoveWTFirst
				it.Short += t.EvRemoveWTFirst
			}
		}
		rep.Branches = append(rep.Branches, it)
	}
	return rep, nil
}

type proof struct {
	basis     Basis
	evidence  string
	short     string
	reasons   []string
	remote    string
	remoteRef string
	unmerged  bool // 有独有提交：不在默认分支、也没有远程引用包含
}

func (it *Item) apply(p proof) {
	it.Basis = p.basis
	it.Removable = p.basis == Merged || p.basis == Pushed
	it.Evidence, it.Short = p.evidence, p.short
	it.Reasons = append(it.Reasons, p.reasons...)
	it.Remote, it.RemoteRef = p.remote, p.remoteRef
	it.Unmerged = p.unmerged
}

type branchRef struct {
	sha  string
	when time.Time
}

type checker struct {
	ctx          context.Context
	root         string
	def          string // 默认分支的显示名：main，或远程跟踪引用 origin/main
	defRef       string // 默认分支的完整引用：refs/heads/main 或 refs/remotes/origin/main
	defTip       string
	remotes      string
	objEnv       []string
	branchOrder  []string
	introducedBy map[string]string            // 默认分支上的提交 → 第一父链上最早包含它的提交
	trees        map[string]string            // 提交 → 树
	subjects     map[string]string            // 提交 → 标题
	defPIDs      map[string]map[string]string // 合并基点 → (patch-id → 默认分支提交)
	opts         Options
	t            *msg.Text
	now          time.Time
	procs        []Proc // 进程和工作目录，查 worktree 是不是在用
	procErr      error  // 没查成进程时的错误
}

func (c *checker) git(args ...string) (string, error) { return gitx.Run(c.ctx, c.root, args...) }

func (c *checker) localBranches() (map[string]branchRef, error) {
	out, err := c.git("for-each-ref", "--format=%(refname)%00%(objectname)%00%(committerdate:unix)", "refs/heads")
	if err != nil {
		return nil, fmt.Errorf(c.t.ListBranches, err)
	}
	m := map[string]branchRef{}
	for _, l := range gitx.Lines(out) {
		f := strings.Split(l, "\x00")
		if len(f) != 3 {
			continue
		}
		name := strings.TrimPrefix(f[0], "refs/heads/")
		sec, _ := strconv.ParseInt(f[2], 10, 64)
		m[name] = branchRef{sha: f[1], when: time.Unix(sec, 0)}
		c.branchOrder = append(c.branchOrder, name)
	}
	return m, nil
}

// defaultBranch 返回默认分支的显示名、完整引用和末端提交；找不到时都为空。
// 本地分支优先：origin/HEAD 指向的、main、master；本地都没有时依次用远程跟踪引用
// origin/HEAD 指向的、origin/main、origin/master。
func defaultBranch(ctx context.Context, root string, branches map[string]branchRef) (name, ref, sha string) {
	var head string
	if r, err := gitx.Run(ctx, root, "symbolic-ref", "-q", "refs/remotes/origin/HEAD"); err == nil {
		head = strings.TrimPrefix(r, "refs/remotes/origin/")
	}
	cands := []string{head, "main", "master"}
	for _, n := range cands {
		if b, ok := branches[n]; ok && n != "" {
			return n, "refs/heads/" + n, b.sha
		}
	}
	for _, n := range cands {
		if n == "" {
			continue
		}
		ref := "refs/remotes/origin/" + n
		if sha, err := gitx.Run(ctx, root, "rev-parse", "--verify", "-q", ref+"^{commit}"); err == nil {
			return "origin/" + n, ref, sha
		}
	}
	return "", "", ""
}

func (c *checker) isAncestor(a, b string) (bool, error) {
	_, code, err := gitx.Exec(c.ctx, c.root, gitx.Opts{}, "merge-base", "--is-ancestor", a, b)
	switch code {
	case 0:
		return true, nil
	case 1:
		return false, nil
	}
	return false, err
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// describe 返回提交号加标题，如「3702a43d「合并 导出 CSV」」。
func (c *checker) describe(sha string) string {
	s, ok := c.subjects[sha]
	if !ok {
		s, _ = c.git("log", "-1", "--format=%s", sha)
		if r := []rune(s); len(r) > 40 {
			s = string(r[:40]) + "…"
		}
		c.subjects[sha] = s
	}
	if s == "" {
		return short(sha)
	}
	return fmt.Sprintf(c.t.Quoted, short(sha), s)
}

// firstTrue 在 xs 上二分，找第一个 pred 为真的位置；调用方保证最后一个为真。
// pred 不单调时也只会返回一个 pred 为真的位置，证据依然成立。
func firstTrue(xs []string, pred func(string) (bool, error)) (string, error) {
	lo, hi := 0, len(xs)-1
	for lo < hi {
		mid := (lo + hi) / 2
		ok, err := pred(xs[mid])
		if err != nil {
			return "", err
		}
		if ok {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return xs[lo], nil
}

// firstContaining 返回默认分支第一父链上最早包含 tip 的提交（tip 是默认分支末端的祖先）。
// 第一次调用时读一遍默认分支的提交图，算出每个提交是第一父链上哪个提交带进来的；
// 之后每个分支查表，不用逐个分支在链上二分、跑十几次 merge-base。
func (c *checker) firstContaining(tip string) (string, error) {
	if c.introducedBy == nil {
		out, err := c.git("rev-list", "--first-parent", "--reverse", c.defRef)
		if err != nil {
			return "", fmt.Errorf(c.t.ListDefHistory, err)
		}
		chain := gitx.Lines(out)
		out, err = c.git("rev-list", "--parents", c.defRef)
		if err != nil {
			return "", fmt.Errorf(c.t.ReadDefGraph, err)
		}
		parents := map[string][]string{}
		for _, l := range gitx.Lines(out) {
			f := strings.Fields(l)
			parents[f[0]] = f[1:]
		}
		// 链从旧到新：链上第 k 个提交带进来的，是它的祖先里前面的链提交都没包含的那些
		by := make(map[string]string, len(parents))
		for _, x := range chain {
			stack := []string{x}
			for len(stack) > 0 {
				y := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if _, done := by[y]; done {
					continue
				}
				by[y] = x
				stack = append(stack, parents[y]...)
			}
		}
		c.introducedBy = by
	}
	at, ok := c.introducedBy[tip]
	if !ok {
		return "", fmt.Errorf(c.t.NotInHistory, short(tip), c.def)
	}
	return at, nil
}

func (c *checker) tree(sha string) (string, error) {
	if t, ok := c.trees[sha]; ok {
		return t, nil
	}
	t, err := c.git("rev-parse", sha+"^{tree}")
	if err != nil {
		return "", err
	}
	c.trees[sha] = t
	return t, nil
}

// mergeAddsNothing：把 tip 模拟合并进 base，结果树和 base 的树相同。
func (c *checker) mergeAddsNothing(base, tip string) (bool, error) {
	out, code, err := gitx.Exec(c.ctx, c.root, gitx.Opts{Env: c.objEnv}, "merge-tree", "--write-tree", "--no-messages", base, tip)
	switch code {
	case 0:
	case 1:
		return false, nil // 有冲突
	default:
		return false, err
	}
	bt, err := c.tree(base)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(strings.SplitN(out, "\n", 2)[0]) == bt, nil
}

func (c *checker) patchIDs(input string) (map[string]string, []string, error) {
	if strings.TrimSpace(input) == "" {
		return map[string]string{}, nil, nil
	}
	out, _, err := gitx.Exec(c.ctx, c.root, gitx.Opts{Stdin: input}, "patch-id", "--stable")
	if err != nil {
		return nil, nil, fmt.Errorf(c.t.PatchID, err)
	}
	m := map[string]string{}
	var order []string
	for _, l := range gitx.Lines(out) {
		pid, commit, _ := strings.Cut(l, " ")
		if _, dup := m[pid]; !dup {
			m[pid] = commit
		}
		order = append(order, pid)
	}
	return m, order, nil
}

// defPatchIDs：默认分支在合并基点之后的提交（最多 2000 个）的 patch-id。
func (c *checker) defPatchIDs(mb string) (map[string]string, error) {
	if m, ok := c.defPIDs[mb]; ok {
		return m, nil
	}
	log, err := c.git("log", "--no-merges", "--no-color", "--no-ext-diff", "-p", "--format=commit %H", "--max-count=2000", mb+".."+c.defRef)
	if err != nil {
		return nil, fmt.Errorf(c.t.ReadDefChanges, err)
	}
	m, _, err := c.patchIDs(log + "\n")
	if err != nil {
		return nil, err
	}
	c.defPIDs[mb] = m
	return m, nil
}

// commitStatus 判定提交 tip（分支 branch 的末端，分离头指针时 branch 为空）。
func (c *checker) commitStatus(tip, branch string) (proof, error) {
	p, ok, err := c.mergedProof(tip)
	if err != nil || ok {
		return p, err
	}
	if p, ok, err := c.pushedProof(tip, branch); err != nil || ok {
		return p, err
	}
	return c.unmergedReason(tip)
}

func (c *checker) mergedProof(tip string) (proof, bool, error) {
	def, t := c.def, c.t
	// merged 用长、短两种说法写证据：长的带提交标题，短的只带提交号
	merged := func(long, brief, at, describe string) (proof, bool, error) {
		p := proof{basis: Merged, evidence: fmt.Sprintf(long, def, describe), short: fmt.Sprintf(brief, def, short(at))}
		return p, true, nil
	}
	if tip == c.defTip {
		return merged(t.EvSame, t.EvSameShort, tip, short(tip))
	}
	anc, err := c.isAncestor(tip, c.defTip)
	if err != nil {
		return proof{}, false, err
	}
	if anc {
		at, err := c.firstContaining(tip)
		if err != nil {
			return proof{}, false, err
		}
		if at == tip {
			return merged(t.EvOnDef, t.EvOnDefShort, at, c.describe(at))
		}
		return merged(t.EvMerged, t.EvMergedShort, at, c.describe(at))
	}

	mb, code, err := gitx.Exec(c.ctx, c.root, gitx.Opts{}, "merge-base", c.defTip, tip)
	if code == 1 {
		return proof{}, false, nil // 没有共同历史
	}
	if err != nil {
		return proof{}, false, err
	}
	mb = strings.TrimSpace(mb)

	defPIDs, err := c.defPatchIDs(mb)
	if err != nil {
		return proof{}, false, err
	}
	// squash：整段改动的 patch-id 等于默认分支上某个提交
	diff, err := c.git("diff", "--no-color", "--no-ext-diff", mb, tip)
	if err != nil {
		return proof{}, false, fmt.Errorf(t.ReadBranchChanges, err)
	}
	if _, order, err := c.patchIDs(diff + "\n"); err != nil {
		return proof{}, false, err
	} else if len(order) == 1 {
		if at, ok := defPIDs[order[0]]; ok {
			return merged(t.EvSquash, t.EvSquashShort, at, c.describe(at))
		}
	}
	// rebase：分支上每个提交在默认分支上都有同 patch-id 的提交
	blog, err := c.git("log", "--no-merges", "--no-color", "--no-ext-diff", "-p", "--format=commit %H", mb+".."+tip)
	if err != nil {
		return proof{}, false, fmt.Errorf(t.ReadBranchCommits, err)
	}
	if _, order, err := c.patchIDs(blog + "\n"); err != nil {
		return proof{}, false, err
	} else if len(order) > 0 {
		var hits []string
		for _, pid := range order {
			at, ok := defPIDs[pid]
			if !ok {
				hits = nil
				break
			}
			hits = append(hits, at)
		}
		if hits != nil {
			d := c.describe(hits[0])
			if len(hits) > 1 {
				d += fmt.Sprintf(t.AndTotal, len(hits))
			}
			return merged(t.EvRebase, t.EvRebaseShort, hits[0], d)
		}
	}
	// 模拟合并不增加内容：覆盖 squash 后默认分支又改过、分几次合并等情况
	ok, err := c.mergeAddsNothing(c.defTip, tip)
	if err != nil || !ok {
		return proof{}, false, err
	}
	out, err := c.git("rev-list", "--first-parent", "--reverse", mb+".."+c.defRef)
	if err != nil {
		return proof{}, false, err
	}
	chain := gitx.Lines(out)
	if len(chain) == 0 {
		ev := fmt.Sprintf(t.EvNoNet, def)
		return proof{basis: Merged, evidence: ev, short: ev}, true, nil
	}
	at, err := firstTrue(chain, func(x string) (bool, error) { return c.mergeAddsNothing(x, tip) })
	if err != nil {
		return proof{}, false, err
	}
	return merged(t.EvContent, t.EvContentShort, at, c.describe(at))
}

// pushedProof：某个远程跟踪引用包含 tip。优先和分支同名的引用。
func (c *checker) pushedProof(tip, branch string) (proof, bool, error) {
	out, err := c.git("for-each-ref", "--contains", tip, "--format=%(refname) %(objectname)", "refs/remotes")
	if err != nil {
		return proof{}, false, fmt.Errorf(c.t.ListTracking, err)
	}
	var best, bestSHA string
	for _, l := range gitx.Lines(out) {
		ref, sha, _ := strings.Cut(l, " ")
		if strings.HasSuffix(ref, "/HEAD") {
			continue
		}
		remote, name := c.splitRemoteRef(ref)
		if remote == "" {
			continue
		}
		if best == "" || (name == branch && !strings.HasSuffix(best, "/"+branch)) {
			best, bestSHA = ref, sha
		}
	}
	if best == "" {
		return proof{}, false, nil
	}
	remote, name := c.splitRemoteRef(best)
	return proof{
		basis: Pushed, remote: remote, remoteRef: "refs/heads/" + name,
		evidence: fmt.Sprintf(c.t.EvPushed, remote, name, short(bestSHA)),
		short:    fmt.Sprintf(c.t.EvPushedShort, remote, name),
	}, true, nil
}

// splitRemoteRef 把 refs/remotes/<远程>/<分支> 拆开；远程名可能带斜杠，取最长匹配。
func (c *checker) splitRemoteRef(ref string) (string, string) {
	rest := strings.TrimPrefix(ref, "refs/remotes/")
	var remote string
	for _, r := range gitx.Lines(c.remotes) {
		if strings.HasPrefix(rest, r+"/") && len(r) > len(remote) {
			remote = r
		}
	}
	if remote == "" {
		return "", ""
	}
	return remote, strings.TrimPrefix(rest, remote+"/")
}

func (c *checker) unmergedReason(tip string) (proof, error) {
	not := []string{tip, "--not", c.defRef, "--remotes"}
	n, err := c.git(append([]string{"rev-list", "--count"}, not...)...)
	if err != nil {
		return proof{}, fmt.Errorf(c.t.CountUnique, err)
	}
	if n == "0" {
		return proof{basis: Keep, unmerged: true, reasons: []string{fmt.Sprintf(c.t.NotContained, c.def)}}, nil
	}
	out, err := c.git(append([]string{"log", "--format=%H", "--max-count=3"}, not...)...)
	if err != nil {
		return proof{}, err
	}
	var ds []string
	for _, sha := range gitx.Lines(out) {
		ds = append(ds, c.describe(sha))
	}
	nn, _ := strconv.Atoi(n)
	r := fmt.Sprintf(c.t.NotOnDefault, c.t.UniqueCommits.N(nn), c.def, strings.Join(ds, c.t.ReasonSep))
	if nn > len(ds) {
		r += "…"
	}
	return proof{basis: Keep, unmerged: true, reasons: []string{r}}, nil
}

// relTo：p 在 root 里就返回相对路径，否则原样返回。
func relTo(root, p string) string {
	if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return p
}
