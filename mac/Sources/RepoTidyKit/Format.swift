import Foundation

/// 展示用的格式化：大小、相对时间、路径、证据里的提交号。
public enum Format {
    /// 「223 B」「38.7 KB」「1.2 GB」，按 1000 进位（和 Finder 一致），不随系统语言变。
    public static func bytes(_ n: Int64) -> String {
        if n < 1000 { return "\(n) B" }
        var v = Double(n)
        for unit in ["KB", "MB", "GB", "TB"] {
            v /= 1000
            if v < 1000 || unit == "TB" {
                return v < 10 ? String(format: "%.1f %@", v, unit) : String(format: "%.0f %@", v, unit)
            }
        }
        return "\(n) B"
    }

    /// worktree 显示相对仓库的路径，在仓库外的显示完整路径（家目录缩成 ~）。
    public static func itemName(_ name: String, repo: String) -> String {
        let prefix = repo.hasSuffix("/") ? repo : repo + "/"
        if name.hasPrefix(prefix) { return String(name.dropFirst(prefix.count)) }
        return tildePath(name)
    }

    /// 「刚刚」「3 小时前」「12 天前」；超过一年写日期。
    public static func ago(_ date: Date?, now: Date = Date()) -> String {
        guard let date else { return L.unknownTime }
        let s = now.timeIntervalSince(date)
        switch s {
        case ..<60: return L.justNow
        case ..<3600: return L.minutesAgo(Int(s / 60))
        case ..<86400: return L.hoursAgo(Int(s / 3600))
        case ..<(86400 * 365): return L.daysAgo(Int(s / 86400))
        default:
            let f = DateFormatter()
            f.dateFormat = "yyyy-MM-dd"
            return f.string(from: date)
        }
    }

    public static func clock(_ date: Date) -> String {
        let f = DateFormatter()
        f.dateFormat = "HH:mm"
        return f.string(from: date)
    }

    /// 缩写成 `~` 的主目录。截图模式会换成演示目录，让截图里不出现本机路径。
    nonisolated(unsafe) public static var home = NSHomeDirectory()

    public static func tildePath(_ path: String, home: String = Format.home) -> String {
        if path == home { return "~" }
        if path.hasPrefix(home + "/") { return "~" + path.dropFirst(home.count) }
        return path
    }

    /// 证据里的片段：普通文字，或提交号（7–40 位十六进制，至少含一个数字）。
    public struct Piece: Equatable, Sendable {
        public var text: String
        public var isHash: Bool
    }

    public static func pieces(_ s: String) -> [Piece] {
        var out: [Piece] = []
        var buf = ""
        func flush(_ word: String) {
            let isHash = word.count >= 7 && word.count <= 40
                && word.allSatisfy(\.isHexDigit) && word.contains(where: \.isNumber)
                && word.allSatisfy { !$0.isUppercase }
            if isHash {
                if !buf.isEmpty { out.append(Piece(text: buf, isHash: false)) }
                buf = ""
                out.append(Piece(text: word, isHash: true))
            } else {
                buf += word
            }
        }
        var word = ""
        for ch in s {
            if ch.isASCII && (ch.isLetter || ch.isNumber) {
                word.append(ch)
            } else {
                flush(word)
                word = ""
                buf.append(ch)
            }
        }
        flush(word)
        if !buf.isEmpty { out.append(Piece(text: buf, isHash: false)) }
        return out
    }
}
