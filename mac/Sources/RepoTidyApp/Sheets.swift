import AppKit
import RepoTidyKit
import SwiftUI

/// 点「清理」后的确认：逐项列出要删的东西和证据，确认了才调命令行。
struct ConfirmSheet: View {
    @Environment(Store.self) private var store
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        let items = store.checkedItems
        let groups = Dictionary(grouping: items, by: \.repo)
        let order = store.repos.map(\.root).filter { groups[$0] != nil }
        VStack(alignment: .leading, spacing: 12) {
            Text("删除这 \(items.count) 项？").font(Theme.repoTitle)
            Text("删之前 repo-tidy 会逐项再判定一次，和现在不一样的不删；靠「已推到远程」的会先联网确认远程还在。worktree 用 git worktree remove（不加 --force），恢复命令写进日志。")
                .font(Theme.note).foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    ForEach(order, id: \.self) { root in
                        VStack(alignment: .leading, spacing: 8) {
                            Text(Format.tildePath(root)).font(Theme.body.weight(.semibold))
                            ForEach(groups[root] ?? []) { line($0) }
                        }
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(12)
            }
            .frame(minHeight: 140, maxHeight: 360)
            .background(RoundedRectangle(cornerRadius: Theme.blockRadius).fill(.background))
            .overlay(RoundedRectangle(cornerRadius: Theme.blockRadius).strokeBorder(.separator))
            HStack {
                Spacer()
                Button("取消", role: .cancel) { dismiss() }.keyboardShortcut(.cancelAction)
                Button("删除 \(items.count) 项", role: .destructive) {
                    dismiss()
                    Task { await store.applyChecked() }
                }
                .keyboardShortcut(.defaultAction)
                .buttonStyle(ClearedButtonStyle())
            }
        }
        .padding(20)
        .frame(width: 600)
    }

    private func line(_ it: Item) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 8) {
                Text(it.kind == .worktree ? "worktree" : "分支").font(Theme.meta).foregroundStyle(.secondary)
                    .frame(width: 56, alignment: .leading)
                Text(it.displayName).font(Theme.name).lineLimit(1).truncationMode(.middle)
            }
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Color.clear.frame(width: 56, height: 1)
                VStack(alignment: .leading, spacing: 2) {
                    Evidence(text: it.evidence)
                    if let wt = store.worktreeFor(it), store.isChecked(wt) {
                        Text("检出在上面那个 worktree 里，worktree 删掉后才删得了它").font(Theme.meta).foregroundStyle(.tertiary)
                    }
                }
            }
        }
    }
}

/// 删完的逐项结果。
struct OutcomeSheet: View {
    @Environment(Store.self) private var store
    @Environment(\.dismiss) private var dismiss
    let outcome: ApplyOutcomeBox

    var body: some View {
        let o = outcome.outcome
        let skipped = o.results.filter { !$0.done }.count
        VStack(alignment: .leading, spacing: 12) {
            Text(skipped == 0 && o.errors.isEmpty ? "删了 \(o.deleted) 项" : "删了 \(o.deleted) 项，\(skipped) 项没删")
                .font(Theme.repoTitle)
            ScrollView {
                VStack(alignment: .leading, spacing: 8) {
                    ForEach(o.results) { r in
                        HStack(alignment: .firstTextBaseline, spacing: 8) {
                            Text(r.done ? "已删" : "没删").font(Theme.verdict)
                                .foregroundStyle(r.done ? AnyShapeStyle(Theme.cleared) : AnyShapeStyle(Theme.held))
                                .frame(width: 30, alignment: .leading)
                            VStack(alignment: .leading, spacing: 2) {
                                Text((r.kind == .worktree ? "worktree " : "分支 ") + Format.itemName(r.name, repo: r.repo)).font(Theme.name)
                                Text(r.message).font(Theme.note).foregroundStyle(.secondary)
                            }
                        }
                    }
                    ForEach(o.errors, id: \.repo) { e in
                        Text("\(Format.tildePath(e.repo))：\(e.error)").font(Theme.note).foregroundStyle(Theme.held)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(12)
            }
            .frame(minHeight: 100, maxHeight: 320)
            .overlay(RoundedRectangle(cornerRadius: Theme.blockRadius).strokeBorder(.separator))
            HStack {
                if !o.log.isEmpty {
                    Text("恢复命令在 \(Format.tildePath(o.log))").font(Theme.meta).foregroundStyle(.secondary)
                        .lineLimit(1).truncationMode(.middle).textSelection(.enabled)
                    Button("在 Finder 中显示") {
                        NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: o.log)])
                    }
                    .buttonStyle(.link).font(Theme.meta)
                }
                Spacer()
                Button("好") { store.outcome = nil; dismiss() }.keyboardShortcut(.defaultAction)
            }
        }
        .padding(20)
        .frame(width: 600)
    }
}
