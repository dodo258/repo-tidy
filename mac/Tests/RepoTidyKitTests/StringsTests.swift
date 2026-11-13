import Foundation
import Testing
@testable import RepoTidyKit

/// 把一套文案的每一项都取出文字：普通文字直接取，带参数的用样例参数调一次。
/// 遇到认不得的类型直接记失败，免得新加的项漏测。
func allTexts(_ s: Strings) -> [(String, String)] {
    Mirror(reflecting: s).children.compactMap { child in
        let name = child.label ?? "?"
        switch child.value {
        case let v as String: return (name, v)
        case let f as Fmt1<Int>: return (name, f(2))
        case let f as Fmt1<String>: return (name, f("x"))
        case let f as Fmt2<Int, Int>: return (name, f(2, 1))
        case let f as Fmt2<Int, String>: return (name, f(2, "x"))
        case let f as Fmt2<String, String>: return (name, f("x", "y"))
        case let f as Fmt3<Int, Int, Int>: return (name, f(3, 2, 1))
        case let f as Fmt4<Int, Int, Int, Int>: return (name, f(3, 2, 1, 1))
        default:
            Issue.record("\(name) 的类型测试认不得：\(type(of: child.value))")
            return nil
        }
    }
}

func hasCJK(_ s: String) -> Bool {
    s.unicodeScalars.contains { u in
        switch u.value {
        case 0x1100...0x11FF, 0x2E80...0x2FDF, 0x3000...0x303F, 0x3040...0x30FF, 0x3100...0x31FF,
             0x3400...0x4DBF, 0x4E00...0x9FFF, 0xAC00...0xD7AF, 0xF900...0xFAFF, 0xFF00...0xFFEF,
             0x20000...0x3FFFF:
            return true
        default:
            return false
        }
    }
}

@Test func everyStringFilledInBothLanguages() {
    let zh = allTexts(.zh), en = allTexts(.en)
    #expect(zh.count == en.count && zh.count > 100)
    for (name, text) in zh + en {
        #expect(!text.trimmingCharacters(in: .whitespaces).isEmpty, "\(name) 是空的")
    }
}

@Test func englishHasNoCJK() {
    #expect(hasCJK("正在扫") && hasCJK("「") && !hasCJK("Scanning 3/13 — ok…"))
    for (name, text) in allTexts(.en) {
        #expect(!hasCJK(text), "\(name)：\(text)")
    }
}

@Test func englishCountsAgree() {
    let en = Strings.en
    #expect(en.daysAgo(1) == "1 day ago" && en.daysAgo(3) == "3 days ago")
    #expect(en.overviewCounts(1, 2, 1) == "1 repo · 2 worktrees · 1 branch")
    #expect(en.removableFreeing(2, "2.0 KB") == "2 removable, frees 2.0 KB")
    #expect(en.progress(3, 13) == "Scanning 3/13")
    #expect(en.confirmTitle(1) == "Delete 1 Item?")
}

@Test func detectsLanguageFromPreferredList() {
    #expect(Lang.detect(["zh-Hans-CN", "en-US"]) == .zh)
    #expect(Lang.detect(["zh-Hant-TW"]) == .zh)
    #expect(Lang.detect(["en-US", "zh-Hans"]) == .en)
    #expect(Lang.detect(["ja-JP"]) == .en)
    #expect(Lang.detect([]) == .en)
}

@Test func cliPassesLang() {
    let exe = URL(fileURLWithPath: "/bin/repo-tidy")
    let en = CLI(executable: exe, lang: .en), zh = CLI(executable: exe, lang: .zh)
    #expect(en.scanArgs(root: "/p", depth: 1, online: false)
        == ["--json", "--progress", "--lang", "en", "--depth", "1", "/p"])
    #expect(zh.scanArgs(root: "/p", depth: 3, online: true)
        == ["--json", "--progress", "--lang", "zh", "--depth", "3", "--online", "/p"])
    #expect(en.applyArgs(select: "/t/s.json") == ["--apply", "--json", "--lang", "en", "--select", "/t/s.json"])
}
