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
            Text(L.confirmTitle(items.count)).font(Theme.repoTitle)
            Text(L.confirmBody)
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
                Button(L.cancel, role: .cancel) { dismiss() }.keyboardShortcut(.cancelAction)
                Button(L.deleteCount(items.count), role: .destructive) {
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
                Text(it.kind == .worktree ? L.kindWorktree : L.kindBranch).font(Theme.meta).foregroundStyle(.secondary)
                    .frame(width: 56, alignment: .leading)
                Text(it.displayName).font(Theme.name).lineLimit(1).truncationMode(.middle)
            }
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Color.clear.frame(width: 56, height: 1)
                VStack(alignment: .leading, spacing: 2) {
                    Evidence(text: it.evidence)
                    if let wt = store.worktreeFor(it), store.isChecked(wt) {
                        Text(L.checkedOutAbove).font(Theme.meta).foregroundStyle(.tertiary)
                    }
                }
            }
        }
    }
}

/// 删完的逐项结果；点「撤销这次清理」后换成恢复的逐项结果。
struct OutcomeSheet: View {
    @Environment(Store.self) private var store
    @Environment(\.dismiss) private var dismiss
    let outcome: ApplyOutcomeBox

    var body: some View {
        let o = outcome.outcome
        VStack(alignment: .leading, spacing: 12) {
            Text(title(o)).font(Theme.repoTitle)
            ScrollView {
                VStack(alignment: .leading, spacing: 8) {
                    if let u = store.undoOutcome {
                        ForEach(u.results) { r in
                            row(mark(r.status), color: color(r.status), marks: Self.undoMarks,
                                kind: r.kind, name: Format.itemName(r.name, repo: r.repo), message: r.message)
                        }
                    } else {
                        ForEach(o.results) { r in
                            row(r.done ? L.resultDone : L.resultSkipped,
                                color: r.done ? AnyShapeStyle(Theme.cleared) : AnyShapeStyle(Theme.held),
                                marks: [L.resultDone, L.resultSkipped],
                                kind: r.kind, name: Format.itemName(r.name, repo: r.repo), message: r.message)
                        }
                        ForEach(o.errors, id: \.repo) { e in
                            Text(L.repoError(Format.tildePath(e.repo), e.error)).font(Theme.note).foregroundStyle(Theme.held)
                        }
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(12)
            }
            .frame(minHeight: 100, maxHeight: 320)
            .overlay(RoundedRectangle(cornerRadius: Theme.blockRadius).strokeBorder(.separator))
            if let err = store.undoError {
                Text(L.undoError(err)).font(Theme.note).foregroundStyle(Theme.held)
                    .fixedSize(horizontal: false, vertical: true)
            }
            HStack {
                if !o.log.isEmpty {
                    Text(L.restoreLog(Format.tildePath(o.log))).font(Theme.meta).foregroundStyle(.secondary)
                        .lineLimit(1).truncationMode(.middle).textSelection(.enabled)
                    Button(L.showInFinder) {
                        NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: o.log)])
                    }
                    .buttonStyle(.link).font(Theme.meta)
                }
                Spacer()
                if store.canUndo {
                    if store.applying { ProgressView().controlSize(.small) }
                    Button(L.undoCleanup) { Task { await store.undoLastApply() } }
                        .help(L.undoHelp)
                        .disabled(store.applying)
                }
                Button(L.ok) { store.outcome = nil; dismiss() }.keyboardShortcut(.defaultAction)
            }
        }
        .padding(20)
        .frame(width: 600)
    }

    private func title(_ o: ApplyOutcome) -> String {
        if let u = store.undoOutcome {
            return u.notRestored == 0 ? L.restoredCount(u.restored) : L.restoredSkipped(u.restored, u.notRestored)
        }
        let skipped = o.results.filter { !$0.done }.count
        return skipped == 0 && o.errors.isEmpty ? L.deletedCount(o.deleted) : L.deletedSkipped(o.deleted, skipped)
    }

    private static var undoMarks: [String] { [L.undoRestored, L.undoExists, L.undoLost, L.undoFailed] }

    private func mark(_ s: UndoOutcome.Entry.Status) -> String {
        switch s {
        case .ready, .restored: return L.undoRestored
        case .exists: return L.undoExists
        case .lost: return L.undoLost
        case .failed: return L.undoFailed
        }
    }

    /// 恢复了用墨色（回到原样，不是核销）；跳过的淡一些；恢复不了、失败的用存疑赭。
    private func color(_ s: UndoOutcome.Entry.Status) -> AnyShapeStyle {
        switch s {
        case .ready, .restored: return AnyShapeStyle(.primary)
        case .exists: return AnyShapeStyle(.secondary)
        case .lost, .failed: return AnyShapeStyle(Theme.held)
        }
    }

    private func row(_ text: String, color: AnyShapeStyle, marks: [String], kind: Item.Kind, name: String, message: String) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            // 各种结论叠在一起占位，列宽取最长的那个，换语言也对齐
            ZStack(alignment: .leading) {
                ForEach(marks, id: \.self) { Text($0).hidden() }
                Text(text).foregroundStyle(color)
            }
            .font(Theme.verdict)
            VStack(alignment: .leading, spacing: 2) {
                Text((kind == .worktree ? L.kindWorktree : L.kindBranch) + " " + name).font(Theme.name)
                Text(message).font(Theme.note).foregroundStyle(.secondary)
            }
        }
    }
}
