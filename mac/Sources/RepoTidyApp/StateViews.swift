import AppKit
import RepoTidyKit
import SwiftUI

/// 整窗状态的统一版式：左对齐的一段说明，下面是能做的事。不放大插画。
struct StatePage<Actions: View>: View {
    let title: String
    var path: String? = nil
    var busy = false
    let detail: String
    @ViewBuilder var actions: Actions

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 8) {
                    Text(title).font(Theme.repoTitle)
                    if busy { ProgressView().controlSize(.small) }
                }
                if let path {
                    Text(verbatim: Format.tildePath(path)).font(Theme.name).foregroundStyle(.secondary)
                        .textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
                }
            }
            // 不加 fixedSize：放在分栏右边（全部干净）时，固定高度的长句会把分栏撑高，左栏被顶到标题栏下面
            Text(detail).font(Theme.body).foregroundStyle(.secondary)
            actions
        }
        .frame(maxWidth: 520, alignment: .leading)
        .padding(40)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

struct ScanningView: View {
    let root: String
    let progress: ScanProgress?

    var body: some View {
        StatePage(title: L.scanningTitle, path: root, busy: true, detail: L.scanningDetail) {
            // 不画进度条：命令行几个仓库并行扫，「第几个」是开始扫的顺序，满格时最后几个还没扫完
            if let progress { ScanProgressLabel(progress: progress) }
        }
    }
}

/// 「正在扫 3/13 repo-name」：数字等宽，仓库名用标识的等宽字体，和命令行同一句话。
struct ScanProgressLabel: View {
    let progress: ScanProgress

    var body: some View {
        HStack(spacing: 6) {
            Text(L.progress(progress.current, progress.total)).font(Theme.meta)
            Text(verbatim: progress.repo).font(Theme.name).lineLimit(1).truncationMode(.middle)
        }
        .foregroundStyle(.secondary)
        .accessibilityElement(children: .combine)
    }
}

struct FailedView: View {
    @Environment(Store.self) private var store
    let message: String

    var body: some View {
        StatePage(title: L.failedTitle, detail: L.failedDetail) {
            Text(message)
                .font(Theme.name)
                .textSelection(.enabled)
                .padding(10)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(RoundedRectangle(cornerRadius: Theme.blockRadius).fill(.quaternary.opacity(0.6)))
            HStack {
                Button(L.rescan) { store.refresh() }.keyboardShortcut(.defaultAction)
                Button(L.chooseAnotherFolder) { chooseFolder(store) }
            }
        }
    }
}

struct EmptyFolderView: View {
    @Environment(Store.self) private var store

    var body: some View {
        StatePage(title: L.emptyTitle, path: store.scanRoot, detail: L.emptyDetail) {
            HStack {
                Button(L.chooseAnotherFolder) { chooseFolder(store) }.keyboardShortcut(.defaultAction)
                Button(L.rescan) { store.refresh() }
            }
        }
    }
}

struct AllCleanView: View {
    @Environment(Store.self) private var store

    var body: some View {
        if let s = store.report?.summary, s.removable == 0 {
            StatePage(title: L.allCleanTitle, detail: cleanDetail(s)) {
                Text(L.allCleanHint)
                    .font(Theme.note).foregroundStyle(.tertiary)
            }
        } else {
            StatePage(title: L.pickRepoTitle, detail: L.pickRepoDetail) { EmptyView() }
        }
    }

    private func cleanDetail(_ s: Summary) -> String {
        if s.worktrees + s.branches == 0 { return L.cleanNoItems(s.repos) }
        return L.cleanAllKept(s.repos, s.worktrees, s.branches, s.unmerged)
    }
}

/// App 自带的命令行没了（或不能执行），别处也没找到：说清楚它是什么、怎么补、找过哪里。
struct MissingCLIView: View {
    @Environment(Store.self) private var store
    static let install = "go build -o ~/.local/bin/repo-tidy ./cmd/repo-tidy"

    var body: some View {
        StatePage(title: L.missingTitle, detail: L.missingDetail) {
            VStack(alignment: .leading, spacing: 6) {
                Text(L.missingBuildHint).font(Theme.note)
                HStack(spacing: 8) {
                    Text(Self.install).font(Theme.name).textSelection(.enabled)
                        .padding(.horizontal, 8).padding(.vertical, 6)
                        .background(RoundedRectangle(cornerRadius: Theme.blockRadius).fill(.quaternary.opacity(0.6)))
                    Button(L.copy) {
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(Self.install, forType: .string)
                    }
                }
                Text(L.missingSearched)
                    .font(Theme.meta).foregroundStyle(.tertiary)
            }
            HStack {
                Button(L.searchAgain) { store.refresh() }.keyboardShortcut(.defaultAction)
                Button(L.chooseCLIFile) { chooseCLI(store) }
            }
        }
    }
}

@MainActor
func chooseCLI(_ store: Store) {
    let panel = NSOpenPanel()
    panel.canChooseFiles = true
    panel.canChooseDirectories = false
    panel.prompt = L.cliPanelPrompt
    panel.message = L.cliPanelMessage
    guard panel.runModal() == .OK, let url = panel.url else { return }
    store.cliPath = url.path
    store.refresh()
}
