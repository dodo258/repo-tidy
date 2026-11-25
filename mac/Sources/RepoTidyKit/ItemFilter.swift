import Foundation

/// 右栏的筛选：只看可删、按名字搜。只决定显示哪些行，不动勾选，也不改清理的范围。
public struct ItemFilter: Equatable, Sendable {
    public var onlyRemovable = false
    /// 搜索框里的字。按空白拆成几个词，每个词都要出现在列表里显示的名字、完整路径或分支名里（不分大小写）。
    public var query = ""

    public init(onlyRemovable: Bool = false, query: String = "") {
        self.onlyRemovable = onlyRemovable
        self.query = query
    }

    private var terms: [Substring] { query.split(whereSeparator: \.isWhitespace) }

    public var isActive: Bool { onlyRemovable || !terms.isEmpty }

    /// 符合搜索词的项（不看「只看可删」）：分段按钮上的两个数按它算。
    public func matchingQuery(_ items: [Item]) -> [Item] {
        let terms = terms
        guard !terms.isEmpty else { return items }
        return items.filter { item in
            let fields = [item.displayName, item.name, item.branch]
            return terms.allSatisfy { t in fields.contains { $0.localizedCaseInsensitiveContains(t) } }
        }
    }

    /// 筛完要显示的项，顺序不变。
    public func apply(_ items: [Item]) -> [Item] {
        let matched = matchingQuery(items)
        return onlyRemovable ? matched.filter(\.isRemovable) : matched
    }

    /// 段标题上的计数，按这一段筛完要显示的项算。只看可删时留着的项都藏起来了，
    /// 只写可删几项，不写「留着 0」，免得看成没有要留着的。
    public func sectionCounts(_ items: [Item]) -> String {
        let shown = apply(items)
        let removable = shown.filter(\.isRemovable).count
        return onlyRemovable ? L.removableCount(removable) : L.sectionCounts(removable, shown.count - removable)
    }
}
