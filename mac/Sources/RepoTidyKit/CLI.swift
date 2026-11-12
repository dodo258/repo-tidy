import Foundation

public enum CLIError: Error, Equatable, LocalizedError {
    case failed(code: Int32, message: String)
    case timedOut(seconds: Int)
    case badOutput(String)
    case launch(String)

    public var errorDescription: String? {
        switch self {
        case let .failed(code, message):
            return message.isEmpty ? "repo-tidy 退出码 \(code)，没有说明" : message
        case let .timedOut(s): return "repo-tidy 跑了 \(s) 秒还没结束，已停止"
        case let .badOutput(why): return "看不懂 repo-tidy 的输出：\(why)"
        case let .launch(why): return "启动 repo-tidy 失败：\(why)"
        }
    }
}

/// 扫描结果：有仓库，或这个目录下一个仓库都没有。
public enum ScanResult: Sendable, Equatable {
    case report(Report)
    case noRepos
}

/// 调 `repo-tidy` 命令行。所有判定、删除都在命令行里做，这里只传参数和解析输出。
public struct CLI: Sendable {
    public let executable: URL

    public init(executable: URL) { self.executable = executable }

    /// 找命令行工具：设置里指定的路径 → 环境变量 REPO_TIDY_BIN → 常见安装位置和 PATH。
    /// 从 Finder 打开的 App 拿不到终端里的 PATH，所以常见位置要自己列。
    public static func locate(custom: String?, environment: [String: String] = ProcessInfo.processInfo.environment,
                              home: String = NSHomeDirectory(), isExecutable: (String) -> Bool = {
                                  FileManager.default.isExecutableFile(atPath: $0)
                              }) -> URL? {
        var candidates: [String] = []
        if let custom, !custom.isEmpty { candidates.append((custom as NSString).expandingTildeInPath) }
        if let env = environment["REPO_TIDY_BIN"], !env.isEmpty { candidates.append(env) }
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

    /// 只读扫描 `root`。
    public func scan(root: String, online: Bool = false) async throws -> ScanResult {
        var args = ["--json"]
        if online { args.append("--online") }
        args.append(root)
        let out = try await run(args, timeout: 600)
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
        let out = try await run(["--apply", "--json", "--select", file.path], timeout: 600)
        guard out.code == 0 || out.code == 1, !out.stdout.isEmpty else {
            throw CLIError.failed(code: out.code, message: Self.clean(out.stderr))
        }
        do {
            return try JSON.decoder.decode(ApplyOutcome.self, from: out.stdout)
        } catch {
            throw CLIError.badOutput(String(describing: error))
        }
    }

    public func version() async throws -> String {
        let out = try await run(["--version"], timeout: 10)
        return String(decoding: out.stdout, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines)
    }

    struct Output { var code: Int32; var stdout: Data; var stderr: String }

    /// 跑一次命令行，输出写进临时文件（不会因为管道写满卡住），超时就停掉。
    func run(_ args: [String], timeout: Int) async throws -> Output {
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

    /// 命令行的报错去掉「repo-tidy：」前缀。
    static func clean(_ stderr: String) -> String {
        stderr.split(separator: "\n").map {
            $0.hasPrefix("repo-tidy：") ? String($0.dropFirst("repo-tidy：".count)).trimmingCharacters(in: .whitespaces) : String($0)
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
