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
    var excludes: [[String]] = []
    var appliedExcludes: [[String]] = []
    /// 报完进度后等多久再返回结果，给测试看扫描中的状态。
    var delay: Duration = .zero

    func scan(root: String, depth: Int, excludes: [String], online: Bool,
              progress report: @escaping @Sendable (ScanProgress) -> Void) async throws -> ScanResult {
        depths.append(depth)
        self.excludes.append(excludes)
        for p in progress { report(p) }
        if delay > .zero { try await Task.sleep(for: delay) }
        return try result.get()
    }

    func apply(_ items: [Item], excludes: [String]) async throws -> ApplyOutcome {
        applied.append(items)
        appliedExcludes.append(excludes)
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
    #expect(s.screen == .empty(depth: 1))

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

@MainActor @Test func excludePathsAreSavedAndPassedToScanAndApply() async throws {
    let defaults = try #require(UserDefaults(suiteName: "repo-tidy-test-\(UUID().uuidString)"))
    let fake = FakeBackend(.success(.report(try decodeSample())))
    let s = Store(defaults: defaults, makeBackend: { _ in fake })
    #expect(s.excludePaths.isEmpty)
    s.refresh()
    await settle(s)
    // 加了就重新扫描；重复的、空的不加，没有变化不重扫
    s.addExcludes(["/p/vendor", "", "/p/other clone", "/p/vendor"])
    await settle(s)
    #expect(s.excludePaths == ["/p/vendor", "/p/other clone"])
    s.addExcludes(["/p/vendor"])
    await settle(s)
    #expect(fake.excludes == [[], ["/p/vendor", "/p/other clone"]])
    // 删除也带上，命令行删前再挡一次
    let removable = s.repos.flatMap(\.items).filter(\.isRemovable)
    let item = try #require(removable.first)
    s.toggle(item)
    await s.applyChecked()
    await settle(s)
    #expect(fake.appliedExcludes == [["/p/vendor", "/p/other clone"]])
    // 存进设置，下次打开还在；移掉一个也重新扫描
    #expect(Store(defaults: defaults, makeBackend: { _ in nil }).excludePaths == ["/p/vendor", "/p/other clone"])
    s.removeExclude("/p/vendor")
    await settle(s)
    #expect(s.excludePaths == ["/p/other clone"])
    #expect(fake.excludes.last == ["/p/other clone"])
    #expect(Store(defaults: defaults, makeBackend: { _ in nil }).excludePaths == ["/p/other clone"])
}

@MainActor @Test func excludeChangedWhileScanningRescansWhenDone() async throws {
    let fake = FakeBackend(.success(.noRepos))
    fake.delay = .milliseconds(200)
    let s = makeStore(fake)
    s.refresh()
    #expect(s.scanning)
    s.addExcludes(["/p/vendor"]) // 正在扫：这次按旧设置，扫完再扫一次
    for _ in 0..<200 where fake.excludes.count < 2 || s.scanning { try await Task.sleep(for: .milliseconds(10)) }
    #expect(fake.excludes == [[], ["/p/vendor"]])
    #expect(!s.scanning)
}

@MainActor @Test func emptyScreenSaysDepthOfTheFinishedScan() async throws {
    let fake = FakeBackend(.success(.noRepos))
    fake.delay = .milliseconds(500)
    let s = makeStore(fake)
    s.scanDepth = 2
    s.refresh()
    s.scanDepth = 4
    s.rescanForNewSettings() // 正在扫：这次按 2 层，扫完再按 4 层扫一次
    for _ in 0..<200 where fake.depths.count < 2 { try await Task.sleep(for: .milliseconds(10)) }
    // 4 层的还没扫完，不能说扫了 4 层
    #expect(s.scanning && s.screen == .empty(depth: 2))
    for _ in 0..<200 where s.scanning { try await Task.sleep(for: .milliseconds(10)) }
    #expect(fake.depths == [2, 4] && s.screen == .empty(depth: 4))
}

@MainActor @Test func keptItemsPointAtTheirFolder() async throws {
    var r = try decodeSample()
    // 一个检出在留着的 worktree 里的分支，一个主工作区当前检出的分支
    r.repos[0].worktrees[1].branch = "agent/kept"
    var inWorktree = r.repos[0].branches[1], inMain = inWorktree
    inWorktree.name = "agent/kept"
    inMain.name = "feature"
    r.repos[0].branches += [inWorktree, inMain]
    r.repos[0].health?.branch = "feature"
    let s = makeStore(FakeBackend(.success(.report(r))))
    s.refresh()
    await settle(s)
    let app = try #require(s.selectedRepo)
    #expect(app.worktrees.map(s.folder(of:)) == [nil, "/tmp/elsewhere"]) // 可删的不给
    // 可删的、没检出在任何地方的、在 worktree 里的、在主工作区的
    #expect(app.branches.map(s.folder(of:)) == [nil, nil, "/tmp/elsewhere", "/p/app"])
    // 没有体检结果的仓库（health 为空）：不知道主工作区检出了什么，不给
    #expect(s.folder(of: s.repos[2].branches[0]) == nil)
}

@MainActor @Test func openingAMissingFolderSaysWhy() throws {
    let s = makeStore(nil)
    let dir = FileManager.default.temporaryDirectory.appendingPathComponent("repo-tidy-test-\(UUID().uuidString)")
    try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: dir) }
    #expect(s.canOpen(folder: dir.path) && s.openError == nil)
    let gone = dir.path + "/gone"
    #expect(!s.canOpen(folder: gone))
    #expect(s.openError?.contains(gone) == true)
    // 同一个路径换成了文件：不能当文件夹打开（交给终端会被当脚本跑）
    s.openError = nil
    try Data("#!/bin/sh\n".utf8).write(to: URL(fileURLWithPath: gone))
    #expect(!s.canOpen(folder: gone) && s.openError != nil)
}
