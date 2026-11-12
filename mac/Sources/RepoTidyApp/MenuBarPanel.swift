import AppKit
import RepoTidyKit
import SwiftUI

/// 菜单栏图标：分支图形 + 可删项总数。
struct MenuBarLabel: View {
    @Environment(Store.self) private var store

    var body: some View {
        let n = store.removableTotal
        HStack(spacing: 2) {
            Image(systemName: "arrow.triangle.branch")
            if store.missingCLI {
                Text("!")
            } else if n > 0 {
                Text("\(n)").monospacedDigit()
            }
        }
        .accessibilityLabel(store.missingCLI ? "repo-tidy：没装命令行工具" : "repo-tidy：可删 \(n) 项")
    }
}

/// 菜单栏点开的面板：总览、最乱的几个仓库、打开主窗口、刷新。
struct MenuBarPanel: View {
    @Environment(Store.self) private var store
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline) {
                Text("repo-tidy").font(Theme.body.weight(.semibold))
                Spacer()
                Text(status).font(Theme.meta).foregroundStyle(.secondary)
            }
            overview
            let messy = store.repos(in: .messy).prefix(6)
            if !messy.isEmpty {
                Divider()
                VStack(alignment: .leading, spacing: 2) {
                    ForEach(Array(messy)) { repo in
                        Button { open(repo.id) } label: {
                            RepoRow(repo: repo).contentShape(Rectangle())
                        }
                        .buttonStyle(PanelRowStyle())
                    }
                }
            }
            Divider()
            HStack {
                Button("打开主窗口") { open(nil) }.keyboardShortcut("o")
                Button("刷新") { store.refresh() }.keyboardShortcut("r").disabled(store.scanning)
                Spacer()
                Button("退出") { NSApp.terminate(nil) }.keyboardShortcut("q")
            }
        }
        .padding(14)
        .frame(width: 320)
    }

    private var status: String {
        if store.scanning { return "扫描中…" }
        if let t = store.lastScan { return "\(Format.clock(t)) 扫描" }
        return ""
    }

    @ViewBuilder
    private var overview: some View {
        switch store.screen {
        case .missingCLI:
            Text("没找到 repo-tidy 命令行工具，打开主窗口看怎么装。").font(Theme.note).foregroundStyle(Theme.held)
        case .scanning:
            Text("正在扫描 \(Format.tildePath(store.scanRoot))…").font(Theme.note).foregroundStyle(.secondary)
        case let .failed(msg):
            Text("扫描没完成：\(msg)").font(Theme.note).foregroundStyle(Theme.held).lineLimit(3)
        case .empty:
            Text("\(Format.tildePath(store.scanRoot)) 下没有 git 仓库").font(Theme.note).foregroundStyle(.secondary)
        case .allClean, .hasRemovable:
            if let s = store.report?.summary {
                VStack(alignment: .leading, spacing: 3) {
                    if s.removable > 0 {
                        Text(s.removableLine)
                            .font(Theme.body.weight(.medium)).foregroundStyle(Theme.cleared)
                    } else {
                        Text("没有可删的项").font(Theme.body.weight(.medium))
                    }
                    Text("\(s.repos) 个仓库 · 留着 \(s.keep) 项" + (s.unmerged > 0 ? "，其中 \(s.unmerged) 项有未合并内容" : ""))
                        .font(Theme.note).foregroundStyle(.secondary)
                }
            }
        }
    }

    private func open(_ repoID: String?) {
        if let repoID { store.selectedRepoID = repoID }
        openWindow(id: "main")
        NSApp.activate(ignoringOtherApps: true)
    }
}

struct PanelRowStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .padding(.horizontal, 6).padding(.vertical, 3)
            .background(RoundedRectangle(cornerRadius: Theme.blockRadius)
                .fill(configuration.isPressed ? Color.primary.opacity(0.1) : .clear))
    }
}
