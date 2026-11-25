import Foundation
import Testing
@testable import RepoTidyKit

/// 样本里的 worktree 改个名字、大小和最后活动时间（秒）。
private func worktree(_ name: String, size: Int64?, active: TimeInterval?) throws -> Item {
    var it = try decodeSample().repos[0].worktrees[0]
    it.name = name
    it.sizeBytes = size
    it.lastActive = active.map { Date(timeIntervalSince1970: $0) }
    return it
}

private func mixed() throws -> [Item] {
    [try worktree("small-new", size: 10, active: 400),
     try worktree("gone", size: nil, active: nil),
     try worktree("big", size: 80_000_000_000, active: 300),
     try worktree("empty", size: 0, active: 100),
     try worktree("mid-old", size: 5_000, active: 50)]
}

@Test func standardSortKeepsOrder() throws {
    let items = try mixed()
    #expect(WorktreeSort.standard.apply(items) == items)
}

@Test func largestFirstUnmeasuredLast() throws {
    #expect(WorktreeSort.largest.apply(try mixed()).map(\.name) == ["big", "mid-old", "small-new", "empty", "gone"])
}

@Test func longestIdleFirstUnknownLast() throws {
    #expect(WorktreeSort.longestIdle.apply(try mixed()).map(\.name) == ["mid-old", "empty", "big", "small-new", "gone"])
}

@Test func tiesKeepOriginalOrder() throws {
    // 够多才看得出排序稳不稳
    let items = try (0..<200).map { try worktree("w\($0)", size: $0 % 3 == 0 ? nil : 7, active: $0 % 3 == 0 ? nil : 7) }
    let expected = items.filter { $0.sizeBytes != nil } + items.filter { $0.sizeBytes == nil }
    #expect(WorktreeSort.largest.apply(items) == expected)
    #expect(WorktreeSort.longestIdle.apply(items) == expected)
    #expect(WorktreeSort.largest.apply([]).isEmpty)
}

@MainActor @Test func sortIsRememberedAndDoesNotTouchChecks() async throws {
    let defaults = try #require(UserDefaults(suiteName: "repo-tidy-test-\(UUID().uuidString)"))
    var report = try decodeSample()
    report.repos[0].worktrees = try [worktree("a", size: 1, active: 1), worktree("b", size: 2, active: 2)]
    let fake = FakeBackend(.success(.report(report)))
    let s = Store(defaults: defaults, makeBackend: { _ in fake })
    #expect(s.worktreeSort == .standard)
    s.refresh()
    await settle(s)
    s.checkAll(in: try #require(s.selectedRepo))
    let before = s.checkedItems
    s.worktreeSort = .largest
    #expect(s.checkedItems == before) // 勾选和删除顺序不跟着显示变
    #expect(Store(defaults: defaults, makeBackend: { _ in nil }).worktreeSort == .largest)
    defaults.set("no-such-sort", forKey: Store.Keys.worktreeSort)
    #expect(Store(defaults: defaults, makeBackend: { _ in nil }).worktreeSort == .standard)
}
