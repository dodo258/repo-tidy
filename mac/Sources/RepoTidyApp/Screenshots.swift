import AppKit
import RepoTidyKit
import SwiftUI

/// 截图模式：`RepoTidy --screenshot <输出目录> --cli <repo-tidy> --messy <目录> --clean <目录> --empty <目录> [--home <目录>]`。
/// `--home` 让截图里的路径按这个目录缩写成 `~`，不露出本机的临时目录。
/// 用真的命令行扫临时目录里造的仓库（scripts/demo-repos.sh），把各状态的窗口画到屏幕外再存成 PNG，
/// 不在屏幕上弹窗口，也不碰 ~/projects。「有可删项」那一组会真的删一次勾选的项，截删完的结果。
@MainActor
enum Screenshots {
    static func run() {
        let app = NSApplication.shared
        app.setActivationPolicy(.accessory)
        Task { @MainActor in
            do {
                try await shootAll(Args(CommandLine.arguments))
                exit(0)
            } catch {
                FileHandle.standardError.write(Data("截图失败：\(error)\n".utf8))
                exit(1)
            }
        }
        app.run()
    }

    struct Args {
        var out = "", cli = "", messy = "", clean = "", empty = "", home = ""

        init(_ argv: [String]) {
            var it = argv.dropFirst().makeIterator()
            while let a = it.next() {
                switch a {
                case "--screenshot": out = it.next() ?? ""
                case "--cli": cli = it.next() ?? ""
                case "--messy": messy = it.next() ?? ""
                case "--clean": clean = it.next() ?? ""
                case "--empty": empty = it.next() ?? ""
                case "--home": home = it.next() ?? ""
                default: break
                }
            }
        }
    }

    struct Failure: Error, CustomStringConvertible { let description: String }

    static func shootAll(_ a: Args) async throws {
        guard !a.out.isEmpty, !a.cli.isEmpty, !a.messy.isEmpty, !a.clean.isEmpty, !a.empty.isEmpty else {
            throw Failure(description: "参数不全：--screenshot 目录 --cli 路径 --messy 目录 --clean 目录 --empty 目录")
        }
        if !a.home.isEmpty { Format.home = a.home }
        let out = URL(fileURLWithPath: a.out)
        try FileManager.default.createDirectory(at: out, withIntermediateDirectories: true)
        let cli = CLI(executable: URL(fileURLWithPath: a.cli))
        let main = CGSize(width: 1100, height: 700)

        // 1. 空状态：文件夹下没有仓库
        let empty = try await scanned(cli, root: a.empty)
        try await shoot(MainView().environment(empty), main, "1-empty", out)

        // 2. 扫描中（扫描永远不结束的假后端，画面和真扫描时一样）
        let scanning = store(root: a.messy) { _ in Hanging() }
        scanning.refresh()
        try await shoot(MainView().environment(scanning), main, "2-scanning", out)

        // 3. 有可删项：选最乱的仓库，勾上它可删的项
        let messy = try await scanned(cli, root: a.messy)
        guard let repo = messy.repos(in: .messy).first(where: { $0.removableCount > 0 }) else {
            throw Failure(description: "\(a.messy) 里没有可删项，造仓库的脚本不对")
        }
        messy.selectedRepoID = repo.id
        messy.checkAll(in: repo)
        try await shoot(MainView().environment(messy), main, "3-removable", out)
        try await shoot(MainView().environment(messy), main, "3-removable-dark", out, dark: true)
        try await shoot(ConfirmSheet().environment(messy), CGSize(width: 600, height: 480), "3-confirm", out, chrome: false)
        try await shoot(MenuBarPanel().environment(messy), CGSize(width: 320, height: 250), "3-menubar", out, chrome: false)
        // 真删一次勾选的项，截结果
        await messy.applyChecked()
        try await waitIdle(messy)
        guard let outcome = messy.outcome else {
            throw Failure(description: "删除没有返回结果：\(messy.applyError ?? "")")
        }
        try await shoot(OutcomeSheet(outcome: ApplyOutcomeBox(outcome: outcome)).environment(messy),
                        CGSize(width: 600, height: 360), "3-outcome", out, chrome: false)
        messy.outcome = nil
        try await shoot(MainView().environment(messy), main, "3-after-clean", out)

        // 4. 全部干净
        let clean = try await scanned(cli, root: a.clean)
        try await shoot(MainView().environment(clean), main, "4-all-clean", out)
        try await shoot(MenuBarPanel().environment(clean), CGSize(width: 320, height: 230), "4-menubar", out, chrome: false)

        // 5. 出错：命令行报错（路径不存在）
        let failed = try await scanned(cli, root: a.empty + "/不存在的文件夹")
        try await shoot(MainView().environment(failed), main, "5-failed", out)

        // 6. 没装命令行工具
        let missing = store(root: a.messy) { _ in nil }
        missing.refresh()
        try await shoot(MainView().environment(missing), main, "6-missing-cli", out)
    }

