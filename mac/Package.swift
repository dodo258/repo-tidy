// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "RepoTidy",
    platforms: [.macOS(.v14)],
    targets: [
        // 数据层：解析 repo-tidy --json、调命令行、界面状态。不含 SwiftUI 视图，能单独测试。
        .target(name: "RepoTidyKit"),
        // 界面：主窗口、菜单栏、确认弹窗、设置。
        .executableTarget(name: "RepoTidyApp", dependencies: ["RepoTidyKit"]),
        .testTarget(name: "RepoTidyKitTests", dependencies: ["RepoTidyKit"]),
    ]
)
