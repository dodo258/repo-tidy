import AppKit
import RepoTidyKit
import SwiftUI

struct RepoTidyApp: App {
    @State private var store: Store

    init() {
        let store = Store()
        _store = State(initialValue: store)
        // 一启动就扫：菜单栏的数字不用等主窗口打开
        Task { @MainActor in store.refresh() }
    }

    var body: some Scene {
        Window("repo-tidy", id: "main") {
            MainView().environment(store)
        }
        .defaultSize(width: 1100, height: 700)
        .commands {
            CommandGroup(after: .newItem) {
                Button("选择要扫描的文件夹…") { chooseFolder(store) }.keyboardShortcut("o")
                Button("重新扫描") { store.refresh() }.keyboardShortcut("r")
                    .disabled(store.scanning || store.applying)
                CleanCommand()
            }
        }

        MenuBarExtra {
            MenuBarPanel().environment(store)
        } label: {
            MenuBarLabel().environment(store)
        }
        .menuBarExtraStyle(.window)

        Settings {
            SettingsView().environment(store)
        }
    }
}

/// 文件菜单里的「清理…」（⌘⌫），打开主窗口的确认弹窗。
struct CleanCommand: View {
    @FocusedValue(\.confirmClean) private var confirm

    var body: some View {
        Button("清理勾选的项…") { confirm?() }
            .keyboardShortcut(.delete, modifiers: .command)
            .disabled(confirm == nil)
    }
}

struct SettingsView: View {
    @Environment(Store.self) private var store

    var body: some View {
        @Bindable var store = store
        Form {
            LabeledContent("扫描的文件夹") {
                HStack {
                    Text(Format.tildePath(store.scanRoot)).font(Theme.name).lineLimit(1).truncationMode(.middle)
                    Button("选择…") { chooseFolder(store) }
                }
            }
            LabeledContent("命令行工具") {
                VStack(alignment: .leading, spacing: 4) {
                    HStack {
                        TextField("自动查找", text: $store.cliPath).font(Theme.name)
                        Button("选择…") { chooseCLI(store) }
                    }
                    Text(verbatim: "留空时依次找 ~/.local/bin、~/go/bin、/opt/homebrew/bin、/usr/local/bin、~/bin")
                        .font(Theme.meta).foregroundStyle(.secondary)
                }
            }
        }
        .padding(20)
        .frame(width: 560)
    }
}
