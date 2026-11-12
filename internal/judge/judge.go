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

// Inspect 扫描 dir 所在的仓库。
func Inspect(ctx context.Context, dir string) (RepoReport, error) {
	wts, err := gitx.Worktrees(ctx, dir)
	if err != nil {
		return RepoReport{}, fmt.Errorf("列 worktree：%w", err)
	}
	if len(wts) == 0 {
		return RepoReport{}, fmt.Errorf("%s 没有工作区", dir)
	}
	rep := RepoReport{Root: wts[0].Path}
	if wts[0].Bare {
		rep.Note = "裸仓库，跳过"
		return rep, nil
	}
	root := rep.Root
	common, err := gitx.Run(ctx, root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return rep, fmt.Errorf("找 git 目录：%w", err)
	}
	tmpObj, err := os.MkdirTemp("", "repo-tidy-obj-")
	if err != nil {
		return rep, fmt.Errorf("建临时对象目录：%w", err)
	}
	defer os.RemoveAll(tmpObj)

	c := &checker{
		ctx:      ctx,
		root:     root,
		objEnv:   []string{"GIT_OBJECT_DIRECTORY=" + tmpObj, "GIT_ALTERNATE_OBJECT_DIRECTORIES=" + filepath.Join(common, "objects")},
		trees:    map[string]string{},
		subjects: map[string]string{},
		defPIDs:  map[string]map[string]string{},
	}
	branches, err := c.localBranches()
	if err != nil {
		return rep, err
	}
	c.def = defaultBranch(ctx, root, branches)
	rep.DefaultBranch = c.def
	if c.def == "" {
		rep.Note = "找不到默认分支（origin/HEAD、main、master 都没有），只列出不判定"
	} else {
		c.defTip = branches[c.def].sha
		if c.remotes, err = gitx.Run(ctx, root, "remote"); err != nil {
			return rep, fmt.Errorf("列远程：%w", err)
		}
	}

	proofs := map[string]proof{} // 按分支名缓存
	statusOf := func(sha, branch string) proof {
		if c.def == "" {
			return proof{basis: Keep, reasons: []string{"没有默认分支可比对"}}
		}
		if p, ok := proofs[branch]; ok && branch != "" {
			return p
		}
		p, err := c.commitStatus(sha, branch)
		if err != nil {
			p = proof{basis: Keep, reasons: []string{"检查出错，保守留着：" + err.Error()}}
		}
		if branch != "" {
			proofs[branch] = p
		}
		return p
	}

	checkedOut := map[string]Item{} // 分支名 → 检出它的附加 worktree
	mainBranch := wts[0].Branch
	for _, w := range wts[1:] {
		it := c.worktreeItem(w, statusOf)
		rep.Worktrees = append(rep.Worktrees, it)
		if w.Branch != "" {
			checkedOut[w.Branch] = it
		}
	}
	for _, name := range c.branchOrder {
		if name == c.def {
			continue
		}
		b := branches[name]
		it := Item{Kind: KindBranch, Name: name, Branch: name, Head: b.sha, LastActive: b.when}
		p := statusOf(b.sha, name)
		wt, hasWT := checkedOut[name]
		switch {
		case name == mainBranch:
			it.Basis, it.Reasons = Keep, []string{"主工作区当前检出的分支"}
		case hasWT && !wt.Removable:
			it.Basis, it.Reasons = Keep, []string{"在留着的 worktree 里检出：" + relTo(root, wt.Name)}
			if p.basis == Keep {
				it.Reasons = append(it.Reasons, p.reasons...)
				it.Unmerged = p.unmerged
			}
		default:
			it.apply(p)
			if it.Removable && hasWT {
				it.Evidence += "；先删检出它的 worktree"
			}
		}
		rep.Branches = append(rep.Branches, it)
	}
	return rep, nil
}

type proof struct {
	basis     Basis
	evidence  string
	reasons   []string
	remote    string
	remoteRef string
	unmerged  bool // 有独有提交：不在默认分支、也没有远程引用包含
}

func (it *Item) apply(p proof) {
	it.Basis = p.basis
	it.Removable = p.basis == Merged || p.basis == Pushed
	it.Evidence = p.evidence
	it.Reasons = append(it.Reasons, p.reasons...)
	it.Remote, it.RemoteRef = p.remote, p.remoteRef
	it.Unmerged = p.unmerged
}

type branchRef struct {
	sha  string
	when time.Time
}

type checker struct {
	ctx         context.Context
	root        string
	def         string
	defTip      string
	remotes     string
	objEnv      []string
	branchOrder []string
	fpChain     []string                     // 默认分支第一父链，旧 → 新
	trees       map[string]string            // 提交 → 树
	subjects    map[string]string            // 提交 → 标题
	defPIDs     map[string]map[string]string // 合并基点 → (patch-id → 默认分支提交)
}

func (c *checker) git(args ...string) (string, error) { return gitx.Run(c.ctx, c.root, args...) }

