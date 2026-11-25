import Foundation
import Testing
@testable import RepoTidyKit

// 样本仓库 app：worktree x（可删，分支 agent/x）、/tmp/elsewhere（留着，游离 HEAD）；分支 agent/x（可删）、wip（留着）。

@Test func emptyFilterShowsEverythingInOrder() throws {
    let app = try decodeSample().repos[0]
    let f = ItemFilter()
    #expect(!f.isActive)
    #expect(f.apply(app.items) == app.items)
    #expect(!ItemFilter(query: "  \t ").isActive) // 只有空白不算筛选
}

@Test func onlyRemovableHidesKeptItems() throws {
    let app = try decodeSample().repos[0]
    let f = ItemFilter(onlyRemovable: true)
    #expect(f.isActive)
    #expect(f.apply(app.worktrees).map(\.name) == ["/p/app/.claude/worktrees/x"])
    #expect(f.apply(app.branches).map(\.name) == ["agent/x"])
}

@Test func queryMatchesNamePathOrBranchIgnoringCase() throws {
    let app = try decodeSample().repos[0]
    // 列表里显示的相对路径
    #expect(ItemFilter(query: "WORKTREES/X").apply(app.items).map(\.name) == ["/p/app/.claude/worktrees/x"])
    // 在仓库外的 worktree 按完整路径找得到
    #expect(ItemFilter(query: "elsewhere").apply(app.worktrees).map(\.name) == ["/tmp/elsewhere"])
    // worktree 按它检出的分支也找得到
    #expect(ItemFilter(query: "agent/").apply(app.items).map(\.kind) == [.worktree, .branch])
    // 几个词都要对上（顺序不限）
    #expect(ItemFilter(query: "x agent").apply(app.items).count == 2)
    #expect(ItemFilter(query: "agent wip").apply(app.items).isEmpty)
    // 不搜证据和原因里的字
    #expect(ItemFilter(query: "squash").apply(app.items).isEmpty)
}

@Test func countsForTheSegmentFollowTheQueryOnly() throws {
    let app = try decodeSample().repos[0]
    let f = ItemFilter(onlyRemovable: true, query: "wip")
    #expect(f.matchingQuery(app.items).map(\.name) == ["wip"]) // 分段按钮上「全部 1」
    #expect(f.apply(app.items).isEmpty) // 只看可删时一个都没有
}

@Test func sectionCountsFollowTheShownItems() throws {
    Lang.current = .zh
    let app = try decodeSample().repos[0]
    // 没筛选：整段的数
    #expect(ItemFilter().sectionCounts(app.worktrees) == "可删 1 · 留着 1")
    #expect(ItemFilter().sectionCounts(app.branches) == "可删 1 · 留着 1")
    // 搜索：只数搜到的
    #expect(ItemFilter(query: "agent/x").sectionCounts(app.worktrees) == "可删 1 · 留着 0")
    #expect(ItemFilter(query: "wip").sectionCounts(app.branches) == "可删 0 · 留着 1")
    #expect(ItemFilter(query: "wip").sectionCounts(app.worktrees) == "可删 0 · 留着 0")
    // 只看可删：不写留着
    #expect(ItemFilter(onlyRemovable: true).sectionCounts(app.branches) == "可删 1")
    #expect(ItemFilter(onlyRemovable: true, query: "wip").sectionCounts(app.branches) == "可删 0")
}

@MainActor @Test func filterSurvivesRescanAndDoesNotTouchChecks() async throws {
    let s = makeStore(FakeBackend(.success(.report(try decodeSample()))))
    s.refresh()
    await settle(s)
    let app = try #require(s.selectedRepo)
    s.checkAll(in: app)
    s.filter = ItemFilter(onlyRemovable: true, query: "wip")
    #expect(s.checkedItems.count == 2) // 筛掉的勾选项照样留着、照样清理
    s.refresh()
    await settle(s)
    #expect(s.filter == ItemFilter(onlyRemovable: true, query: "wip"))
    #expect(s.checkedItems.count == 2)
}
