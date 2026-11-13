import Foundation
import Testing
@testable import RepoTidyKit

/// 和 `repo-tidy --json` 一样形状的样本：一个乱的仓库（可删 worktree + 检出它的分支 + 要留的分支）、一个干净的、一个没扫成的。
let sample = """
{
  "version": 1,
  "generated_at": "2026-11-12T18:05:55+08:00",
  "summary": {"repos": 3, "failed": 1, "clean": 1, "worktrees": 2, "branches": 3,
              "removable_worktrees": 1, "removable_branches": 1, "keep_worktrees": 1, "keep_branches": 2,
              "unmerged": 1, "free_bytes": 2048, "unmeasured": 0},
  "repos": [
    {"root": "/p/app", "default_branch": "main", "note": "", "error": "", "mess": 3, "clean": false,
     "worktrees": [
       {"repo": "/p/app", "kind": "worktree", "name": "/p/app/.claude/worktrees/x", "branch": "agent/x",
        "head": "1111111111111111111111111111111111111111", "verdict": "removable", "basis": "merged",
        "evidence": "内容已 squash 进 main：提交 98ad62bd「合并 x」", "reasons": [], "unmerged": false,
        "last_active": "2026-11-11T09:30:12.123456789+08:00", "size_bytes": 2048},
       {"repo": "/p/app", "kind": "worktree", "name": "/tmp/elsewhere", "branch": "",
        "head": "2222222222222222222222222222222222222222", "verdict": "keep", "basis": "keep",
        "evidence": "", "reasons": ["有未提交改动或未跟踪文件 1 个：a.txt"], "unmerged": true,
        "last_active": null}
     ],
     "branches": [
       {"repo": "/p/app", "kind": "branch", "name": "agent/x", "branch": "agent/x",
        "head": "1111111111111111111111111111111111111111", "verdict": "removable", "basis": "merged",
        "evidence": "内容已 squash 进 main：提交 98ad62bd「合并 x」；先删检出它的 worktree", "reasons": [],
        "unmerged": false, "last_active": "2026-11-10T18:05:55+08:00"},
       {"repo": "/p/app", "kind": "branch", "name": "wip", "branch": "wip",
        "head": "3333333333333333333333333333333333333333", "verdict": "keep", "basis": "keep",
        "evidence": "", "reasons": ["有 1 个提交既不在 main 也没推到任何远程：a6423750「试」"],
        "unmerged": true, "last_active": "2026-11-10T18:05:55+08:00"}
     ],
     "health": {"note": "", "git_dir_bytes": 1, "main_bytes": 2, "branch": "main", "upstream": "",
                "upstream_gone": false, "ahead": 0, "behind": 0, "changes": 2, "untracked": 1, "stashes": 0,
                "last_commit": "2026-11-12T18:05:55+08:00", "last_commit_branch": "main",
                "backup": {"present": true, "online": false, "online_error": "", "checked": 3,
                           "stale": [{"branch": "wip", "state": "not-pushed", "local": "3333", "remote": "",
                                      "local_only": 0, "remote_only": 0}]},
                "errors": []}},
    {"root": "/p/broken", "default_branch": "", "note": "", "error": "列 worktree：exit 128", "mess": 0,
     "clean": false, "worktrees": [], "branches": [], "health": null},
    {"root": "/p/tidy", "default_branch": "main", "note": "", "error": "", "mess": 0, "clean": true,
     "worktrees": [], "branches": [
       {"repo": "/p/tidy", "kind": "branch", "name": "locked", "branch": "locked",
        "head": "4444444444444444444444444444444444444444", "verdict": "keep", "basis": "keep",
        "evidence": "", "reasons": ["主工作区当前检出的分支"], "unmerged": false, "last_active": null}
     ], "health": null}
  ]
}
"""

func decodeSample() throws -> Report {
    try JSON.decoder.decode(Report.self, from: Data(sample.utf8))
}

