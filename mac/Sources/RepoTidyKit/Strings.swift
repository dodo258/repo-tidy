import Foundation

/// 界面语言：系统首选语言以 zh 开头用中文，其余用英文。启动时定下，测试和截图模式可以改。
public enum Lang: String, Sendable, CaseIterable {
    case zh, en

    public static func detect(_ preferred: [String] = Locale.preferredLanguages) -> Lang {
        preferred.first?.lowercased().hasPrefix("zh") == true ? .zh : .en
    }

    nonisolated(unsafe) public static var current = detect()

    public var strings: Strings { self == .zh ? .zh : .en }
}

/// 当前语言的文案。
public var L: Strings { Lang.current.strings }

/// App 里所有给人看的文字，中英各一套。新加一项两套都要填（编译器会逼着填），
/// 测试会遍历每一项：都非空、英文里没有中日韩字符。
/// 带参数的项用 `Fmt1`…`Fmt4` 包住闭包（像函数一样调用），参数类型只用测试认得的几种（见 StringsTests）。
/// 不直接存闭包：测试用 Mirror 把 Any 转回函数类型时运行时会崩（signal 11），转结构体没问题。
public struct Strings: Sendable {
    // MARK: 菜单、设置
    public let menuChooseFolder: String
    public let rescan: String
    public let rescanHelp: String
    public let menuCleanChecked: String
    public let settingsFolder: String
    public let choose: String
    public let settingsCLI: String
    public let cliPlaceholder: String
    public let cliHint: String
    public let settingsDepth: String
    public let depthHint: String

    // MARK: 主窗口
    public let applyFailedTitle: String
    public let ok: String
    public let folderHelp: Fmt1<String>
    public let folderPanelPrompt: String
    public let folderPanelMessage: String

    // MARK: 侧栏
    public let groupMessy: String
    public let groupFailed: String
    public let groupClean: String
    public let overviewCounts: Fmt3<Int, Int, Int>
    public let nothingRemovable: String
    public let scannedAtReadOnly: Fmt1<String>
    public let repoScanFailed: String
    public let removableCount: Fmt1<Int>
    public let keepCount: Fmt1<Int>
    public let unmergedHelp: Fmt1<Int>
    /// 「可删 5 项」；能腾出空间时另用 `removableFreeing`。
    public let removableLine: Fmt1<Int>
    public let removableFreeing: Fmt2<Int, String>

    // MARK: 仓库详情
    public let repoFailedNotice: Fmt1<String>
    public let showInFinder: String
    public let defaultBranch: Fmt1<String>
    public let sectionWorktrees: String
    public let noWorktrees: String
    public let sectionBranches: String
    public let noBranches: String
    public let sectionCounts: Fmt2<Int, Int>
    public let uncheckRepo: String
    public let checkRepo: Fmt1<Int>
    public let cleanUp: String
    public let cleanUpCount: Fmt1<Int>
    public let hintCheck: String
    public let noSafeItems: String
    public let checkedCount: Fmt1<Int>
    public let checkedAcross: Fmt2<Int, Int>
    public let checkHelpRemovable: String
    public let checkHelpKeep: String
    public let detachedHead: String
    public let lastActiveHelpWorktree: String
    public let lastActiveHelpBranch: String
    public let verdictRemovable: String
    public let verdictKeep: String

    // MARK: 体检一行
    public let noBackup: String
    public let backupUpToDate: String
    public let backupStale: Fmt1<Int>
    public let uncommitted: Fmt1<Int>
    public let stashes: Fmt1<Int>
    public let aheadBehind: Fmt2<Int, Int>
    public let lastCommit: Fmt1<String>
    public let lastCommitOn: Fmt2<String, String>

    // MARK: 确认和结果弹窗
    public let confirmTitle: Fmt1<Int>
    public let confirmBody: String
    public let cancel: String
    public let deleteCount: Fmt1<Int>
    public let kindWorktree: String
    public let kindBranch: String
    public let checkedOutAbove: String
    public let deletedCount: Fmt1<Int>
    public let deletedSkipped: Fmt2<Int, Int>
    public let resultDone: String
    public let resultSkipped: String
    public let repoError: Fmt2<String, String>
    public let restoreLog: Fmt1<String>
    public let undoCleanup: String
    public let undoHelp: String
    public let restoredCount: Fmt1<Int>
    public let restoredSkipped: Fmt2<Int, Int>
    public let undoRestored: String
    public let undoExists: String
    public let undoLost: String
    public let undoFailed: String
    public let undoError: Fmt1<String>

