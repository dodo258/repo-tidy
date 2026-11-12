import Foundation
import Testing
@testable import RepoTidyKit

/// 记下删除请求的假后端。
final class FakeBackend: Backend, @unchecked Sendable {
    var result: Result<ScanResult, Error>
    var applied: [[Item]] = []

    init(_ result: Result<ScanResult, Error>) { self.result = result }

    func scan(root: String, online: Bool) async throws -> ScanResult { try result.get() }

    func apply(_ items: [Item]) async throws -> ApplyOutcome {
        applied.append(items)
        return ApplyOutcome(log: "/s/log", target: items.count, deleted: items.count, results: [], errors: [])
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
