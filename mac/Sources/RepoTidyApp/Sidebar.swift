import RepoTidyKit
import SwiftUI

/// 左栏：仓库按乱的程度排（命令行排好的顺序），分「要整理」「没扫成」「干净」三组；底部是总览。
struct Sidebar: View {
    @Environment(Store.self) private var store

    var body: some View {
        @Bindable var store = store
        List(selection: $store.selectedRepoID) {
            section(L.groupMessy, .messy)
            section(L.groupFailed, .failed)
            section(L.groupClean, .clean)
        }
        .listStyle(.sidebar)
        .safeAreaInset(edge: .bottom, spacing: 0) { overview }
    }

    @ViewBuilder
    private func section(_ title: String, _ group: Repo.Group) -> some View {
        let repos = store.repos(in: group)
        if !repos.isEmpty {
            Section(title) {
                ForEach(repos) { RepoRow(repo: $0).tag($0.id) }
            }
        }
    }

    @ViewBuilder
    private var overview: some View {
        if let s = store.report?.summary {
            VStack(alignment: .leading, spacing: Theme.gap) {
                Divider()
                Text(L.overviewCounts(s.repos, s.worktrees, s.branches))
                if s.removable > 0 {
                    Text(s.removableLine)
                        .foregroundStyle(Theme.cleared)
                } else {
                    Text(L.nothingRemovable)
                }
                if let t = store.lastScan {
                    Text(L.scannedAtReadOnly(Format.clock(t))).foregroundStyle(.tertiary)
                }
            }
            .font(Theme.meta)
            .foregroundStyle(.secondary)
            .padding(.horizontal, 12)
            .padding(.bottom, 10)
        }
    }
}

struct RepoRow: View {
    let repo: Repo

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            VStack(alignment: .leading, spacing: 2) {
                Text(repo.name).font(Theme.body.weight(.medium)).lineLimit(1)
                Text(subtitle).font(Theme.meta).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer(minLength: 4)
            counts
        }
        .padding(.vertical, 2)
    }

    private var subtitle: String {
        if !repo.error.isEmpty { return L.repoScanFailed }
        if !repo.note.isEmpty { return repo.note }
        return Format.tildePath((repo.root as NSString).deletingLastPathComponent)
    }

    @ViewBuilder
    private var counts: some View {
        if repo.group != .failed, !repo.items.isEmpty {
            HStack(spacing: 6) {
                if repo.removableCount > 0 {
                    Text(L.removableCount(repo.removableCount)).foregroundStyle(Theme.cleared)
                }
                if repo.keepCount > 0 {
                    Text(L.keepCount(repo.keepCount))
                        .foregroundStyle(repo.unmergedCount > 0 ? AnyShapeStyle(Theme.held) : AnyShapeStyle(.secondary))
                }
            }
            .font(Theme.meta)
            .help(repo.unmergedCount > 0 ? L.unmergedHelp(repo.unmergedCount) : "")
        }
    }
}
