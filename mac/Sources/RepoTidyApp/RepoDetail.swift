import AppKit
import RepoTidyKit
import SwiftUI

/// 右栏：选中仓库的 worktree 和本地分支，可删项能勾，底部是「清理」。
struct RepoDetail: View {
    @Environment(Store.self) private var store
    let repo: Repo
    let onClean: () -> Void

    /// 每加一次就把光标放进搜索框（⌘F）。
    @State private var focusSearch = 0

    /// 仓库标题和筛选条固定在上面，几十上百项时滚到哪都能改筛选。
    var body: some View {
        ScrollViewReader { proxy in
            List {
                if !repo.error.isEmpty {
                    Notice(text: L.repoFailedNotice(repo.error), tint: Theme.held)
                } else if !repo.note.isEmpty {
                    Notice(text: repo.note, tint: .secondary)
                }
                itemSection(L.sectionWorktrees, worktrees, empty: L.noWorktrees) {
                    if repo.worktrees.count > 1 { sortMenu }
                }
                itemSection(L.sectionBranches, repo.branches, empty: L.noBranches) {}
            }
            .listStyle(.inset(alternatesRowBackgrounds: false))
            // 滚到中间时换了排序，回到排在最前的那一项。对齐到底边：它就在列表开头，滚不到那么远，停在最顶上；
            // 对齐到顶边会被吸顶的段标题盖住
            .onChange(of: store.worktreeSort) {
                if let first = store.filter.apply(worktrees).first { proxy.scrollTo(first.id, anchor: .bottom) }
            }
        }
        .safeAreaInset(edge: .top, spacing: 0) {
            VStack(alignment: .leading, spacing: 10) {
                header
                if canFilter { filterBar }
            }
            .padding(.horizontal, Theme.pad)
            .padding(.bottom, 10)
            .background(.background)
        }
        .safeAreaInset(edge: .bottom, spacing: 0) { actionBar }
        .focusedSceneValue(\.findItems, canFilter ? { focusSearch += 1 } : nil)
    }

    private var worktrees: [Item] { store.worktreeSort.apply(repo.worktrees) }

    /// 扫成了、有项可筛时才有筛选条。
    private var canFilter: Bool { repo.error.isEmpty && !repo.items.isEmpty }

    /// 「全部 9｜只看可删 4」和搜索框。两个数按搜索词筛过之后算。
    private var filterBar: some View {
        @Bindable var store = store
        let matched = store.filter.matchingQuery(repo.items)
        let removable = matched.filter(\.isRemovable).count
        return HStack(spacing: 12) {
            Picker(L.menuFind, selection: $store.filter.onlyRemovable) {
                Text(L.filterAll(matched.count)).monospacedDigit().tag(false)
                Text(L.filterRemovable(removable)).monospacedDigit().tag(true)
            }
            .pickerStyle(.segmented).labelsHidden().fixedSize()
            Spacer(minLength: 12)
            SearchField(text: $store.filter.query, prompt: L.filterPrompt, focusRequest: focusSearch)
                .frame(maxWidth: 260)
        }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                Text(repo.name).font(Theme.repoTitle)
                Spacer()
                Button(L.showInFinder) {
                    NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: repo.root)])
                }
                .buttonStyle(.link)
                .font(Theme.note)
            }
            Text(pathLine).font(Theme.note).foregroundStyle(.secondary).textSelection(.enabled)
            if let h = repo.health, h.note.isEmpty {
                HealthLine(health: h)
            }
        }
        .padding(.top, 8)
    }

    private var pathLine: String {
        var s = Format.tildePath(repo.root)
        if !repo.defaultBranch.isEmpty { s += " · " + L.defaultBranch(repo.defaultBranch) }
        return s
    }

    /// Worktree 段标题右边的排序菜单，对着下面的时间和大小两列。
    private var sortMenu: some View {
        @Bindable var store = store
        return Picker(L.sortWorktrees, selection: $store.worktreeSort) {
            Text(L.sortStandard).tag(WorktreeSort.standard)
            Text(L.sortLargest).tag(WorktreeSort.largest)
            Text(L.sortLongestIdle).tag(WorktreeSort.longestIdle)
        }
        .pickerStyle(.menu).buttonStyle(.borderless).controlSize(.small).labelsHidden().fixedSize()
        .help(L.sortWorktrees)
    }

    @ViewBuilder
    private func itemSection(_ title: String, _ items: [Item], empty: String,
                             @ViewBuilder trailing: () -> some View) -> some View {
        let shown = store.filter.apply(items)
        Section {
            if shown.isEmpty {
                Text(items.isEmpty ? empty : L.filterNoMatch).font(Theme.note).foregroundStyle(.tertiary)
            }
            ForEach(shown) { ItemRow(item: $0) }
        } header: {
            HStack(spacing: 8) {
                Text(title).font(Theme.body.weight(.semibold)).foregroundStyle(.primary)
                Text(store.filter.sectionCounts(items)).font(Theme.meta).foregroundStyle(.secondary)
                Spacer(minLength: 8)
                trailing()
            }
            .padding(.top, 6)
        }
    }

    private var actionBar: some View {
        let total = store.checked.count
        let here = store.checkedCount(in: repo)
        let removable = repo.removableCount
        return VStack(spacing: 0) {
            Divider()
            HStack(spacing: 12) {
                Text(selectionText(total: total, here: here))
                    .font(Theme.note).foregroundStyle(.secondary)
                Spacer()
                if removable > 0 {
                    if here == removable {
                        Button(L.uncheckRepo) { store.uncheckAll(in: repo) }
                    } else {
                        Button(L.checkRepo(removable)) { store.checkAll(in: repo) }
                    }
                }
                Button(total == 0 ? L.cleanUp : L.cleanUpCount(total), action: onClean)
                    .buttonStyle(ClearedButtonStyle())
                    .disabled(total == 0 || store.applying || store.scanning)
                    .help("⌘⌫")
            }
            .padding(.horizontal, Theme.pad)
            .padding(.vertical, 10)
        }
        .background(.bar)
    }

    private func selectionText(total: Int, here: Int) -> String {
        if total == 0 { return repo.removableCount > 0 ? L.hintCheck : L.noSafeItems }
        if total == here { return L.checkedCount(total) }
        return L.checkedAcross(total, here)
    }
}

