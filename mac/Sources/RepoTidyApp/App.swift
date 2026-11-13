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
                Button(L.menuChooseFolder) { chooseFolder(store) }.keyboardShortcut("o")
                Button(L.rescan) { store.refresh() }.keyboardShortcut("r")
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
        Button(L.menuCleanChecked) { confirm?() }
            .keyboardShortcut(.delete, modifiers: .command)
            .disabled(confirm == nil)
    }
}

struct SettingsView: View {
    @Environment(Store.self) private var store

    var body: some View {
        @Bindable var store = store
        Form {
            LabeledContent(L.settingsFolder) {
                HStack {
                    Text(Format.tildePath(store.scanRoot)).font(Theme.name).lineLimit(1).truncationMode(.middle)
                    Button(L.choose) { chooseFolder(store) }
                }
            }
            LabeledContent(L.settingsDepth) {
                VStack(alignment: .leading, spacing: 4) {
                    Picker(L.settingsDepth, selection: $store.scanDepth) {
                        ForEach(Array(Store.depthRange), id: \.self) { Text("\($0)").tag($0) }
                    }
                    .pickerStyle(.segmented).labelsHidden().fixedSize()
                    Text(L.depthHint)
                        .font(Theme.meta).foregroundStyle(.secondary)
                }
            }
            LabeledContent(L.settingsCLI) {
                VStack(alignment: .leading, spacing: 4) {
                    HStack {
                        TextField(L.cliPlaceholder, text: $store.cliPath).font(Theme.name)
                        Button(L.choose) { chooseCLI(store) }
                    }
                    Text(L.cliHint)
                        .font(Theme.meta).foregroundStyle(.secondary)
                }
            }
        }
        .padding(20)
        .frame(width: 560)
        .onChange(of: store.scanDepth) { store.refresh() }
    }
}
