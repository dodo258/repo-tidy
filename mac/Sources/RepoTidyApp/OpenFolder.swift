import AppKit
import RepoTidyKit
import UniformTypeIdentifiers

/// 在 Finder 里选中这个目录。
@MainActor
func showInFinder(folder: String, _ store: Store) {
    guard store.canOpen(folder: folder) else { return }
    NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: folder)])
}

/// 用系统默认的终端打开这个目录。macOS 没有「默认终端」这个设置，Ghostty、Kaku 这些的「设为默认」
/// 改的是可执行文件的默认打开方式，所以看它；没有就用系统自带的终端。
@MainActor
func openInTerminal(folder: String, _ store: Store) async {
    guard store.canOpen(folder: folder) else { return }
    let workspace = NSWorkspace.shared
    let terminal = workspace.urlForApplication(toOpen: .unixExecutable)
        ?? URL(fileURLWithPath: "/System/Applications/Utilities/Terminal.app")
    do {
        _ = try await workspace.open([URL(fileURLWithPath: folder, isDirectory: true)],
                                     withApplicationAt: terminal, configuration: NSWorkspace.OpenConfiguration())
    } catch {
        store.openError = error.localizedDescription
    }
}
