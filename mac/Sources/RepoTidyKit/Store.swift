import Foundation
import Observation

/// 界面拿数据的地方。真实实现是 `CLI`；测试和截图用假的。
public protocol Backend: Sendable {
    func scan(root: String, online: Bool) async throws -> ScanResult
    func apply(_ items: [Item]) async throws -> ApplyOutcome
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
    public private(set) var missingCLI = false
    public private(set) var lastScan: Date?
    public private(set) var checked: Set<String> = []
    public private(set) var applying = false
    public var selectedRepoID: String?
    /// 删除完的逐项结果，界面弹出来给人看；看完置空。
    public var outcome: ApplyOutcome?
    public var applyError: String?

    public var scanRoot: String {
        didSet { defaults.set(scanRoot, forKey: Keys.root) }
    }
    public var cliPath: String {
        didSet { defaults.set(cliPath, forKey: Keys.cli) }
    }

    private let defaults: UserDefaults
    private let makeBackend: @MainActor (Store) -> Backend?
    private var scanTask: Task<Void, Never>?

    enum Keys {
        static let root = "scanRoot"
        static let cli = "cliPath"
    }

    /// 默认扫 $REPO_TIDY_ROOT，没设就扫 ~/projects，和命令行一致。
    public static func defaultRoot(environment: [String: String] = ProcessInfo.processInfo.environment) -> String {
        if let r = environment["REPO_TIDY_ROOT"], !r.isEmpty { return r }
        return NSHomeDirectory() + "/projects"
    }

    public init(defaults: UserDefaults = .standard,
                makeBackend: @escaping @MainActor (Store) -> Backend? = { store in
                    CLI.locate(custom: store.cliPath).map(CLI.init(executable:))
                }) {
        self.defaults = defaults
        self.makeBackend = makeBackend
        scanRoot = defaults.string(forKey: Keys.root) ?? Store.defaultRoot()
        cliPath = defaults.string(forKey: Keys.cli) ?? ""
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
        let root = (scanRoot as NSString).expandingTildeInPath
        scanTask = Task { [weak self] in
            let result: Result<ScanResult, Error>
            do {
                result = .success(try await backend.scan(root: root, online: false))
            } catch {
                result = .failure(error)
            }
            self?.finishScan(result)
        }
    }

    private func finishScan(_ result: Result<ScanResult, Error>) {
        scanning = false
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
}
