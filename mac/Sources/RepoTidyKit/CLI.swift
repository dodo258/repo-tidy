import Foundation

public enum CLIError: Error, Equatable, LocalizedError {
    case failed(code: Int32, message: String)
    case timedOut(seconds: Int)
    case badOutput(String)
    case launch(String)

    public var errorDescription: String? {
        switch self {
        case let .failed(code, message):
            return message.isEmpty ? L.cliExitNoMessage(Int(code)) : message
        case let .timedOut(s): return L.cliTimedOut(s)
        case let .badOutput(why): return L.cliBadOutput(why)
        case let .launch(why): return L.cliLaunch(why)
        }
    }
}

/// 扫描结果：有仓库，或这个目录下一个仓库都没有。
public enum ScanResult: Sendable, Equatable {
    case report(Report)
    case noRepos
}

/// 扫描进度：命令行加 `--progress` 时，每开始扫一个仓库往 stderr 写一行「正在扫 3/13 名字」（英文是「Scanning 3/13 名字」）。
public struct ScanProgress: Equatable, Sendable {
    public var current: Int
    public var total: Int
    public var repo: String

    public init(current: Int, total: Int, repo: String) {
        self.current = current
        self.total = total
        self.repo = repo
    }

    /// 认「<一个词> <当前>/<总数> <仓库名>」；不是进度行（比如报错）返回 nil。
    /// 只看数字和名字，不依赖前面那个词。
    public static func parse<S: StringProtocol>(_ line: S) -> ScanProgress? {
        let parts = line.split(separator: " ", maxSplits: 2, omittingEmptySubsequences: false)
        guard parts.count == 3, !parts[0].isEmpty, !parts[2].isEmpty else { return nil }
        let nums = parts[1].split(separator: "/", omittingEmptySubsequences: false)
        guard nums.count == 2, let cur = Int(nums[0]), let total = Int(nums[1]),
              cur >= 1, cur <= total else { return nil }
        return ScanProgress(current: cur, total: total, repo: String(parts[2]))
    }

    /// 和命令行同一句话。
    public var text: String { L.progress(current, total) + " " + repo }
}

/// 调 `repo-tidy` 命令行。所有判定、删除都在命令行里做，这里只传参数和解析输出。
public struct CLI: Sendable {
    public let executable: URL
    /// 传给命令行的 `--lang`：证据、原因、删除结果的说明跟着它变。
    public let lang: Lang

    public init(executable: URL, lang: Lang = .current) {
        self.executable = executable
        self.lang = lang
    }

    /// App 里自带的命令行在包里的位置（`mac/scripts/build-app.sh` 放进去的）。
    public static let bundledSubpath = "Contents/Helpers/repo-tidy"

    /// `bundle` 是 .app 时返回它自带的命令行路径；从 `swift build` 直接跑（不是 .app）时没有。
    public static func bundledPath(in bundle: URL = Bundle.main.bundleURL) -> String? {
        guard bundle.pathExtension == "app" else { return nil }
        return bundle.appendingPathComponent(bundledSubpath).path
    }

    /// 找命令行工具：设置里指定的路径 → 环境变量 REPO_TIDY_BIN → App 自带的 → 常见安装位置和 PATH。
    /// 从 Finder 打开的 App 拿不到终端里的 PATH，所以常见位置要自己列。
    public static func locate(custom: String?, environment: [String: String] = ProcessInfo.processInfo.environment,
                              home: String = NSHomeDirectory(), bundled: String? = bundledPath(),
                              isExecutable: (String) -> Bool = {
                                  FileManager.default.isExecutableFile(atPath: $0)
                              }) -> URL? {
        var candidates: [String] = []
        if let custom, !custom.isEmpty { candidates.append((custom as NSString).expandingTildeInPath) }
        if let env = environment["REPO_TIDY_BIN"], !env.isEmpty { candidates.append(env) }
        if let bundled { candidates.append(bundled) }
        let dirs = ["\(home)/.local/bin", "\(home)/go/bin", "/opt/homebrew/bin", "/usr/local/bin", "\(home)/bin"]
            + (environment["PATH"] ?? "").split(separator: ":").map(String.init)
        candidates += dirs.map { "\($0)/repo-tidy" }
        return candidates.first(where: isExecutable).map { URL(fileURLWithPath: $0) }
    }

