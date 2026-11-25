import Foundation

/// 右栏 worktree 的排序。只决定显示的先后，不动勾选，也不改清理的范围和删除顺序。
public enum WorktreeSort: String, CaseIterable, Sendable {
    /// 命令行给的顺序。
    case standard
    /// 最占地方的在前；没量到大小的（目录已不在、没扫完）排最后。
    case largest
    /// 最久没动的在前；不知道最后活动时间的排最后，不当成最久没动。
    case longestIdle

    /// 排好的项；比不出先后的保持原来的顺序。
    public func apply(_ items: [Item]) -> [Item] {
        switch self {
        case .standard: items
        case .largest: Self.sorted(items, by: \.sizeBytes, >)
        case .longestIdle: Self.sorted(items, by: \.lastActive, <)
        }
    }

    /// 按 `key` 排，没有值的排最后。标准库没有保证 sort 稳定，所以平手时按原位置比。
    private static func sorted<Key: Comparable>(_ items: [Item], by key: KeyPath<Item, Key?>,
                                                _ before: (Key, Key) -> Bool) -> [Item] {
        items.enumerated().sorted { a, b in
            switch (a.element[keyPath: key], b.element[keyPath: key]) {
            case let (x?, y?) where x != y: before(x, y)
            case (_?, nil): true
            case (nil, _?): false
            default: a.offset < b.offset
            }
        }.map(\.element)
    }
}
