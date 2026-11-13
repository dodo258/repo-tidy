import AppKit
import RepoTidyKit
import SwiftUI

/// 主窗口：没数据时整窗显示状态（没装命令行、扫描中、出错、没有仓库），有数据时左右分栏。
struct MainView: View {
    @Environment(Store.self) private var store
    @State private var confirming = false

    var body: some View {
        Group {
            switch store.screen {
            case .missingCLI: MissingCLIView()
            case .scanning: ScanningView(root: store.scanRoot, progress: store.progress)
            case let .failed(msg): FailedView(message: msg)
            case .empty: EmptyFolderView()
            case .allClean, .hasRemovable: split
            }
        }
        .frame(minWidth: 820, minHeight: 520)
        .toolbar { toolbar }
        .sheet(isPresented: $confirming) { ConfirmSheet() }
        .sheet(item: outcomeBinding) { OutcomeSheet(outcome: $0) }
        .alert(L.applyFailedTitle, isPresented: applyErrorShown) {
            Button(L.ok) { store.applyError = nil }
        } message: {
            Text(store.applyError ?? "")
        }
        .focusedSceneValue(\.confirmClean, store.checked.isEmpty ? nil : { confirming = true })
    }

    private var split: some View {
        NavigationSplitView {
            Sidebar()
                .navigationSplitViewColumnWidth(min: 240, ideal: 280, max: 360)
        } detail: {
            if let repo = store.selectedRepo {
                RepoDetail(repo: repo, onClean: { confirming = true })
            } else {
                AllCleanView()
            }
        }
    }

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        ToolbarItem(placement: .primaryAction) {
            Button {
                chooseFolder(store)
            } label: {
                Label(Format.tildePath(store.scanRoot), systemImage: "folder")
                    .labelStyle(.titleAndIcon)
                    .lineLimit(1).truncationMode(.head)
                    .frame(maxWidth: 320)
            }
            .help(L.folderHelp(store.scanRoot))
        }
        ToolbarItem(placement: .primaryAction) {
            if store.scanning || store.applying {
                HStack(spacing: 8) {
                    // 已有结果、重新扫描时，进度写在转圈旁边（第一次扫描整页都在说这件事，不重复）
                    if store.scanning, store.screen != .scanning, let p = store.progress {
                        ScanProgressLabel(progress: p).padding(.leading, 8)
                    }
                    ProgressView().controlSize(.small)
                }
            } else {
                Button { store.refresh() } label: { Label(L.rescan, systemImage: "arrow.clockwise") }
                    .help(L.rescanHelp)
            }
        }
    }

    private var outcomeBinding: Binding<ApplyOutcomeBox?> {
        Binding(get: { store.outcome.map(ApplyOutcomeBox.init) }, set: { if $0 == nil { store.outcome = nil } })
    }

    private var applyErrorShown: Binding<Bool> {
        Binding(get: { store.applyError != nil }, set: { if !$0 { store.applyError = nil } })
    }
}

struct ApplyOutcomeBox: Identifiable {
    let outcome: ApplyOutcome
    var id: String { outcome.log + "\(outcome.results.count)" }
}

/// 选一个要扫描的文件夹，选完马上扫。
@MainActor
func chooseFolder(_ store: Store) {
    let panel = NSOpenPanel()
    panel.canChooseDirectories = true
    panel.canChooseFiles = false
    panel.allowsMultipleSelection = false
    panel.prompt = L.folderPanelPrompt
    panel.message = L.folderPanelMessage
    panel.directoryURL = URL(fileURLWithPath: (store.scanRoot as NSString).expandingTildeInPath)
    guard panel.runModal() == .OK, let url = panel.url else { return }
    store.scanRoot = url.path
    store.selectedRepoID = nil
    store.refresh()
}

struct ConfirmCleanKey: FocusedValueKey {
    typealias Value = () -> Void
}

extension FocusedValues {
    var confirmClean: (() -> Void)? {
        get { self[ConfirmCleanKey.self] }
        set { self[ConfirmCleanKey.self] = newValue }
    }
}