    /// 给子进程的环境：PATH 前面补上 Homebrew 的位置，让命令行用到较新的 git（需要 ≥ 2.38）。
    static func childEnvironment(_ env: [String: String] = ProcessInfo.processInfo.environment) -> [String: String] {
        var e = env
        let base = e["PATH"].flatMap { $0.isEmpty ? nil : $0 } ?? "/usr/bin:/bin:/usr/sbin:/sbin"
        e["PATH"] = "/opt/homebrew/bin:/usr/local/bin:" + base
        return e
    }

    /// 只读扫描 `root`。扫到第几个仓库通过 `progress` 报出来（在后台线程调用）。
    public func scan(root: String, depth: Int = 1, online: Bool = false,
                     progress: @escaping @Sendable (ScanProgress) -> Void = { _ in }) async throws -> ScanResult {
        let out = try await run(scanArgs(root: root, depth: depth, online: online), timeout: 600) { line in
            if let p = ScanProgress.parse(line) { progress(p) }
        }
        // 退出码 1、什么都没输出：这个目录下没有 git 仓库（docs/reference.md「JSON 输出」）
        if out.code == 1 && out.stdout.isEmpty {
            return .noRepos
        }
        guard out.code == 0 || out.code == 1 else {
            throw CLIError.failed(code: out.code, message: Self.clean(out.stderr))
        }
        do {
            return .report(try JSON.decoder.decode(Report.self, from: out.stdout))
        } catch {
            throw CLIError.badOutput(String(describing: error))
        }
    }

    /// 只删 `items`（必须是扫描结果里的可删项）。删前命令行会逐项重新判定。
    public func apply(_ items: [Item]) async throws -> ApplyOutcome {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("repo-tidy-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        defer {
            // swiftlint:disable:next silent_try
            try? FileManager.default.removeItem(at: dir) // 临时文件，删不掉无所谓
        }
        let file = dir.appendingPathComponent("select.json")
        try JSONEncoder().encode(Selection(items)).write(to: file)
        return try Self.decodeResult(try await run(applyArgs(select: file.path), timeout: 600))
    }

    /// 按删除时写的恢复日志，把删掉的分支和 worktree 建回原处（已经在的跳过）。
    public func undo(log: String) async throws -> UndoOutcome {
        try Self.decodeResult(try await run(undoArgs(log: log), timeout: 600))
    }

    /// 删除、撤销的 JSON 结果：退出码 1 表示有项没做成，结果照样在标准输出里。
    static func decodeResult<T: Decodable>(_ out: Output) throws -> T {
        guard out.code == 0 || out.code == 1, !out.stdout.isEmpty else {
            throw CLIError.failed(code: out.code, message: Self.clean(out.stderr))
        }
        do {
            return try JSON.decoder.decode(T.self, from: out.stdout)
        } catch {
            throw CLIError.badOutput(String(describing: error))
        }
    }

    func scanArgs(root: String, depth: Int, online: Bool) -> [String] {
        ["--json", "--progress", "--lang", lang.rawValue, "--depth", String(depth)] + (online ? ["--online"] : []) + [root]
    }

    func applyArgs(select: String) -> [String] {
        ["--apply", "--json", "--lang", lang.rawValue, "--select", select]
    }

    func undoArgs(log: String) -> [String] {
        ["--undo", log, "--apply", "--json", "--lang", lang.rawValue]
    }

    public func version() async throws -> String {
        let out = try await run(["--version"], timeout: 10)
        return String(decoding: out.stdout, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines)
    }

    struct Output { var code: Int32; var stdout: Data; var stderr: String }

