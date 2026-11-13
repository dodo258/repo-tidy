import Foundation
import Testing
@testable import RepoTidyKit

/// 记下删除请求的假后端。
final class FakeBackend: Backend, @unchecked Sendable {
    var result: Result<ScanResult, Error>
    var applied: [[Item]] = []
    var undone: [String] = []

    init(_ result: Result<ScanResult, Error>) { self.result = result }

    var progress: [ScanProgress] = []
    var depths: [Int] = []
    /// 报完进度后等多久再返回结果，给测试看扫描中的状态。
    var delay: Duration = .zero

    func scan(root: String, depth: Int, online: Bool, progress report: @escaping @Sendable (ScanProgress) -> Void) async throws -> ScanResult {
        depths.append(depth)
        for p in progress { report(p) }
        if delay > .zero { try await Task.sleep(for: delay) }
        return try result.get()
    }

    func apply(_ items: [Item]) async throws -> ApplyOutcome {
        applied.append(items)
        return ApplyOutcome(log: "/s/log", target: items.count, deleted: items.count, results: [], errors: [])
    }

    func undo(log: String) async throws -> UndoOutcome {
        undone.append(log)
        return UndoOutcome(log: log, restored: 1, results: [])
    }
}

@MainActor
func makeStore(_ backend: Backend?) -> Store {
    let suite = "repo-tidy-test-\(UUID().uuidString)"
    return Store(defaults: UserDefaults(suiteName: suite) ?? .standard, makeBackend: { _ in backend })
}

@MainActor
func settle(_ s: Store) async {
    for _ in 0..<200 where s.scanning || s.applying { await Task.yield(); try? await Task.sleep(for: .milliseconds(5)) }
}

@MainActor @Test func screensFollowScanResult() async throws {
    let missing = makeStore(nil)
    missing.refresh()
    #expect(missing.screen == .missingCLI)

    let fake = FakeBackend(.success(.noRepos))
    let s = makeStore(fake)
    #expect(s.screen == .scanning)
    s.refresh()
    #expect(s.scanning)
    await settle(s)
    #expect(s.screen == .empty)

    fake.result = .failure(CLIError.failed(code: 2, message: "路径 /x：不存在"))
    s.refresh()
    await settle(s)
    #expect(s.screen == .failed("路径 /x：不存在"))

    fake.result = .success(.report(try decodeSample()))
    s.refresh()
    await settle(s)
    #expect(s.screen == .hasRemovable)
    #expect(s.selectedRepoID == "/p/app")
    #expect(s.removableTotal == 2)
}

@MainActor @Test func allCleanSelectsNothing() async throws {
    var r = try decodeSample()
    r.repos = [r.repos[2]]
    r.summary.removableWorktrees = 0
    r.summary.removableBranches = 0
    let s = makeStore(FakeBackend(.success(.report(r))))
    s.refresh()
    await settle(s)
    #expect(s.screen == .allClean && s.selectedRepoID == nil)
}

@MainActor @Test func onlyRemovableCanBeChecked() async throws {
    let s = makeStore(FakeBackend(.success(.report(try decodeSample()))))
    s.refresh()
    await settle(s)
    let app = try #require(s.selectedRepo)
    let keepBranch = app.branches[1], keepWT = app.worktrees[1]
    s.toggle(keepBranch)
    s.toggle(keepWT)
    #expect(s.checked.isEmpty)

    // 勾分支，连同检出它的 worktree 一起勾
    s.toggle(app.branches[0])
    #expect(s.isChecked(app.branches[0]) && s.isChecked(app.worktrees[0]))
    // 取消 worktree，它检出的分支一起取消
    s.toggle(app.worktrees[0])
    #expect(s.checked.isEmpty)

    s.checkAll(in: app)
    #expect(s.checkedItems.map(\.name) == ["/p/app/.claude/worktrees/x", "agent/x"])
    s.uncheckAll(in: app)
    #expect(s.checked.isEmpty)
}

@MainActor @Test func applySendsOnlyCheckedAndRescans() async throws {
    let fake = FakeBackend(.success(.report(try decodeSample())))
    let s = makeStore(fake)
    s.refresh()
    await settle(s)
    let app = try #require(s.selectedRepo)
    s.toggle(app.worktrees[0])
    await s.applyChecked()
    await settle(s)
    #expect(fake.applied.count == 1 && fake.applied[0].map(\.name) == ["/p/app/.claude/worktrees/x"])
    #expect(s.outcome?.deleted == 1 && s.checked.isEmpty)
}

@MainActor @Test func undoUsesTheOutcomeLogOnceAndRescans() async throws {
    let fake = FakeBackend(.success(.report(try decodeSample())))
    let s = makeStore(fake)
    #expect(!s.canUndo)
    await s.undoLastApply()
    #expect(fake.undone.isEmpty) // 还没删过东西
    s.refresh()
    await settle(s)
    let app = try #require(s.selectedRepo)
    s.toggle(app.worktrees[0])
    await s.applyChecked()
    await settle(s)
    #expect(s.canUndo)
    await s.undoLastApply()
    await settle(s)
    #expect(fake.undone == ["/s/log"] && s.undoOutcome?.restored == 1 && !s.canUndo)
    await s.undoLastApply() // 撤销过了，不再撤销
    #expect(fake.undone.count == 1)
    s.outcome = nil // 关掉弹窗，撤销结果一起清掉
    #expect(s.undoOutcome == nil)
}

@MainActor @Test func rescanDropsChecksThatAreNoLongerRemovable() async throws {
    let fake = FakeBackend(.success(.report(try decodeSample())))
    let s = makeStore(fake)
    s.refresh()
    await settle(s)
    let app = try #require(s.selectedRepo)
    s.checkAll(in: app)
    var changed = try decodeSample()
    changed.repos[0].branches[0].verdict = "keep"
    fake.result = .success(.report(changed))
    s.refresh()
    await settle(s)
    #expect(s.checkedItems.map(\.name) == ["/p/app/.claude/worktrees/x"])
}

@MainActor @Test func progressShownWhileScanning() async throws {
    Lang.current = .zh
    let fake = FakeBackend(.success(.report(try decodeSample())))
    let p = ScanProgress(current: 3, total: 13, repo: "repo-name")
    fake.progress = [p]
    fake.delay = .milliseconds(300)
    let s = makeStore(fake)
    s.refresh()
    for _ in 0..<100 where s.progress == nil { try await Task.sleep(for: .milliseconds(5)) }
    #expect(s.scanning && s.progress == p)
    #expect(p.text == "正在扫 3/13 repo-name")
    await settle(s)
    for _ in 0..<100 where s.scanning { try await Task.sleep(for: .milliseconds(10)) }
    #expect(!s.scanning && s.progress == nil)
}

@MainActor @Test func scanDepthIsSavedAndPassedToCLI() async throws {
    let defaults = try #require(UserDefaults(suiteName: "repo-tidy-test-\(UUID().uuidString)"))
    let fake = FakeBackend(.success(.noRepos))
    let s = Store(defaults: defaults, makeBackend: { _ in fake })
    #expect(s.scanDepth == 1)
    s.refresh()
    await settle(s)
    s.scanDepth = 3
    s.refresh()
    await settle(s)
    #expect(fake.depths == [1, 3])
    #expect(Store(defaults: defaults, makeBackend: { _ in nil }).scanDepth == 3)
    defaults.set(9, forKey: "scanDepth") // 超出范围的旧值回到默认
    #expect(Store(defaults: defaults, makeBackend: { _ in nil }).scanDepth == 1)
}
