import Foundation

// 带 --screenshot 时不进正常界面，只把各个状态渲染成 PNG（给验收和界面门用）。
if CommandLine.arguments.contains("--screenshot") {
    MainActor.assumeIsolated { Screenshots.run() }
} else {
    RepoTidyApp.main()
}
