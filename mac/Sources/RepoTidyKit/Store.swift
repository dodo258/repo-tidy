import Foundation
import Observation

/// 界面拿数据的地方。真实实现是 `CLI`；测试和截图用假的。
public protocol Backend: Sendable {
    func scan(root: String, depth: Int, online: Bool, progress: @escaping @Sendable (ScanProgress) -> Void) async throws -> ScanResult
    func apply(_ items: [Item]) async throws -> ApplyOutcome
    func undo(log: String) async throws -> UndoOutcome
}

extension CLI: Backend {}

/// 主窗口和菜单栏共用的状态。判定全来自命令行，这里只管扫描、勾选、调用删除。
@MainActor @Observable
public final class Store {
    /// 界面要画哪一种：没装命令行、第一次扫描中、出错、没有仓库、全部干净、有可删项。
    public enum Screen: Equatable {
        case missingCLI, scanning, failed(String), empty, allClean, hasRemovable
    }

    public private(set) var report: Report?
    /// 扫描的文件夹下一个仓库都没有。
    public private(set) var noRepos = false
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

    private let defaults: UserDefaults
    private let makeBackend: @MainActor (Store) -> Backend?
    private var scanTask: Task<Void, Never>?

    enum Keys {
        static let root = "scanRoot"
        static let cli = "cliPath"
        static let depth = "scanDepth"
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
    }

    // MARK: 读

    public var screen: Screen {
        if missingCLI { return .missingCLI }
        if let error { return .failed(error) }
        if noRepos { return .empty }
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
        // 命令行在后台线程报进度，回主线程再改界面状态；扫完之后迟到的不要
        let onProgress: @Sendable (ScanProgress) -> Void = { [weak self] p in
            Task { @MainActor in
                if let self, self.scanning { self.progress = p }
            }
        }
        scanTask = Task { [weak self] in
            let result: Result<ScanResult, Error>
            do {
                result = .success(try await backend.scan(root: root, depth: depth, online: false, progress: onProgress))
            } catch {
                result = .failure(error)
            }
            self?.finishScan(result)
        }
    }

    private func finishScan(_ result: Result<ScanResult, Error>) {
        scanning = false
        progress = nil
        lastScan = Date()
        switch result {
        case let .success(.report(r)):
            error = nil
            noRepos = false
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
            noRepos = true
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

    // MARK: 删除

    public func applyChecked() async {
        let items = checkedItems
        guard !items.isEmpty, !applying, let backend = makeBackend(self) else { return }
        applying = true
        applyError = nil
        do {
            outcome = try await backend.apply(items)
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
