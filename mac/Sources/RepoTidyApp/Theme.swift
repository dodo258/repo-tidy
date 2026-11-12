import AppKit
import SwiftUI

/// 设计语言「对账单」：
/// 系统原生的窗口、侧栏和列表，个性只放在「核销」这件事上——
/// 可删项的证据里，提交号印成一枚小小的核销印。其余全部安静。
enum Theme {
    // 颜色：一个主导色（系统墨色，跟随明暗），两个强调色只用在结论上。
    /// 核销绿：可删、勾选框、「清理」按钮、核销印。
    static let cleared = Color(nsColor: .adaptive(light: 0x2E7D4F, dark: 0x5BC48B))
    /// 存疑赭：留着、且里面有没合并也没推送的东西（删了会丢）。
    static let held = Color(nsColor: .adaptive(light: 0xA8661B, dark: 0xE2A75E))
    /// 核销印的底色。
    static let stamp = Color(nsColor: .adaptive(light: 0x2E7D4F, dark: 0x5BC48B, alpha: 0.12))

    // 字体：界面用系统字体（苹方/SF Pro）；分支名、路径、提交号是代码里的标识，用 SF Mono。
    static let repoTitle = Font.system(size: 20, weight: .semibold)
    static let name = Font.system(size: 13, design: .monospaced)
    static let body = Font.system(size: 13)
    static let note = Font.system(size: 12)
    static let meta = Font.system(size: 11).monospacedDigit()
    static let verdict = Font.system(size: 11, weight: .semibold)
    static let hash = Font.system(size: 11, weight: .medium, design: .monospaced)

    // 圆角两级：核销印 3，块状提示 6；不加阴影。
    static let stampRadius: CGFloat = 3
    static let blockRadius: CGFloat = 6

    // 间距按 4 的倍数。
    static let gap: CGFloat = 4
    static let pad: CGFloat = 16
}

extension NSColor {
    static func adaptive(light: UInt32, dark: UInt32, alpha: CGFloat = 1) -> NSColor {
        NSColor(name: nil) { appearance in
            let hex = appearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua ? dark : light
            return NSColor(srgbRed: CGFloat((hex >> 16) & 0xFF) / 255, green: CGFloat((hex >> 8) & 0xFF) / 255,
                           blue: CGFloat(hex & 0xFF) / 255, alpha: alpha)
        }
    }
}

/// 「清理」「删除」这类核销动作的按钮：核销绿实底白字。不随窗口是否在前台变灰，禁用时才变淡。
struct ClearedButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) private var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(Theme.body.weight(.medium))
            .foregroundStyle(.white)
            .padding(.horizontal, 12)
            .padding(.vertical, 5)
            .background(RoundedRectangle(cornerRadius: Theme.blockRadius)
                .fill(Theme.cleared.opacity(isEnabled ? (configuration.isPressed ? 0.8 : 1) : 0.35)))
    }
}
