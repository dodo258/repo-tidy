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
            Text(detail).font(Theme.body).foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            actions
        }
        .frame(maxWidth: 520, alignment: .leading)
        .padding(40)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

struct ScanningView: View {
    let root: String

    var body: some View {
        StatePage(title: "正在扫描", path: root, busy: true,
                  detail: "每个仓库要跑几十次 git 来比对提交，仓库多的话要等十几秒。只读：不 fetch、不改任何文件。") {
            EmptyView()
        }
    }
}

struct FailedView: View {
    @Environment(Store.self) private var store
    let message: String

    var body: some View {
        StatePage(title: "扫描没完成", detail: "repo-tidy 报了下面的错，什么都没删。") {
            Text(message)
                .font(Theme.name)
                .textSelection(.enabled)
                .padding(10)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(RoundedRectangle(cornerRadius: Theme.blockRadius).fill(.quaternary.opacity(0.6)))
            HStack {
                Button("重新扫描") { store.refresh() }.keyboardShortcut(.defaultAction)
                Button("换个文件夹…") { chooseFolder(store) }
            }
        }
    }
}

struct EmptyFolderView: View {
    @Environment(Store.self) private var store

    var body: some View {
        StatePage(title: "这个文件夹下没有 git 仓库", path: store.scanRoot,
                  detail: "repo-tidy 扫你选的文件夹本身和它下面一层的文件夹。换成放仓库的那一层，比如 ~/projects。") {
            HStack {
                Button("换个文件夹…") { chooseFolder(store) }.keyboardShortcut(.defaultAction)
                Button("重新扫描") { store.refresh() }
            }
        }
    }
}

struct AllCleanView: View {
    @Environment(Store.self) private var store

    var body: some View {
        if let s = store.report?.summary, s.removable == 0 {
            StatePage(title: "没有能删的",
                      detail: cleanDetail(s)) {
                Text("左边选一个仓库，能看到每一项为什么留着。")
                    .font(Theme.note).foregroundStyle(.tertiary)
            }
        } else {
            StatePage(title: "选一个仓库", detail: "左边的仓库按乱的程度排，最乱的在最上面。") { EmptyView() }
        }
    }

    private func cleanDetail(_ s: Summary) -> String {
        var t = "扫了 \(s.repos) 个仓库"
        if s.worktrees + s.branches == 0 {
            return t + "，除了默认分支，没有附加 worktree，也没有别的本地分支。"
        }
        t += "，\(s.worktrees) 个 worktree、\(s.branches) 个分支都得留着"
        if s.unmerged > 0 { t += "，其中 \(s.unmerged) 项有没合并也没推送的内容" }
        return t + "。"
    }
}

/// 没装命令行工具：说清楚它是什么、怎么装、找过哪里。
struct MissingCLIView: View {
    @Environment(Store.self) private var store
    static let install = "go build -o ~/.local/bin/repo-tidy ./cmd/repo-tidy"

    var body: some View {
        StatePage(title: "没找到 repo-tidy 命令行工具",
                  detail: "这个 App 只是界面：判断哪些能删、真正去删，都由命令行工具 repo-tidy 来做。先把它装上。") {
            VStack(alignment: .leading, spacing: 6) {
                Text("在 repo-tidy 源码目录里运行（要 Go 1.26 和 git 2.38 以上）：").font(Theme.note)
                HStack(spacing: 8) {
                    Text(Self.install).font(Theme.name).textSelection(.enabled)
                        .padding(.horizontal, 8).padding(.vertical, 6)
                        .background(RoundedRectangle(cornerRadius: Theme.blockRadius).fill(.quaternary.opacity(0.6)))
                    Button("拷贝") {
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(Self.install, forType: .string)
                    }
                }
                Text(verbatim: "找过这些位置：~/.local/bin、~/go/bin、/opt/homebrew/bin、/usr/local/bin、~/bin")
                    .font(Theme.meta).foregroundStyle(.tertiary)
            }
            HStack {
                Button("重新查找") { store.refresh() }.keyboardShortcut(.defaultAction)
                Button("装在别处，选择文件…") { chooseCLI(store) }
            }
        }
    }
}

@MainActor
func chooseCLI(_ store: Store) {
    let panel = NSOpenPanel()
    panel.canChooseFiles = true
    panel.canChooseDirectories = false
    panel.prompt = "用这个"
    panel.message = "选 repo-tidy 可执行文件"
    guard panel.runModal() == .OK, let url = panel.url else { return }
    store.cliPath = url.path
    store.refresh()
}