    // MARK: 整窗状态
    public let scanningTitle: String
    public let scanningDetail: String
    /// 「正在扫 3/13」，后面接仓库名。
    public let progress: Fmt2<Int, Int>
    public let failedTitle: String
    public let failedDetail: String
    public let chooseAnotherFolder: String
    public let emptyTitle: String
    public let emptyDetail: String
    public let allCleanTitle: String
    public let allCleanHint: String
    public let pickRepoTitle: String
    public let pickRepoDetail: String
    public let cleanNoItems: Fmt1<Int>
    /// 仓库数、worktree 数、分支数、其中有没合并内容的项数。
    public let cleanAllKept: Fmt4<Int, Int, Int, Int>
    public let missingTitle: String
    public let missingDetail: String
    public let missingBuildHint: String
    public let copy: String
    public let missingSearched: String
    public let searchAgain: String
    public let chooseCLIFile: String
    public let cliPanelPrompt: String
    public let cliPanelMessage: String

    // MARK: 菜单栏
    public let a11yMissing: String
    public let a11yRemovable: Fmt1<Int>
    public let openMainWindow: String
    public let refresh: String
    public let quit: String
    public let scanningEllipsis: String
    public let scannedAt: Fmt1<String>
    public let panelMissing: String
    public let panelScanning: Fmt1<String>
    public let panelFailed: Fmt1<String>
    public let panelEmpty: Fmt1<String>
    /// 仓库数、留着的项数、其中有没合并内容的项数。
    public let panelKept: Fmt3<Int, Int, Int>

    // MARK: 时间
    public let unknownTime: String
    public let justNow: String
    public let minutesAgo: Fmt1<Int>
    public let hoursAgo: Fmt1<Int>
    public let daysAgo: Fmt1<Int>

    // MARK: 命令行出错
    public let cliExitNoMessage: Fmt1<Int>
    public let cliTimedOut: Fmt1<Int>
    public let cliBadOutput: Fmt1<String>
    public let cliLaunch: Fmt1<String>
    public let badTime: Fmt1<String>
}

public struct Fmt1<A>: Sendable {
    let make: @Sendable (A) -> String
    public init(_ make: @escaping @Sendable (A) -> String) { self.make = make }
    public func callAsFunction(_ a: A) -> String { make(a) }
}

public struct Fmt2<A, B>: Sendable {
    let make: @Sendable (A, B) -> String
    public init(_ make: @escaping @Sendable (A, B) -> String) { self.make = make }
    public func callAsFunction(_ a: A, _ b: B) -> String { make(a, b) }
}

public struct Fmt3<A, B, C>: Sendable {
    let make: @Sendable (A, B, C) -> String
    public init(_ make: @escaping @Sendable (A, B, C) -> String) { self.make = make }
    public func callAsFunction(_ a: A, _ b: B, _ c: C) -> String { make(a, b, c) }
}

public struct Fmt4<A, B, C, D>: Sendable {
    let make: @Sendable (A, B, C, D) -> String
    public init(_ make: @escaping @Sendable (A, B, C, D) -> String) { self.make = make }
    public func callAsFunction(_ a: A, _ b: B, _ c: C, _ d: D) -> String { make(a, b, c, d) }
}

/// 英文计数：「1 item」「3 items」。
private func count(_ n: Int, _ one: String, _ many: String? = nil) -> String {
    "\(n) \(n == 1 ? one : many ?? one + "s")"
}

