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
            CommandGroup(after: .textEditing) { FindCommand() }
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

/// 编辑菜单里的「筛选 worktree 和分支」（⌘F），把光标放进右栏的搜索框。
struct FindCommand: View {
    @FocusedValue(\.findItems) private var find

    var body: some View {
        Button(L.menuFind) { find?() }
            .keyboardShortcut("f")
            .disabled(find == nil)
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
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            LabeledContent(L.settingsExclude) {
                VStack(alignment: .leading, spacing: 4) {
                    if store.excludePaths.isEmpty {
                        Text(L.excludeNone).font(Theme.body).foregroundStyle(.secondary)
                    }
                    ForEach(store.excludePaths, id: \.self) { path in
                        HStack(spacing: 6) {
                            Text(verbatim: Format.tildePath(path)).font(Theme.name)
                                .lineLimit(1).truncationMode(.middle).textSelection(.enabled)
                            Spacer(minLength: 4)
                            Button { store.removeExclude(path) } label: {
                                Label(L.excludeRemoveHelp, systemImage: "minus.circle").labelStyle(.iconOnly)
                            }
                            .buttonStyle(.borderless).help(L.excludeRemoveHelp)
                        }
                    }
                    Button(L.excludeAdd) { chooseExcludes(store) }
                    Text(L.excludeHint)
                        .font(Theme.meta).foregroundStyle(.secondary)
                }
            }
            LabeledContent(L.settingsCLI) {
                VStack(alignment: .leading, spacing: 4) {
                    HStack {
                        // Form 会把 TextField 的标题画成行标题，所以标题藏起来，灰字走 prompt 画在框里
                        TextField(L.settingsCLI, text: $store.cliPath, prompt: Text(L.cliPlaceholder))
                            .labelsHidden().font(Theme.name)
                        Button(L.choose) { chooseCLI(store) }
                    }
                    Text(L.cliHint)
                        .font(Theme.meta).foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
        }
        .padding(20)
        .frame(width: 560)
        .onChange(of: store.scanDepth) { store.rescanForNewSettings() }
    }
}

/// 选几个要排除的文件夹，加进设置并重新扫描。
@MainActor
func chooseExcludes(_ store: Store) {
    let panel = NSOpenPanel()
    panel.canChooseDirectories = true
    panel.canChooseFiles = false
    panel.allowsMultipleSelection = true
    panel.prompt = L.excludePanelPrompt
    panel.message = L.excludePanelMessage
    panel.directoryURL = URL(fileURLWithPath: (store.scanRoot as NSString).expandingTildeInPath)
    guard panel.runModal() == .OK else { return }
    store.addExcludes(panel.urls.map(\.path))
}
