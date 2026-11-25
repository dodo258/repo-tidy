import Foundation
import Observation

/// 界面拿数据的地方。真实实现是 `CLI`；测试和截图用假的。
public protocol Backend: Sendable {
    func scan(root: String, depth: Int, excludes: [String], online: Bool,
              progress: @escaping @Sendable (ScanProgress) -> Void) async throws -> ScanResult
    func apply(_ items: [Item], excludes: [String]) async throws -> ApplyOutcome
    func undo(log: String) async throws -> UndoOutcome
}

extension CLI: Backend {}

/// 主窗口和菜单栏共用的状态。判定全来自命令行，这里只管扫描、勾选、调用删除。
@MainActor @Observable
public final class Store {
    /// 界面要画哪一种：没装命令行、第一次扫描中、出错、没有仓库（那次往下找了几层）、全部干净、有可删项。
    public enum Screen: Equatable {
        case missingCLI, scanning, failed(String), empty(depth: Int), allClean, hasRemovable
    }

    public private(set) var report: Report?
    /// 扫描的文件夹下一个仓库都没找到时，那次扫描往下找了几层（不是设置里刚改、还没扫完的值）；找到了为空。
    private var emptyDepth: Int?
    public private(set) var error: String?
    public private(set) var scanning = false
    /// 扫描中扫到第几个仓库；命令行还没报时为空。只在 `scanning` 时有意义。
    public private(set) var progress: ScanProgress?
    public private(set) var missingCLI = false
    public private(set) var lastScan: Date?
    public private(set) var checked: Set<String> = []
    /// 正在删除或撤销：这时不扫描、不能再清理。
    public private(set) var applying = false
    public var selectedRepoID: String?
    /// 右栏的筛选。换仓库时保留，只影响显示，勾选和清理照旧按勾上的项。
    public var filter = ItemFilter()
    /// 右栏 worktree 的排序。所有仓库同一个，下次打开还是它。
    public var worktreeSort: WorktreeSort {
        didSet { defaults.set(worktreeSort.rawValue, forKey: Keys.worktreeSort) }
    }
    /// 删除完的逐项结果，界面弹出来给人看；看完置空（连同撤销的结果）。
    public var outcome: ApplyOutcome? {
        didSet {
            if outcome == nil {
                undoOutcome = nil
                undoError = nil
            }
        }
    }
    public var applyError: String?
    /// 打开目录没成的原因，界面弹出来给人看；看完置空。
    public var openError: String?
    /// 撤销这次清理的逐项结果，显示在删除结果的弹窗里。
    public private(set) var undoOutcome: UndoOutcome?
    public private(set) var undoError: String?

    public var scanRoot: String {
        didSet { defaults.set(scanRoot, forKey: Keys.root) }
    }
    public var cliPath: String {
        didSet { defaults.set(cliPath, forKey: Keys.cli) }
    }
    /// 给的是文件夹时往下找几层仓库，和命令行的 `--depth` 一样（1 到 4）。
    public var scanDepth: Int {
        didSet { defaults.set(scanDepth, forKey: Keys.depth) }
    }
    public static let depthRange = 1...4
    /// 扫描时跳过的路径，和命令行的 `--exclude` 一样：路径本身、它下面的仓库、有 worktree 在它下面的仓库都不扫不删。
    /// 删除时也带上，命令行删前再挡一次。
    public private(set) var excludePaths: [String] {
        didSet { defaults.set(excludePaths, forKey: Keys.excludes) }
    }

    private let defaults: UserDefaults
    private let makeBackend: @MainActor (Store) -> Backend?
    private var scanTask: Task<Void, Never>?
    /// 扫描中改了扫描的设置：这次的结果是按旧设置扫的，扫完马上再扫一次。
    private var rescanWhenDone = false

    enum Keys {
        static let root = "scanRoot"
        static let cli = "cliPath"
        static let depth = "scanDepth"
        static let excludes = "scanExcludes"
        static let worktreeSort = "worktreeSort"
    }

