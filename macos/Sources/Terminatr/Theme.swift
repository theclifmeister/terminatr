import AppKit
import CoreText
import SwiftUI
import TerminatrKit

/// The app's look as named tokens. Views read only these, never a
/// colour of their own, so another concept (design T744's Afterglow,
/// Signal, Prism) is one more Theme value. Each colour has a light and
/// a dark value and follows the system appearance.
struct Theme: Sendable {
    /// The backdrop's mesh: four corners, then the centre.
    var mesh: [Color]
    /// Under the mesh, and alone when Reduce Transparency is on.
    var base: Color

    var ink: Color
    var muted: Color
    var line: Color
    /// Actions and the selection (Bloom's ink violet).
    var action: Color
    var onAction: Color
    var good: Color
    var idle: Color
    /// Something that needs the user.
    var attention: Color
    var onAttention: Color
    var question: Color
    var approval: Color

    /// Frost tints over the system material: the sidebar and the cards.
    var sidebarTint: Color
    var cardTint: Color
    var edge: Color
    var meter: [Color]

    /// The user's prompts in the conversation: a gradient, and its ink.
    var prompt: [Color]
    var onPrompt: Color
    /// The agent's text cards.
    var agentCard: Color
    /// The question and approval cards' wash, over the frost.
    var questionTint: Color
    var approvalTint: Color
    /// A command or a path set in mono.
    var command: Color
    /// The terminal tab.
    var terminalBackground: Color
    var terminalForeground: Color

    var titleFamily: String
    var monoFamily: String

    func title(_ size: CGFloat, _ weight: Font.Weight = .bold) -> Font {
        .custom(titleFamily, size: size).weight(weight)
    }

    func mono(_ size: CGFloat, _ weight: Font.Weight = .regular) -> Font {
        .custom(monoFamily, size: size).weight(weight)
    }
}

extension Theme {
    /// Bloom (design T744, concept 02): a vivid gradient mesh under
    /// frosted panels. Contrast measured on the design page: body on
    /// frost 12.9:1, muted 5.8:1, white on violet 6.3:1; dark 13.4 / 8.6:1.
    static let bloom = Theme(
        mesh: [
            .dynamic(0xff6b4a, 0xc2365a), // coral
            .dynamic(0x7b5cff, 0x4b2fb8), // violet
            .dynamic(0xc6f432, 0x5b7a10), // lime
            .dynamic(0x29d3c3, 0x0f6e6a), // lagoon
            .dynamic(0xff5fb7, 0x8c1f6e), // bubblegum
        ],
        base: .dynamic(0xf3d7ff, 0x160f2c),
        ink: .dynamic(0x1c1433, 0xf6f2ff),
        muted: .dynamic(0x4f4670, 0xc9c0ea),
        line: .dynamic(0x1c1433, 0xf6f2ff, alpha: 0.12),
        action: .dynamic(0x5a3cf0, 0xa58fff),
        onAction: .dynamic(0xffffff, 0x160f33),
        good: .dynamic(0x0e9f6e, 0x5fe0a8),
        idle: .dynamic(0xa49cc0, 0x6c6390),
        attention: .dynamic(0xd83a22, 0xff8a73),
        onAttention: .dynamic(0xffffff, 0x2a0c06),
        question: .dynamic(0x5a3cf0, 0xb9a8ff),
        approval: .dynamic(0xe2541c, 0xff9a6b),
        sidebarTint: .dynamic(0xffffff, 0x000000, alpha: 0.30, darkAlpha: 0.18),
        cardTint: .dynamic(0xffffff, 0xffffff, alpha: 0.80, darkAlpha: 0.07),
        edge: .dynamic(0xffffff, 0xffffff, alpha: 0.85, darkAlpha: 0.14),
        meter: [.dynamic(0x5a3cf0, 0xa58fff), Color(hex: 0xff5fb7), Color(hex: 0xff6b4a)],
        prompt: [Color(hex: 0x5a3cf0), .dynamic(0x9b3cf0, 0x8a2fd6)],
        onPrompt: Color(hex: 0xffffff),
        agentCard: .dynamic(0xffffff, 0xffffff, alpha: 0.80, darkAlpha: 0.07),
        questionTint: .dynamic(0xefeaff, 0x5a3cf0, alpha: 0.95, darkAlpha: 0.28),
        approvalTint: .dynamic(0xfff1ec, 0xff6a4a, alpha: 1, darkAlpha: 0.22),
        command: .dynamic(0x1c1433, 0x000000, alpha: 0.06, darkAlpha: 0.3),
        terminalBackground: Color(hex: 0x1a1430),
        terminalForeground: Color(hex: 0xefeaff),
        titleFamily: "Bricolage Grotesque",
        monoFamily: "DM Mono"
    )
}

private struct ThemeKey: EnvironmentKey {
    static let defaultValue = Theme.bloom
}

extension EnvironmentValues {
    var theme: Theme {
        get { self[ThemeKey.self] }
        set { self[ThemeKey.self] = newValue }
    }
}

extension Color {
    init(hex: UInt32, alpha: Double = 1) {
        self.init(nsColor: NSColor(hex: hex, alpha: alpha))
    }

    /// A colour with a light and a dark value, swapped with the
    /// appearance the view is drawn in.
    static func dynamic(_ light: UInt32, _ dark: UInt32, alpha: Double = 1, darkAlpha: Double? = nil) -> Color {
        Color(nsColor: NSColor(name: nil) { appearance in
            appearance.bestMatch(from: [.aqua, .darkAqua]) == .darkAqua
                ? NSColor(hex: dark, alpha: darkAlpha ?? alpha)
                : NSColor(hex: light, alpha: alpha)
        })
    }
}

extension NSColor {
    convenience init(hex: UInt32, alpha: Double) {
        self.init(
            srgbRed: CGFloat((hex >> 16) & 0xff) / 255,
            green: CGFloat((hex >> 8) & 0xff) / 255,
            blue: CGFloat(hex & 0xff) / 255,
            alpha: alpha
        )
    }
}

enum Fonts {
    /// Registers the bundled fonts (Bricolage Grotesque, DM Mono; OFL,
    /// their licences beside them) for this process.
    static func register() {
        guard let dir = resources?.url(forResource: "Fonts", withExtension: nil),
              let files = try? FileManager.default.contentsOfDirectory(at: dir, includingPropertiesForKeys: nil)
        else { return }
        for f in files where f.pathExtension == "ttf" {
            CTFontManagerRegisterFontsForURL(f as CFURL, .process, nil)
        }
    }

    /// SwiftPM's resource bundle: in Contents/Resources inside
    /// Terminatr.app, beside the binary under swift run. (Bundle.module
    /// looks only beside the binary, and traps when it isn't there.)
    private static var resources: Bundle? {
        let name = "Terminatr_Terminatr.bundle"
        for base in [Bundle.main.resourceURL, Bundle.main.bundleURL, Bundle.main.executableURL?.deletingLastPathComponent()] {
            if let u = base?.appendingPathComponent(name), let b = Bundle(url: u) {
                return b
            }
        }
        return nil
    }
}

extension Theme {
    /// An attention tone's colour: the dot, the tag.
    func colour(_ tone: Attention.Tone) -> Color {
        switch tone {
        case .approval: approval
        case .question: question
        case .attention: attention
        case .review, .done: good
        case .working: action
        case .idle: idle
        }
    }
}