extension Strings {
    public static let zh = Strings(
        menuChooseFolder: "选择要扫描的文件夹…",
        rescan: "重新扫描",
        rescanHelp: "重新扫描（⌘R）",
        menuCleanChecked: "清理勾选的项…",
        settingsFolder: "扫描的文件夹",
        choose: "选择…",
        settingsCLI: "命令行工具",
        cliPlaceholder: "用 App 自带的",
        cliHint: "留空时用 App 自带的；想用自己编的版本再填路径",
        settingsDepth: "往下找几层",
        depthHint: "在扫描的文件夹里往下找几层仓库。仓库放在 ~/code/组织/仓库 这样的就选 2。找到仓库就不再进它里面",

        applyFailedTitle: "没删成",
        ok: "好",
        folderHelp: .init { "扫描的文件夹：\($0)。点一下换一个" },
        folderPanelPrompt: "扫描这个文件夹",
        folderPanelMessage: "选放仓库的文件夹（往下找几层在设置里改），或者某一个仓库",

        groupMessy: "要整理",
        groupFailed: "没扫成",
        groupClean: "干净",
        overviewCounts: .init { "\($0) 个仓库 · \($1) 个 worktree · \($2) 个分支" },
        nothingRemovable: "没有可删的项",
        scannedAtReadOnly: .init { "\($0) 扫描（扫描只读，不改任何文件）" },
        repoScanFailed: "没扫成",
        removableCount: .init { "可删 \($0)" },
        keepCount: .init { "留着 \($0)" },
        unmergedHelp: .init { "留着的里面有 \($0) 项有没合并也没推送的内容" },
        removableLine: .init { "可删 \($0) 项" },
        removableFreeing: .init { "可删 \($0) 项，能腾出 \($1)" },

        repoFailedNotice: .init { "这个仓库没扫成：\($0)" },
        showInFinder: "在 Finder 中显示",
        defaultBranch: .init { "默认分支 \($0)" },
        sectionWorktrees: "Worktree",
        noWorktrees: "没有附加 worktree",
        sectionBranches: "本地分支",
        noBranches: "除默认分支外没有本地分支",
        sectionCounts: .init { "可删 \($0) · 留着 \($1)" },
        uncheckRepo: "取消本仓库的勾选",
        checkRepo: .init { "勾选本仓库可删的 \($0) 项" },
        cleanUp: "清理…",
        cleanUpCount: .init { "清理 \($0) 项…" },
        hintCheck: "勾选要删的项，再点「清理」",
        noSafeItems: "这个仓库没有能证明安全的可删项",
        checkedCount: .init { "已勾选 \($0) 项" },
        checkedAcross: .init { "已勾选 \($0) 项（本仓库 \($1) 项，其余在别的仓库）" },
        checkHelpRemovable: "勾上，点「清理」时删掉",
        checkHelpKeep: "要留着，不能勾",
        detachedHead: "分离头指针",
        lastActiveHelpWorktree: "最后活动：HEAD 提交、index、reflog 里最新的时间",
        lastActiveHelpBranch: "分支末端提交的时间",
        verdictRemovable: "可删",
        verdictKeep: "留着",

        noBackup: "没有 backup 远程",
        backupUpToDate: "backup 已推到最新",
        backupStale: .init { "backup 有 \($0) 个分支没推到最新" },
        uncommitted: .init { "主工作区 \($0) 处未提交改动" },
        stashes: .init { "stash \($0) 个" },
        aheadBehind: .init { "比上游领先 \($0)、落后 \($1)" },
        lastCommit: .init { "最后提交 \($0)" },
        lastCommitOn: .init { "最后提交 \($0)（\($1)）" },

        confirmTitle: .init { "删除这 \($0) 项？" },
        confirmBody: "删之前 repo-tidy 会逐项再判定一次，和现在不一样的不删；靠「已推到远程」的会先联网确认远程还在。worktree 用 git worktree remove（不加 --force），恢复命令写进日志。",
        cancel: "取消",
        deleteCount: .init { "删除 \($0) 项" },
        kindWorktree: "worktree",
        kindBranch: "分支",
        checkedOutAbove: "检出在上面那个 worktree 里，worktree 删掉后才删得了它",
        deletedCount: .init { "删了 \($0) 项" },
        deletedSkipped: .init { "删了 \($0) 项，\($1) 项没删" },
        resultDone: "已删",
        resultSkipped: "没删",
        repoError: .init { "\($0)：\($1)" },
        restoreLog: .init { "恢复命令在 \($0)" },
        undoCleanup: "撤销这次清理",
        undoHelp: "按恢复日志把删掉的分支和 worktree 建回原处；已经在的跳过",
        restoredCount: .init { "恢复了 \($0) 项" },
        restoredSkipped: .init { "恢复了 \($0) 项，\($1) 项没恢复" },
        undoRestored: "已恢复",
        undoExists: "跳过",
        undoLost: "恢复不了",
        undoFailed: "失败",
        undoError: .init { "撤销没做成：\($0)" },

        scanningTitle: "正在扫描",
        scanningDetail: "每个仓库要跑几十次 git 来比对提交，还要量每个 worktree 占多大。只读：不 fetch、不改任何文件。",
        progress: .init { "正在扫 \($0)/\($1)" },
        failedTitle: "扫描没完成",
        failedDetail: "repo-tidy 报了下面的错，什么都没删。",
        chooseAnotherFolder: "换个文件夹…",
        emptyTitle: "这个文件夹下没有 git 仓库",
        emptyDetail: "repo-tidy 扫你选的文件夹本身和它下面一层的文件夹。换成放仓库的那一层，比如 ~/projects。",
        allCleanTitle: "没有能删的",
        allCleanHint: "左边选一个仓库，能看到每一项为什么留着。",
        pickRepoTitle: "选一个仓库",
        pickRepoDetail: "左边的仓库按乱的程度排，最乱的在最上面。",
        cleanNoItems: .init { "扫了 \($0) 个仓库，除了默认分支，没有附加 worktree，也没有别的本地分支。" },
        cleanAllKept: .init { repos, wts, branches, unmerged in
            "扫了 \(repos) 个仓库，\(wts) 个 worktree、\(branches) 个分支都得留着"
                + (unmerged > 0 ? "，其中 \(unmerged) 项有没合并也没推送的内容" : "") + "。"
        },
        missingTitle: "没找到 repo-tidy 命令行工具",
        missingDetail: "判断哪些能删、真正去删，都由命令行工具 repo-tidy 来做。它本来装在 App 里面，现在不见了或不能执行。重新编一次 App 就好，或者自己编一个装到下面的位置。",
        missingBuildHint: "在 repo-tidy 源码目录里运行（要 Go 1.26 和 git 2.38 以上）：",
        copy: "拷贝",
        missingSearched: "找过：App 自带的、~/.local/bin、~/go/bin、/opt/homebrew/bin、/usr/local/bin、~/bin",
        searchAgain: "重新查找",
        chooseCLIFile: "装在别处，选择文件…",
        cliPanelPrompt: "用这个",
        cliPanelMessage: "选 repo-tidy 可执行文件",

        a11yMissing: "repo-tidy：没找到命令行工具",
        a11yRemovable: .init { "repo-tidy：可删 \($0) 项" },
        openMainWindow: "打开主窗口",
        refresh: "刷新",
        quit: "退出",
        scanningEllipsis: "扫描中…",
        scannedAt: .init { "\($0) 扫描" },
        panelMissing: "没找到 repo-tidy 命令行工具，打开主窗口看怎么办。",
        panelScanning: .init { "正在扫描 \($0)…" },
        panelFailed: .init { "扫描没完成：\($0)" },
        panelEmpty: .init { "\($0) 下没有 git 仓库" },
        panelKept: .init { repos, keep, unmerged in
            "\(repos) 个仓库 · 留着 \(keep) 项" + (unmerged > 0 ? "，其中 \(unmerged) 项有未合并内容" : "")
        },

        unknownTime: "不知道",
        justNow: "刚刚",
        minutesAgo: .init { "\($0) 分钟前" },
        hoursAgo: .init { "\($0) 小时前" },
        daysAgo: .init { "\($0) 天前" },

        cliExitNoMessage: .init { "repo-tidy 退出码 \($0)，没有说明" },
        cliTimedOut: .init { "repo-tidy 跑了 \($0) 秒还没结束，已停止" },
        cliBadOutput: .init { "看不懂 repo-tidy 的输出：\($0)" },
        cliLaunch: .init { "启动 repo-tidy 失败：\($0)" },
        badTime: .init { "时间格式不对：\($0)" }
    )