    /// 默认扫 $REPO_TIDY_ROOT，没设就扫 ~/projects，和命令行一致。
    public static func defaultRoot(environment: [String: String] = ProcessInfo.processInfo.environment) -> String {
        if let r = environment["REPO_TIDY_ROOT"], !r.isEmpty { return r }
        return NSHomeDirectory() + "/projects"
    }

    public init(defaults: UserDefaults = .standard,
                makeBackend: @escaping @MainActor (Store) -> Backend? = { store in
                    CLI.locate(custom: store.cliPath).map { CLI(executable: $0) }
                }) {
        self.defaults = defaults
        self.makeBackend = makeBackend
        scanRoot = defaults.string(forKey: Keys.root) ?? Store.defaultRoot()
        cliPath = defaults.string(forKey: Keys.cli) ?? ""
        let depth = defaults.integer(forKey: Keys.depth) // 没存过是 0
        scanDepth = Store.depthRange.contains(depth) ? depth : Store.depthRange.lowerBound
        worktreeSort = defaults.string(forKey: Keys.worktreeSort).flatMap(WorktreeSort.init) ?? .standard
        excludePaths = (defaults.stringArray(forKey: Keys.excludes) ?? []).filter { !$0.isEmpty }
    }

    /// 加几个排除路径（已有的、空的不重复加），有变化就重新扫描。
    public func addExcludes(_ paths: [String]) {
        var list = excludePaths
        for p in paths where !p.isEmpty && !list.contains(p) { list.append(p) }
        guard list != excludePaths else { return }
        excludePaths = list
        rescanForNewSettings()
    }

    public func removeExclude(_ path: String) {
        guard excludePaths.contains(path) else { return }
        excludePaths.removeAll { $0 == path }
        rescanForNewSettings()
    }

    /// 改了扫描的设置（文件夹、层数、排除路径）后重新扫描；正在扫就扫完马上再扫一次。
    public func rescanForNewSettings() {
        if scanning { rescanWhenDone = true } else { refresh() }
    }

    // MARK: 读

    public var screen: Screen {
        if missingCLI { return .missingCLI }
        if let error { return .failed(error) }
        if let emptyDepth { return .empty(depth: emptyDepth) }
        guard let report else { return .scanning }
        return report.summary.removable > 0 ? .hasRemovable : .allClean
    }

    public var repos: [Repo] { report?.repos ?? [] }

    public func repos(in group: Repo.Group) -> [Repo] { repos.filter { $0.group == group } }

    public var selectedRepo: Repo? { repos.first { $0.id == selectedRepoID } }

    public var removableTotal: Int { report?.summary.removable ?? 0 }

    public func isChecked(_ item: Item) -> Bool { checked.contains(item.id) }

    /// 勾选的项，按仓库、worktree 在前的顺序（和删除顺序一致）。
    public var checkedItems: [Item] { repos.flatMap(\.items).filter { checked.contains($0.id) } }

    public func checkedCount(in repo: Repo) -> Int { repo.items.filter { checked.contains($0.id) }.count }

    // MARK: 扫描

    public func refresh() {
        guard !scanning, !applying else { return }
        guard let backend = makeBackend(self) else {
            missingCLI = true
            return
        }
        missingCLI = false
        scanning = true
        progress = nil
        let root = (scanRoot as NSString).expandingTildeInPath
        let depth = scanDepth
        let excludes = excludePaths
        // 命令行在后台线程报进度，回主线程再改界面状态；扫完之后迟到的不要
        let onProgress: @Sendable (ScanProgress) -> Void = { [weak self] p in
            Task { @MainActor in
                if let self, self.scanning { self.progress = p }
            }
        }
        scanTask = Task { [weak self] in
            let result: Result<ScanResult, Error>
            do {
                result = .success(try await backend.scan(root: root, depth: depth, excludes: excludes, online: false, progress: onProgress))
            } catch {
                result = .failure(error)
            }
            self?.finishScan(result, depth: depth)
        }
    }