    /// 跑一次命令行，输出写进临时文件（不会因为管道写满卡住），超时就停掉。
    /// 给了 `onStderrLine` 就边跑边把 stderr 新写的整行交给它（进度用）。
    func run(_ args: [String], timeout: Int, onStderrLine: (@Sendable (String) -> Void)? = nil) async throws -> Output {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("repo-tidy-run-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        defer {
            // swiftlint:disable:next silent_try
            try? FileManager.default.removeItem(at: dir) // 临时文件，删不掉无所谓
        }
        let outURL = dir.appendingPathComponent("out"), errURL = dir.appendingPathComponent("err")
        FileManager.default.createFile(atPath: outURL.path, contents: nil)
        FileManager.default.createFile(atPath: errURL.path, contents: nil)
        let outH = try FileHandle(forWritingTo: outURL), errH = try FileHandle(forWritingTo: errURL)
        defer {
            // swiftlint:disable:next silent_try
            try? outH.close(); try? errH.close() // 只是释放句柄
        }

        let p = Process()
        p.executableURL = executable
        p.arguments = args
        p.environment = Self.childEnvironment()
        p.standardOutput = outH
        p.standardError = errH
        p.standardInput = FileHandle.nullDevice

        let timedOut = Flag()
        let tail = onStderrLine.map { Self.tail(errURL, onLine: $0) }
        defer { tail?.cancel() }
        let code: Int32 = try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { (cont: CheckedContinuation<Int32, Error>) in
                p.terminationHandler = { cont.resume(returning: $0.terminationStatus) }
                do {
                    try p.run()
                } catch {
                    p.terminationHandler = nil
                    cont.resume(throwing: CLIError.launch(error.localizedDescription))
                    return
                }
                let deadline = DispatchTime.now() + .seconds(timeout)
                DispatchQueue.global().asyncAfter(deadline: deadline) { [weak p] in
                    // 先发 SIGINT：命令行收到后取消正在跑的 git，自己退出
                    if let p, p.isRunning { timedOut.set(); p.interrupt() }
                }
            }
        } onCancel: {
            if p.isRunning { p.interrupt() }
        }
        try Task.checkCancellation()
        let stdout = try Data(contentsOf: outURL)
        let stderr = String(decoding: try Data(contentsOf: errURL), as: UTF8.self)
        if timedOut.isSet { throw CLIError.timedOut(seconds: timeout) }
        return Output(code: code, stdout: stdout, stderr: stderr)
    }

    /// 每隔一小会儿读 `url` 里新写的整行交给 `onLine`，直到被取消。只用来显示进度，读不了就下一轮再读。
    static func tail(_ url: URL, onLine: @escaping @Sendable (String) -> Void) -> Task<Void, Never> {
        Task.detached {
            var offset: UInt64 = 0
            var pending = Data()
            while !Task.isCancelled {
                do {
                    try await Task.sleep(for: .milliseconds(150))
                    let h = try FileHandle(forReadingFrom: url)
                    defer { h.closeFile() }
                    try h.seek(toOffset: offset)
                    let d = try h.readToEnd() ?? Data()
                    offset += UInt64(d.count)
                    pending.append(d)
                } catch {
                    continue
                }
                while let nl = pending.firstIndex(of: 0x0A) {
                    onLine(String(decoding: pending[pending.startIndex..<nl], as: UTF8.self))
                    pending.removeSubrange(pending.startIndex...nl)
                }
            }
        }
    }

    /// 命令行的报错去掉「repo-tidy：」（英文是「repo-tidy: 」）前缀和进度行。
    static func clean(_ stderr: String) -> String {
        stderr.split(separator: "\n").filter { ScanProgress.parse($0) == nil }.map { line in
            for prefix in ["repo-tidy：", "repo-tidy:"] where line.hasPrefix(prefix) {
                return String(line.dropFirst(prefix.count)).trimmingCharacters(in: .whitespaces)
            }
            return String(line)
        }.joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines)
    }
}

/// 跨线程的一次性标记。
final class Flag: @unchecked Sendable {
    private let lock = NSLock()
    private var value = false
    func set() { lock.withLock { value = true } }
    var isSet: Bool { lock.withLock { value } }
}