@Test func decodesReport() throws {
    Lang.current = .zh
    let r = try decodeSample()
    #expect(r.summary.removable == 2 && r.summary.keep == 3 && r.summary.freeBytes == 2048)
    #expect(r.summary.removableLine == "可删 2 项，能腾出 2.0 KB")
    let app = r.repos[0]
    #expect(app.name == "app" && app.removableCount == 2 && app.keepCount == 2 && app.unmergedCount == 2)
    #expect(app.worktrees[0].isRemovable && app.worktrees[0].sizeBytes == 2048)
    #expect(app.worktrees[0].displayName == ".claude/worktrees/x")
    #expect(app.worktrees[1].lastActive == nil && app.worktrees[1].displayName == "/tmp/elsewhere")
    #expect(app.health?.backup.stale.first?.branch == "wip")
    #expect(r.repos.map(\.group) == [.messy, .failed, .clean])
}

@Test func parsesNanosecondTimes() throws {
    let r = try decodeSample()
    let t = try #require(r.repos[0].worktrees[0].lastActive)
    #expect(Int(t.timeIntervalSince1970) == 1_794_360_612)
    #expect(JSON.parseRFC3339("2026-11-12T10:00:00Z") != nil)
    #expect(JSON.parseRFC3339("昨天") == nil)
}

@Test func decodesApplyOutcome() throws {
    let json = """
    {"version": 1, "log": "/s/apply.log", "target": 2, "deleted": 1,
     "results": [{"repo": "/p/app", "kind": "branch", "name": "a", "done": true, "message": "删了"},
                 {"repo": "/p/app", "kind": "branch", "name": "b", "done": false, "message": "提交和勾选时不一样了，这次不动"}],
     "errors": []}
    """
    let o = try JSON.decoder.decode(ApplyOutcome.self, from: Data(json.utf8))
    #expect(o.deleted == 1 && o.results.count == 2 && !o.results[1].done)
}

@Test func selectionCarriesHead() throws {
    let r = try decodeSample()
    let data = try JSONEncoder().encode(Selection([r.repos[0].branches[0]]))
    let obj = try #require(JSONSerialization.jsonObject(with: data) as? [String: [[String: String]]])
    #expect(obj["items"] == [["repo": "/p/app", "kind": "branch", "name": "agent/x",
                              "head": "1111111111111111111111111111111111111111"]])
}

@Test func formatPieces() {
    let p = Format.pieces("内容已 squash 进 main：提交 98ad62bd「合并 x」")
    #expect(p.filter(\.isHash).map(\.text) == ["98ad62bd"])
    #expect(p.map(\.text).joined() == "内容已 squash 进 main：提交 98ad62bd「合并 x」")
    // 普通英文单词、全字母的十六进制样子的词不当成提交号
    #expect(Format.pieces("已推到 backup/agent/1109-search（defaced）").allSatisfy { !$0.isHash })
}

@Test func formatBytesAndAgo() {
    Lang.current = .zh
    #expect(Format.bytes(223) == "223 B")
    #expect(Format.bytes(38_700) == "39 KB")
    #expect(Format.bytes(1_200_000_000) == "1.2 GB")
    let now = Date(timeIntervalSince1970: 1_000_000)
    #expect(Format.ago(now.addingTimeInterval(-30), now: now) == "刚刚")
    #expect(Format.ago(now.addingTimeInterval(-3 * 86400), now: now) == "3 天前")
    #expect(Format.ago(nil) == "不知道")
    #expect(Format.tildePath("/home/me/projects/a", home: "/home/me") == "~/projects/a")
}

