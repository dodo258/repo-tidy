import AppKit
import RepoTidyKit
import SwiftUI

/// 右栏：选中仓库的 worktree 和本地分支，可删项能勾，底部是「清理」。
struct RepoDetail: View {
    @Environment(Store.self) private var store
    let repo: Repo
    let onClean: () -> Void

    var body: some View {
        List {
            header
                .listRowSeparator(.hidden)
            if !repo.error.isEmpty {
                Notice(text: L.repoFailedNotice(repo.error), tint: Theme.held)
            } else if !repo.note.isEmpty {
                Notice(text: repo.note, tint: .secondary)
            }
            itemSection(L.sectionWorktrees, repo.worktrees, empty: L.noWorktrees)
            itemSection(L.sectionBranches, repo.branches, empty: L.noBranches)
        }
        .listStyle(.inset(alternatesRowBackgrounds: false))
        .safeAreaInset(edge: .bottom, spacing: 0) { actionBar }
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
        .padding(.bottom, 4)
    }

    private var pathLine: String {
        var s = Format.tildePath(repo.root)
        if !repo.defaultBranch.isEmpty { s += " · " + L.defaultBranch(repo.defaultBranch) }
        return s
    }

    @ViewBuilder
    private func itemSection(_ title: String, _ items: [Item], empty: String) -> some View {
        Section {
            if items.isEmpty {
                Text(empty).font(Theme.note).foregroundStyle(.tertiary)
            }
            ForEach(items) { ItemRow(item: $0) }
        } header: {
            HStack(spacing: 8) {
                Text(title).font(Theme.body.weight(.semibold)).foregroundStyle(.primary)
                let r = items.filter(\.isRemovable).count
                Text(L.sectionCounts(r, items.count - r)).font(Theme.meta).foregroundStyle(.secondary)
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
        let parts = Self.parts(health)
        if !parts.isEmpty {
            Text(parts.joined(separator: " · ")).font(Theme.note).foregroundStyle(.secondary)
        }
    }

    static func parts(_ h: Health) -> [String] {
        var p: [String] = []
        if !h.backup.present {
            p.append(L.noBackup)
        } else if h.backup.stale.isEmpty {
            p.append(L.backupUpToDate)
        } else {
            p.append(L.backupStale(h.backup.stale.count))
        }
        if h.changes > 0 { p.append(L.uncommitted(h.changes)) }
        if h.stashes > 0 { p.append(L.stashes(h.stashes)) }
        if h.ahead > 0 || h.behind > 0 { p.append(L.aheadBehind(h.ahead, h.behind)) }
        if let t = h.lastCommit {
            let ago = Format.ago(t)
            p.append(h.lastCommitBranch.isEmpty ? L.lastCommit(ago) : L.lastCommitOn(ago, h.lastCommitBranch))
        }
        return p
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