    /// 每个场景一套独立的设置，不读写用户自己的。
    static func store(root: String, backend: @escaping @MainActor (Store) -> Backend?) -> Store {
        let suite = "repo-tidy-screenshot-\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suite) ?? .standard
        defaults.removePersistentDomain(forName: suite)
        let s = Store(defaults: defaults, makeBackend: backend)
        s.scanRoot = root
        return s
    }

    static func scanned(_ cli: CLI, root: String) async throws -> Store {
        let s = store(root: root) { _ in cli }
        s.refresh()
        try await waitIdle(s)
        return s
    }

    static func waitIdle(_ s: Store) async throws {
        for _ in 0..<1200 where s.scanning || s.applying {
            try await Task.sleep(for: .milliseconds(100))
        }
        if s.scanning || s.applying { throw Failure(description: "等了两分钟还没扫完") }
    }

    /// 在屏幕外开一个真窗口画好后存成 PNG。`chrome` 为真时带标题栏和工具栏（主窗口），
    /// 否则只画内容（弹窗、菜单栏面板，它们本来就没有标题栏）。
    static func shoot<V: View>(_ view: V, _ size: CGSize, _ name: String, _ dir: URL,
                               dark: Bool = false, chrome: Bool = true) async throws {
        let controller = NSHostingController(rootView: view.background(Color(nsColor: .windowBackgroundColor)))
        let win = OffscreenWindow(contentViewController: controller)
        if chrome {
            controller.sceneBridgingOptions = [.toolbars, .title]
            win.styleMask = [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView]
            win.toolbarStyle = .unified
            win.title = "repo-tidy"
        } else {
            win.styleMask = [.borderless]
        }
        win.appearance = NSAppearance(named: dark ? .darkAqua : .aqua)
        win.setContentSize(size)
        win.setFrameOrigin(NSPoint(x: -20000, y: -20000))
        win.orderFrontRegardless()
        defer { win.orderOut(nil) }
        try await Task.sleep(for: .milliseconds(1200))
        guard let frameView = chrome ? win.contentView?.superview : win.contentView else { throw Failure(description: "\(name)：没有窗口视图") }
        let bounds = frameView.bounds
        guard let rep = frameView.bitmapImageRepForCachingDisplay(in: bounds) else {
            throw Failure(description: "\(name)：建不了位图")
        }
        frameView.cacheDisplay(in: bounds, to: rep)
        guard let png = rep.representation(using: .png, properties: [:]) else {
            throw Failure(description: "\(name)：转不了 PNG")
        }
        try png.write(to: dir.appendingPathComponent(name + ".png"))
        print(dir.appendingPathComponent(name + ".png").path)
    }
}

/// 允许放在屏幕外，不被系统拉回可见区域。
final class OffscreenWindow: NSWindow {
    override func constrainFrameRect(_ frameRect: NSRect, to screen: NSScreen?) -> NSRect { frameRect }
}

/// 永远扫不完的后端，只给「扫描中」截图用。
struct Hanging: Backend {
    func scan(root: String, online: Bool) async throws -> ScanResult {
        try await Task.sleep(for: .seconds(3600))
        throw CancellationError()
    }

    func apply(_ items: [Item]) async throws -> ApplyOutcome { throw CancellationError() }
}