@Test func locatesCLIInOrder() {
    let app = "/A/RepoTidy.app/Contents/Helpers/repo-tidy"
    let all: Set<String> = ["/custom/repo-tidy", "/env/repo-tidy", app, "/h/.local/bin/repo-tidy",
                            "/h/go/bin/repo-tidy", "/opt/homebrew/bin/repo-tidy", "/p/repo-tidy"]
    func find(_ custom: String?, env: [String: String] = ["REPO_TIDY_BIN": "/env/repo-tidy", "PATH": "/p"],
              home: String = "/h", bundled: String? = app, exists: Set<String> = all) -> String? {
        CLI.locate(custom: custom, environment: env, home: home, bundled: bundled, isExecutable: exists.contains)?.path
    }
    // 设置里指定的 → $REPO_TIDY_BIN → App 自带的 → 原来那些目录 → PATH
    #expect(find("/custom/repo-tidy") == "/custom/repo-tidy")
    #expect(find("") == "/env/repo-tidy")
    #expect(find(nil, env: ["PATH": "/p"]) == app)
    #expect(find(nil, env: ["REPO_TIDY_BIN": "", "PATH": "/p"]) == app)
    // 设置里的路径不能执行时往后找，不卡在那里
    #expect(find("/没有/repo-tidy", env: [:]) == app)
    // 自带的坏了（没有或不能执行）才轮到常见目录和 PATH
    #expect(find(nil, env: ["PATH": "/p"], exists: all.subtracting([app])) == "/h/.local/bin/repo-tidy")
    #expect(find(nil, env: ["PATH": "/p"], bundled: nil) == "/h/.local/bin/repo-tidy")
    #expect(find(nil, env: ["PATH": "/p"], home: "/x", exists: all.subtracting([app])) == "/opt/homebrew/bin/repo-tidy")
    #expect(find(nil, env: ["PATH": "/p"], home: "/x", exists: ["/p/repo-tidy"]) == "/p/repo-tidy")
    // 一个都没有：界面显示「没找到命令行工具」
    #expect(find(nil, env: ["PATH": "/p"], exists: []) == nil)
}

@Test func bundledPathOnlyInsideApp() {
    #expect(CLI.bundledPath(in: URL(fileURLWithPath: "/Applications/RepoTidy.app"))
        == "/Applications/RepoTidy.app/Contents/Helpers/repo-tidy")
    // swift build 直接跑时 bundle 是放可执行文件的目录，不算 App
    #expect(CLI.bundledPath(in: URL(fileURLWithPath: "/src/mac/.build/debug")) == nil)
}

@Test func childPathPrefersHomebrewGit() {
    let env = CLI.childEnvironment(["PATH": "/usr/bin:/bin"])
    #expect(env["PATH"] == "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin")
}

@Test func parsesProgressLines() {
    #expect(ScanProgress.parse("正在扫 3/13 repo-name") == ScanProgress(current: 3, total: 13, repo: "repo-name"))
    #expect(ScanProgress.parse("Scanning 1/2 my repo") == ScanProgress(current: 1, total: 2, repo: "my repo"))
    for bad in ["repo-tidy：路径 /x：no such file", "正在扫 3/13", "正在扫 0/2 x", "正在扫 3/2 x", "x 1/y z", ""] {
        #expect(ScanProgress.parse(bad) == nil, "\(bad)")
    }
    #expect(ScanProgress.parse("Scanning 3/13 foo") == ScanProgress(current: 3, total: 13, repo: "foo"))
    // 报错里去掉进度行，只留真正的错误；中英两种前缀都去掉
    #expect(CLI.clean("正在扫 1/2 a\nrepo-tidy：读目录 /x：permission denied\n") == "读目录 /x：permission denied")
    #expect(CLI.clean("Scanning 1/2 a\nrepo-tidy: read dir /x: permission denied\n") == "read dir /x: permission denied")
}

@Test func tailDeliversLinesAsTheyAreWritten() async throws {
    let url = FileManager.default.temporaryDirectory.appendingPathComponent("repo-tidy-tail-\(UUID().uuidString)")
    FileManager.default.createFile(atPath: url.path, contents: nil)
    defer {
        // swiftlint:disable:next silent_try
        try? FileManager.default.removeItem(at: url) // 临时文件，删不掉无所谓
    }
    let got = Lines()
    let task = CLI.tail(url) { got.add($0) }
    defer { task.cancel() }
    let h = try FileHandle(forWritingTo: url)
    defer { h.closeFile() }
    h.write(Data("正在扫 1/2 a\n正在扫 2/".utf8))
    for _ in 0..<100 where got.all.isEmpty { try await Task.sleep(for: .milliseconds(20)) }
    #expect(got.all == ["正在扫 1/2 a"]) // 没写完的半行先不给
    h.write(Data("2 b\n".utf8))
    for _ in 0..<100 where got.all.count < 2 { try await Task.sleep(for: .milliseconds(20)) }
    #expect(got.all == ["正在扫 1/2 a", "正在扫 2/2 b"])
}

final class Lines: @unchecked Sendable {
    private let lock = NSLock()
    private var lines: [String] = []
    func add(_ l: String) { lock.withLock { lines.append(l) } }
    var all: [String] { lock.withLock { lines } }
}