func (c *checker) localBranches() (map[string]branchRef, error) {
	out, err := c.git("for-each-ref", "--format=%(refname)%00%(objectname)%00%(committerdate:unix)", "refs/heads")
	if err != nil {
		return nil, fmt.Errorf("列本地分支：%w", err)
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

// defaultBranch：origin/HEAD 指向的分支本地也有就用它，否则 main、master。
func defaultBranch(ctx context.Context, root string, branches map[string]branchRef) string {
	if ref, err := gitx.Run(ctx, root, "symbolic-ref", "-q", "refs/remotes/origin/HEAD"); err == nil {
		name := strings.TrimPrefix(ref, "refs/remotes/origin/")
		if _, ok := branches[name]; ok {
			return name
		}
	}
	for _, n := range []string{"main", "master"} {
		if _, ok := branches[n]; ok {
			return n
		}
	}
	return ""
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

// describe 返回「提交号「标题」」。
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
	return short(sha) + "「" + s + "」"
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

func (c *checker) firstParentChain() ([]string, error) {
	if c.fpChain == nil {
		out, err := c.git("rev-list", "--first-parent", "--reverse", "refs/heads/"+c.def)
		if err != nil {
			return nil, fmt.Errorf("列默认分支历史：%w", err)
		}
		c.fpChain = gitx.Lines(out)
	}
	return c.fpChain, nil
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
		return nil, nil, fmt.Errorf("算 patch-id：%w", err)
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
	log, err := c.git("log", "--no-merges", "--no-color", "--no-ext-diff", "-p", "--format=commit %H", "--max-count=2000", mb+"..refs/heads/"+c.def)
	if err != nil {
		return nil, fmt.Errorf("读默认分支改动：%w", err)
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
	def := c.def
	merged := func(ev string) (proof, bool, error) { return proof{basis: Merged, evidence: ev}, true, nil }
	if tip == c.defTip {
		return merged(fmt.Sprintf("和 %s 指向同一个提交 %s", def, short(tip)))
	}
	anc, err := c.isAncestor(tip, c.defTip)
	if err != nil {
		return proof{}, false, err
	}
	if anc {
		chain, err := c.firstParentChain()
		if err != nil {
			return proof{}, false, err
		}
		at, err := firstTrue(chain, func(x string) (bool, error) { return c.isAncestor(tip, x) })
		if err != nil {
			return proof{}, false, err
		}
		if at == tip {
			return merged(fmt.Sprintf("已在 %s 上：提交 %s", def, c.describe(at)))
		}
		return merged(fmt.Sprintf("已合并进 %s：提交 %s", def, c.describe(at)))
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
		return proof{}, false, fmt.Errorf("读分支改动：%w", err)
	}
	if _, order, err := c.patchIDs(diff + "\n"); err != nil {
		return proof{}, false, err
	} else if len(order) == 1 {
		if at, ok := defPIDs[order[0]]; ok {
			return merged(fmt.Sprintf("内容已 squash 进 %s：提交 %s", def, c.describe(at)))
		}
	}
	// rebase：分支上每个提交在默认分支上都有同 patch-id 的提交
	blog, err := c.git("log", "--no-merges", "--no-color", "--no-ext-diff", "-p", "--format=commit %H", mb+".."+tip)
	if err != nil {
		return proof{}, false, fmt.Errorf("读分支提交：%w", err)
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
			ev := fmt.Sprintf("提交都已 rebase 进 %s：%s", def, c.describe(hits[0]))
			if len(hits) > 1 {
				ev += fmt.Sprintf(" 等 %d 个", len(hits))
			}
			return merged(ev)
		}
	}
	// 模拟合并不增加内容：覆盖 squash 后默认分支又改过、分几次合并等情况
	ok, err := c.mergeAddsNothing(c.defTip, tip)
	if err != nil || !ok {
		return proof{}, false, err
	}
	out, err := c.git("rev-list", "--first-parent", "--reverse", mb+"..refs/heads/"+def)
	if err != nil {
		return proof{}, false, err
	}
	chain := gitx.Lines(out)
	if len(chain) == 0 {
		return merged(fmt.Sprintf("相对 %s 没有净改动", def))
	}
	at, err := firstTrue(chain, func(x string) (bool, error) { return c.mergeAddsNothing(x, tip) })
	if err != nil {
		return proof{}, false, err
	}
	return merged(fmt.Sprintf("内容已全部在 %s 里：从提交 %s 起，合并它不再带来改动", def, c.describe(at)))
}

// pushedProof：某个远程跟踪引用包含 tip。优先和分支同名的引用。
func (c *checker) pushedProof(tip, branch string) (proof, bool, error) {
	out, err := c.git("for-each-ref", "--contains", tip, "--format=%(refname) %(objectname)", "refs/remotes")
	if err != nil {
		return proof{}, false, fmt.Errorf("查远程跟踪引用：%w", err)
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
	ev := fmt.Sprintf("已推到 %s/%s（本地记录的远程提交 %s 包含它）", remote, name, short(bestSHA))
	return proof{basis: Pushed, evidence: ev, remote: remote, remoteRef: "refs/heads/" + name}, true, nil
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
	not := []string{tip, "--not", "refs/heads/" + c.def, "--remotes"}
	n, err := c.git(append([]string{"rev-list", "--count"}, not...)...)
	if err != nil {
		return proof{}, fmt.Errorf("数独有提交：%w", err)
	}
	if n == "0" {
		return proof{basis: Keep, unmerged: true, reasons: []string{fmt.Sprintf("内容不在 %s 里，也没有哪个远程引用完整包含它", c.def)}}, nil
	}
	out, err := c.git(append([]string{"log", "--format=%H", "--max-count=3"}, not...)...)
	if err != nil {
		return proof{}, err
	}
	var ds []string
	for _, sha := range gitx.Lines(out) {
		ds = append(ds, c.describe(sha))
	}
	r := fmt.Sprintf("有 %s 个提交既不在 %s 也没推到任何远程：%s", n, c.def, strings.Join(ds, "；"))
	if nn, _ := strconv.Atoi(n); nn > len(ds) {
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