    public static let en = Strings(
        menuChooseFolder: "Choose Folder to Scan…",
        rescan: "Rescan",
        rescanHelp: "Rescan (⌘R)",
        menuCleanChecked: "Clean Up Checked Items…",
        settingsFolder: "Folder to scan",
        choose: "Choose…",
        settingsCLI: "Command-line tool",
        cliPlaceholder: "Use the built-in copy",
        cliHint: "Leave empty to use the copy inside the app. Enter a path to use your own build.",
        settingsDepth: "Search depth",
        depthHint: "How many folder levels to look down for repositories. If they live in ~/code/org/repo, choose 2. Repositories found are not searched further.",

        applyFailedTitle: "Couldn't Delete",
        ok: "OK",
        folderHelp: .init { "Scanning \($0). Click to choose another folder." },
        folderPanelPrompt: "Scan This Folder",
        folderPanelMessage: "Choose the folder that holds your repositories (how many levels down to search is in Settings), or a single repository.",

        groupMessy: "Needs Cleanup",
        groupFailed: "Scan Failed",
        groupClean: "Clean",
        overviewCounts: .init {
            "\(count($0, "repo")) · \(count($1, "worktree")) · \(count($2, "branch", "branches"))"
        },
        nothingRemovable: "Nothing to remove",
        scannedAtReadOnly: .init { "Scanned at \($0) (read-only, nothing changed)" },
        repoScanFailed: "Scan failed",
        removableCount: .init { "\($0) removable" },
        keepCount: .init { "\($0) kept" },
        unmergedHelp: .init { n in
            n == 1 ? "1 kept item has work that isn't merged or pushed"
                : "\(n) kept items have work that isn't merged or pushed"
        },
        removableLine: .init { "\($0) removable" },
        removableFreeing: .init { "\($0) removable, frees \($1)" },

        repoFailedNotice: .init { "Couldn't scan this repository: \($0)" },
        showInFinder: "Show in Finder",
        defaultBranch: .init { "default branch \($0)" },
        sectionWorktrees: "Worktrees",
        noWorktrees: "No linked worktrees",
        sectionBranches: "Local Branches",
        noBranches: "No local branches other than the default",
        sectionCounts: .init { "\($0) removable · \($1) kept" },
        uncheckRepo: "Uncheck All",
        checkRepo: .init { "Check \($0) Removable" },
        cleanUp: "Clean Up…",
        cleanUpCount: .init { "Clean Up \(count($0, "Item"))…" },
        hintCheck: "Check the items to remove, then click Clean Up.",
        noSafeItems: "Nothing here is provably safe to remove.",
        checkedCount: .init { "\(count($0, "item")) checked" },
        checkedAcross: .init { "\(count($0, "item")) checked (\($1) here, the rest in other repos)" },
        checkHelpRemovable: "Check to remove it when you click Clean Up",
        checkHelpKeep: "Kept, so it can't be checked",
        detachedHead: "detached HEAD",
        lastActiveHelpWorktree: "Last activity: the latest of the HEAD commit, index, and reflog",
        lastActiveHelpBranch: "Time of the branch's latest commit",
        verdictRemovable: "Removable",
        verdictKeep: "Keep",

        noBackup: "No backup remote",
        backupUpToDate: "backup is up to date",
        backupStale: .init { "\(count($0, "branch", "branches")) not pushed to backup" },
        uncommitted: .init { "\(count($0, "uncommitted change")) in the main worktree" },
        stashes: .init { count($0, "stash", "stashes") },
        aheadBehind: .init { "\($0) ahead, \($1) behind upstream" },
        lastCommit: .init { "last commit \($0)" },
        lastCommitOn: .init { "last commit \($0) (\($1))" },

        confirmTitle: .init { "Delete \(count($0, "Item"))?" },
        confirmBody: "Before deleting, repo-tidy checks each item again and skips anything that has changed. Items that are safe because they were pushed get their remote checked online first. Worktrees are removed with git worktree remove (no --force), and restore commands are written to a log.",
        cancel: "Cancel",
        deleteCount: .init { "Delete \(count($0, "Item"))" },
        kindWorktree: "worktree",
        kindBranch: "branch",
        checkedOutAbove: "Checked out in the worktree above; it's deleted after that worktree is removed",
        deletedCount: .init { "Deleted \(count($0, "item"))" },
        deletedSkipped: .init { "Deleted \(count($0, "item")), skipped \($1)" },
        resultDone: "Deleted",
        resultSkipped: "Skipped",
        repoError: .init { "\($0): \($1)" },
        restoreLog: .init { "Restore commands: \($0)" },
        undoCleanup: "Undo This Cleanup",
        undoHelp: "Recreate the deleted branches and worktrees where they were, from the restore log; anything already there is skipped",
        restoredCount: .init { "Restored \(count($0, "item"))" },
        restoredSkipped: .init { "Restored \(count($0, "item")), \($1) not restored" },
        undoRestored: "Restored",
        undoExists: "Skipped",
        undoLost: "Can't restore",
        undoFailed: "Failed",
        undoError: .init { "Undo didn't run: \($0)" },

        scanningTitle: "Scanning",
        scanningDetail: "Each repository takes dozens of git commands to compare commits, plus measuring how much space each worktree uses. Read-only: no fetch, no changes to any file.",
        progress: .init { "Scanning \($0)/\($1)" },
        failedTitle: "Scan Didn't Finish",
        failedDetail: "repo-tidy reported the error below. Nothing was deleted.",
        chooseAnotherFolder: "Choose Another Folder…",
        emptyTitle: "No Git Repositories in This Folder",
        emptyDetail: "repo-tidy scans the folder you choose and the folders one level below it. Choose the folder that holds your repositories, such as ~/projects.",
        allCleanTitle: "Nothing to Remove",
        allCleanHint: "Select a repository on the left to see why each item is kept.",
        pickRepoTitle: "Select a Repository",
        pickRepoDetail: "Repositories on the left are sorted by how much cleanup they need, messiest first.",
        cleanNoItems: .init {
            "Scanned \(count($0, "repository", "repositories")). Apart from default branches, there are no linked worktrees and no other local branches."
        },
        cleanAllKept: .init { repos, wts, branches, unmerged in
            "Scanned \(count(repos, "repository", "repositories")). \(count(wts, "worktree")) and \(count(branches, "branch", "branches")) need to stay"
                + (unmerged > 0 ? "; \(unmerged) of them \(unmerged == 1 ? "has" : "have") work that isn't merged or pushed" : "") + "."
        },
        missingTitle: "repo-tidy Command-Line Tool Not Found",
        missingDetail: "The repo-tidy command-line tool decides what can be removed and does the deleting. It ships inside the app, but it's missing or can't be run. Rebuild the app, or build the tool yourself and install it in one of the places below.",
        missingBuildHint: "Run this in the repo-tidy source folder (needs Go 1.26+ and git 2.38+):",
        copy: "Copy",
        missingSearched: "Searched: inside the app, ~/.local/bin, ~/go/bin, /opt/homebrew/bin, /usr/local/bin, ~/bin",
        searchAgain: "Search Again",
        chooseCLIFile: "Choose File…",
        cliPanelPrompt: "Use This",
        cliPanelMessage: "Choose the repo-tidy executable",

        a11yMissing: "repo-tidy: command-line tool not found",
        a11yRemovable: .init { "repo-tidy: \($0) removable" },
        openMainWindow: "Open Main Window",
        refresh: "Refresh",
        quit: "Quit",
        scanningEllipsis: "Scanning…",
        scannedAt: .init { "Scanned \($0)" },
        panelMissing: "repo-tidy command-line tool not found. Open the main window to fix it.",
        panelScanning: .init { "Scanning \($0)…" },
        panelFailed: .init { "Scan didn't finish: \($0)" },
        panelEmpty: .init { "No git repositories in \($0)" },
        panelKept: .init { repos, keep, unmerged in
            "\(count(repos, "repo")) · \(keep) kept" + (unmerged > 0 ? ", \(unmerged) with unmerged work" : "")
        },

        unknownTime: "unknown",
        justNow: "just now",
        minutesAgo: .init { "\(count($0, "minute")) ago" },
        hoursAgo: .init { "\(count($0, "hour")) ago" },
        daysAgo: .init { "\(count($0, "day")) ago" },

        cliExitNoMessage: .init { "repo-tidy exited with code \($0) and no message" },
        cliTimedOut: .init { "repo-tidy was still running after \($0) seconds and was stopped" },
        cliBadOutput: .init { "Couldn't read repo-tidy's output: \($0)" },
        cliLaunch: .init { "Couldn't start repo-tidy: \($0)" },
        badTime: .init { "Unexpected time format: \($0)" }
    )
}
