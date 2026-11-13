import Foundation

// 这些结构和 `repo-tidy --json` 的字段一一对应（见仓库 docs/reference.md「JSON 输出」）。
// 界面只展示和转交，不重新判定。

public struct Report: Decodable, Sendable, Equatable {
    public var version: Int
    public var generatedAt: Date
    public var summary: Summary
    public var repos: [Repo]
}

public struct Summary: Decodable, Sendable, Equatable {
    public var repos: Int
    public var failed: Int
    public var clean: Int
    public var worktrees: Int
    public var branches: Int
    public var removableWorktrees: Int
    public var removableBranches: Int
    public var keepWorktrees: Int
    public var keepBranches: Int
    public var unmerged: Int
    public var freeBytes: Int64
    public var unmeasured: Int

    public var removable: Int { removableWorktrees + removableBranches }

    /// 「可删 5 项，能腾出 223 B」；没有可删 worktree（删分支不腾空间）时不提空间。
    public var removableLine: String {
        freeBytes > 0 ? L.removableFreeing(removable, Format.bytes(freeBytes)) : L.removableLine(removable)
    }
    public var keep: Int { keepWorktrees + keepBranches }
}

public struct Repo: Decodable, Sendable, Equatable, Identifiable {
    public var root: String
    public var defaultBranch: String
    public var note: String
    public var error: String
    public var mess: Int
    public var clean: Bool
    public var worktrees: [Item]
    public var branches: [Item]
    public var health: Health?

    public var id: String { root }
    public var name: String { (root as NSString).lastPathComponent }
    public var items: [Item] { worktrees + branches }
    public var removableCount: Int { items.filter(\.isRemovable).count }
    public var keepCount: Int { items.count - removableCount }
    public var unmergedCount: Int { items.filter(\.unmerged).count }

    /// 侧栏分组：没扫成、要整理、干净。
    public enum Group: Int, Sendable, CaseIterable { case messy, failed, clean }
    public var group: Group {
        if !error.isEmpty { return .failed }
        return clean ? .clean : .messy
    }
}

public struct Item: Decodable, Sendable, Equatable, Hashable, Identifiable {
    public enum Kind: String, Decodable, Sendable { case worktree, branch }

    public var repo: String
    public var kind: Kind
    public var name: String
    public var branch: String
    public var head: String
    public var verdict: String
    public var basis: String
    public var evidence: String
    public var reasons: [String]
    public var unmerged: Bool
    public var lastActive: Date?
    public var remote: String?
    public var remoteRef: String?
    public var sizeBytes: Int64?

    public var id: String { "\(repo)\u{0}\(kind.rawValue)\u{0}\(name)" }
    public var isRemovable: Bool { verdict == "removable" }

    /// 列表里显示的名字：worktree 显示相对仓库的路径，在仓库外的显示完整路径（家目录缩成 ~）。
    public var displayName: String {
        kind == .worktree ? Format.itemName(name, repo: repo) : name
    }
}

public struct Health: Decodable, Sendable, Equatable {
    public var note: String
    public var branch: String
    public var upstream: String
    public var upstreamGone: Bool
    public var ahead: Int
    public var behind: Int
    public var changes: Int
    public var untracked: Int
    public var stashes: Int
    public var lastCommit: Date?
    public var lastCommitBranch: String
    public var backup: Backup
    public var errors: [String]

    public struct Backup: Decodable, Sendable, Equatable {
        public var present: Bool
        public var stale: [Stale]
    }

    public struct Stale: Decodable, Sendable, Equatable {
        public var branch: String
        public var state: String
    }
}

/// `repo-tidy --apply --json` 的输出。
public struct ApplyOutcome: Decodable, Sendable, Equatable {
    public var log: String
    public var target: Int
    public var deleted: Int
    public var results: [Entry]
    public var errors: [RepoError]

    public struct Entry: Decodable, Sendable, Equatable, Identifiable {
        public var repo: String
        public var kind: Item.Kind
        public var name: String
        public var done: Bool
        public var message: String
        public var id: String { "\(repo)\u{0}\(kind.rawValue)\u{0}\(name)" }
    }

    public struct RepoError: Decodable, Sendable, Equatable {
        public var repo: String
        public var error: String
    }
}

/// `repo-tidy --undo <日志> --apply --json` 的输出：按恢复日志把删掉的项建回来的逐项结果。
public struct UndoOutcome: Decodable, Sendable, Equatable {
    public var log: String
    public var restored: Int
    public var results: [Entry]

    public struct Entry: Decodable, Sendable, Equatable, Identifiable {
        /// 恢复了、已经在了（跳过）、提交已被 gc 掉、失败；ready 只在不加 --apply 的预览里出现。
        public enum Status: String, Decodable, Sendable { case ready, restored, exists, lost, failed }
        public var repo: String
        public var kind: Item.Kind
        public var name: String
        public var status: Status
        public var message: String
        public var id: String { "\(repo)\u{0}\(kind.rawValue)\u{0}\(name)" }
    }

    /// 没恢复成的项数（已经在了的也算）。
    public var notRestored: Int { results.count - restored }
}

/// 交给 `repo-tidy --apply --select` 的勾选项，字段和 --json 每一项相同。
struct Selection: Encodable {
    struct Want: Encodable { var repo, kind, name, head: String }
    var items: [Want]

    init(_ items: [Item]) {
        self.items = items.map { Want(repo: $0.repo, kind: $0.kind.rawValue, name: $0.name, head: $0.head) }
    }
}

enum JSON {
    static let decoder: JSONDecoder = {
        let d = JSONDecoder()
        d.keyDecodingStrategy = .convertFromSnakeCase
        d.dateDecodingStrategy = .custom { dec in
            let s = try dec.singleValueContainer().decode(String.self)
            guard let date = parseRFC3339(s) else {
                throw DecodingError.dataCorrupted(.init(codingPath: dec.codingPath, debugDescription: L.badTime(s)))
            }
            return date
        }
        return d
    }()

    /// Go 输出的 RFC 3339 时间可能带 1–9 位小数秒；ISO8601DateFormatter 只认 3 位，先把小数去掉。
    static func parseRFC3339(_ s: String) -> Date? {
        var t = s
        if let dot = t.firstIndex(of: "."), let end = t[dot...].firstIndex(where: { !$0.isNumber && $0 != "." }) {
            t.removeSubrange(dot..<end)
        }
        return ISO8601DateFormatter().date(from: t)
    }
}