    private func finishScan(_ result: Result<ScanResult, Error>, depth: Int) {
        scanning = false
        defer {
            if rescanWhenDone {
                rescanWhenDone = false
                refresh()
            }
        }
        progress = nil
        lastScan = Date()
        switch result {
        case let .success(.report(r)):
            error = nil
            emptyDepth = nil
            report = r
            // 勾选只留下这次还可删、提交没变的项
            let still = Set(r.repos.flatMap(\.items).filter(\.isRemovable).map(\.id))
            checked = checked.intersection(still)
            // 默认选最乱的仓库；全部干净时不选，右边显示「没有能删的」
            if selectedRepo == nil {
                selectedRepoID = r.repos.first { $0.group == .messy }?.id
            }
        case .success(.noRepos):
            error = nil
            report = nil
            checked = []
            emptyDepth = depth
        case let .failure(e):
            if e is CancellationError { return }
            error = e.localizedDescription
        }
    }

    // MARK: 勾选

    /// 只有可删项能勾。勾分支时连同检出它的 worktree 一起勾（不然 git 删不掉这个分支）；
    /// 取消 worktree 时连它检出的分支一起取消。
    public func toggle(_ item: Item) {
        guard item.isRemovable, let repo = repos.first(where: { $0.root == item.repo }) else { return }
        if checked.contains(item.id) {
            checked.remove(item.id)
            if item.kind == .worktree, !item.branch.isEmpty,
               let b = repo.branches.first(where: { $0.name == item.branch }) {
                checked.remove(b.id)
            }
        } else {
            checked.insert(item.id)
            if item.kind == .branch,
               let wt = repo.worktrees.first(where: { $0.branch == item.name && $0.isRemovable }) {
                checked.insert(wt.id)
            }
        }
    }

    public func checkAll(in repo: Repo) {
        for it in repo.items where it.isRemovable { checked.insert(it.id) }
    }

    public func uncheckAll(in repo: Repo) {
        for it in repo.items { checked.remove(it.id) }
    }

    /// 勾选的分支里，是因为它的 worktree 被勾才能删的：确认弹窗里说一句。
    public func worktreeFor(_ branch: Item) -> Item? {
        guard branch.kind == .branch else { return nil }
        return repos.first { $0.root == branch.repo }?.worktrees.first { $0.branch == branch.name }
    }

    // MARK: 去看目录

    /// 留着的项在磁盘上的目录，给「在 Finder 中显示」「在终端打开」用：worktree 是它自己，
    /// 分支是检出它的 worktree 或主工作区。可删的项、没检出在任何地方的分支没有。
    public func folder(of item: Item) -> String? {
        if item.isRemovable { return nil }
        if item.kind == .worktree { return item.name }
        if let wt = worktreeFor(item) { return wt.name }
        return repos.first { $0.root == item.repo }?.health?.branch == item.name ? item.repo : nil
    }

    /// 打开前看一眼目录还在不在：扫完之后可能被删掉或移走。不在了就写明原因。
    public func canOpen(folder path: String) -> Bool {
        var isDirectory: ObjCBool = false
        if FileManager.default.fileExists(atPath: path, isDirectory: &isDirectory), isDirectory.boolValue { return true }
        openError = L.folderGone(Format.tildePath(path))
        return false
    }

    // MARK: 删除

    public func applyChecked() async {
        let items = checkedItems
        guard !items.isEmpty, !applying, let backend = makeBackend(self) else { return }
        applying = true
        applyError = nil
        do {
            outcome = try await backend.apply(items, excludes: excludePaths)
            checked = []
        } catch {
            applyError = error.localizedDescription
        }
        applying = false
        refresh()
    }

    /// 能撤销：刚删过东西、有恢复日志、还没撤销过。
    public var canUndo: Bool {
        guard let o = outcome else { return false }
        return o.deleted > 0 && !o.log.isEmpty && undoOutcome == nil
    }

    /// 撤销刚才那次清理：命令行按它的恢复日志把删掉的分支和 worktree 建回原处，然后重新扫描。
    public func undoLastApply() async {
        guard canUndo, let log = outcome?.log, !applying, let backend = makeBackend(self) else { return }
        applying = true
        undoError = nil
        do {
            undoOutcome = try await backend.undo(log: log)
        } catch {
            undoError = error.localizedDescription
        }
        applying = false
        refresh()
    }
}
