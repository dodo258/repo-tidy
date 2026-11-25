import Foundation
import Testing
@testable import RepoTidyKit

/// 对真的命令行跑一遍：用 scripts/demo-repos.sh 在临时目录造仓库，扫描、只删勾选的项、再扫描。
/// 需要环境变量 REPO_TIDY_BIN 指向构建好的 repo-tidy（跑法见 README「开发」）。
@Test(.enabled(if: ProcessInfo.processInfo.environment["REPO_TIDY_BIN"] != nil, "要先构建 repo-tidy 并设 REPO_TIDY_BIN"))
func scanThenApplySelectedAgainstRealCLI() async throws {
    let env = ProcessInfo.processInfo.environment
    let bin = try #require(env["REPO_TIDY_BIN"])
    let tmp = FileManager.default.temporaryDirectory.appendingPathComponent("repo-tidy-it-\(UUID().uuidString)")
    defer {
        // swiftlint:disable:next silent_try
        try? FileManager.default.removeItem(at: tmp) // 临时目录，删不掉无所谓
    }
    let script = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        .appendingPathComponent("scripts/demo-repos.sh")
    let p = Process()
    p.executableURL = URL(fileURLWithPath: "/bin/sh")
    p.arguments = [script.path, tmp.path]
    p.standardOutput = FileHandle.nullDevice
    try p.run()
    p.waitUntilExit()
    #expect(p.terminationStatus == 0)
    let base = tmp.resolvingSymlinksInPath().path
    // 子进程的 git 只读临时配置，日志写进临时目录
    setenv("GIT_CONFIG_GLOBAL", base + "/.gitconfig", 1)
    setenv("GIT_CONFIG_NOSYSTEM", "1", 1)
    setenv("XDG_STATE_HOME", base + "/state", 1)

    let cli = CLI(executable: URL(fileURLWithPath: bin))
    #expect(try await cli.scan(root: base + "/empty") == .noRepos)
    await #expect(throws: CLIError.self) { try await cli.scan(root: base + "/没有这个目录") }

    guard case let .report(before) = try await cli.scan(root: base + "/projects") else {
        Issue.record("messy 应该有仓库")
        return
    }
    let todo = try #require(before.repos.first { $0.name == "todo-app" })
    #expect(before.summary.excluded == 0 && before.repos.contains { $0.name == "site" })

    // 排除 site：它不出现在报告里，总览记跳过 1 个
    guard case let .report(skipped) = try await cli.scan(root: base + "/projects", excludes: [base + "/projects/site"]) else {
        Issue.record("排除一个后应该还有仓库")
        return
    }
    #expect(skipped.summary.excluded == 1 && !skipped.repos.contains { $0.name == "site" })
    #expect(skipped.repos.contains { $0.name == "todo-app" })
    #expect(before.repos.first?.name == "todo-app") // 最乱的排第一
    let removable = todo.items.filter(\.isRemovable)
    #expect(removable.count == 4)
    // 只勾一个 squash 进 main 的分支
    let pick = try #require(removable.first { $0.name == "agent/1107-fix-timezone" })
    // 勾的项所在的仓库被排除了：删除时命令行再挡一次，不删
    let blocked = try await cli.apply([pick], excludes: [todo.root])
    #expect(blocked.deleted == 0 && blocked.target == 0)
    let outcome = try await cli.apply([pick])
    #expect(outcome.deleted == 1 && outcome.target == 1 && !outcome.log.isEmpty)
    #expect(outcome.results.map(\.name) == ["agent/1107-fix-timezone"])
    // 结果里写原来指向的提交，不撤销也能手动恢复
    #expect(outcome.results.first?.message.contains(String(pick.head.prefix(8))) == true)

    guard case let .report(after) = try await cli.scan(root: base + "/projects") else {
        Issue.record("再扫描应该还有仓库")
        return
    }
    let todoAfter = try #require(after.repos.first { $0.name == "todo-app" })
    #expect(!todoAfter.items.contains { $0.name == "agent/1107-fix-timezone" })
    #expect(todoAfter.items.filter(\.isRemovable).count == 3) // 其余可删项没被动
    #expect(todoAfter.keepCount == todo.keepCount)

    // 撤销：按删除时的恢复日志把分支建回来，指向原来的提交
    let undone = try await cli.undo(log: outcome.log)
    #expect(undone.restored == 1 && undone.results.map(\.status) == [.restored])
    guard case let .report(restored) = try await cli.scan(root: base + "/projects") else {
        Issue.record("撤销后再扫描应该还有仓库")
        return
    }
    let back = try #require(restored.repos.first { $0.name == "todo-app" }?.items.first { $0.name == pick.name })
    #expect(back.head == pick.head)
    // 再撤销一次：已经在了，跳过
    #expect(try await cli.undo(log: outcome.log).results.map(\.status) == [.exists])
}