/// 一行 worktree 或分支：勾选框、名字、结论、证据或原因、最后活动、大小。
struct ItemRow: View {
    @Environment(Store.self) private var store
    let item: Item

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Toggle("", isOn: Binding(get: { store.isChecked(item) }, set: { _ in store.toggle(item) }))
                .toggleStyle(.checkbox)
                .labelsHidden()
                .disabled(!item.isRemovable)
                .help(item.isRemovable ? L.checkHelpRemovable : L.checkHelpKeep)
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(item.displayName).font(Theme.name).lineLimit(1).truncationMode(.middle)
                        .textSelection(.enabled)
                    if item.kind == .worktree {
                        Text(item.branch.isEmpty ? L.detachedHead : item.branch)
                            .font(Theme.meta).foregroundStyle(.secondary).lineLimit(1)
                    }
                    Spacer(minLength: 8)
                    Text(Format.ago(item.lastActive)).font(Theme.meta).foregroundStyle(.secondary)
                        .help(item.kind == .worktree ? L.lastActiveHelpWorktree : L.lastActiveHelpBranch)
                    if let size = item.sizeBytes {
                        Text(Format.bytes(size)).font(Theme.meta).foregroundStyle(.secondary)
                            .frame(minWidth: 56, alignment: .trailing)
                    }
                }
                Verdict(item: item)
            }
        }
        .padding(.vertical, 4)
    }
}

/// 结论一行：「可删」+ 证据（提交号印成核销印），或「留着」+ 每条原因。
struct Verdict: View {
    let item: Item

    var body: some View {
        if item.isRemovable {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(L.verdictRemovable).font(Theme.verdict).foregroundStyle(Theme.cleared)
                Evidence(text: item.evidence)
            }
        } else {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(L.verdictKeep).font(Theme.verdict)
                    .foregroundStyle(item.unmerged ? AnyShapeStyle(Theme.held) : AnyShapeStyle(.secondary))
                VStack(alignment: .leading, spacing: 2) {
                    ForEach(item.reasons, id: \.self) {
                        Text($0).font(Theme.note).foregroundStyle(.secondary).textSelection(.enabled)
                    }
                }
            }
        }
    }
}

/// 证据文字；里面的提交号单独印出来。
struct Evidence: View {
    let text: String

    var body: some View {
        Format.pieces(text).reduce(Text("")) { acc, p in
            if p.isHash {
                // 用 AttributedString 给提交号加底色；Text 拼接不支持背景，所以底色靠 attributes
                var a = AttributedString(" \(p.text) ")
                a.font = Theme.hash
                a.foregroundColor = Theme.cleared
                a.backgroundColor = Theme.stamp
                return acc + Text(a)
            }
            return acc + Text(p.text).font(Theme.note).foregroundColor(.secondary)
        }
        .textSelection(.enabled)
        .fixedSize(horizontal: false, vertical: true)
    }
}

struct HealthLine: View {
    let health: Health

    var body: some View {
        let parts = health.summaryParts
        if !parts.isEmpty {
            Text(parts.joined(separator: " · ")).font(Theme.note).foregroundStyle(.secondary)
        }
    }
}

struct Notice: View {
    let text: String
    let tint: Color

    var body: some View {
        Text(text).font(Theme.note).foregroundStyle(tint)
            .padding(8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: Theme.blockRadius).strokeBorder(tint.opacity(0.4)))
            .textSelection(.enabled)
    }
}

/// 系统搜索框：放大镜、清除按钮、Esc 清空都是 NSSearchField 自带的。`focusRequest` 变一次就把光标放进来。
struct SearchField: NSViewRepresentable {
    @Binding var text: String
    let prompt: String
    let focusRequest: Int

    func makeNSView(context: Context) -> NSSearchField {
        let field = NSSearchField()
        field.sendsSearchStringImmediately = true
        field.target = context.coordinator
        field.action = #selector(Coordinator.changed(_:))
        return field
    }

    func updateNSView(_ field: NSSearchField, context: Context) {
        context.coordinator.text = $text
        if field.stringValue != text { field.stringValue = text }
        field.placeholderString = prompt
        if context.coordinator.focusRequest != focusRequest {
            context.coordinator.focusRequest = focusRequest
            field.window?.makeFirstResponder(field)
        }
    }

    func makeCoordinator() -> Coordinator { Coordinator(text: $text, focusRequest: focusRequest) }

    @MainActor final class Coordinator: NSObject {
        var text: Binding<String>
        var focusRequest: Int

        init(text: Binding<String>, focusRequest: Int) {
            self.text = text
            self.focusRequest = focusRequest
        }

        @objc func changed(_ sender: NSSearchField) { text.wrappedValue = sender.stringValue }
    }
}
