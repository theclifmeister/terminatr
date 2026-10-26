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
    var onGood: Color
    var idle: Color
    /// Something that needs the user.
    var attention: Color
    var onAttention: Color
    var question: Color
    var onQuestion: Color
    var approval: Color
    var onApproval: Color

    /// Frost tints over the system material: the sidebar and the
    /// inspector, the detail's panel (Bloom's window: frost in light,
    /// smoked glass in dark), the title bar over it, and the cards.
    var sidebarTint: Color
    var glass: Color
    var bar: Color
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
    /// frosted panels, smoked glass in dark. No text sits on the raw
    /// mesh: the sidebar and inspector have their frost, the detail its
    /// glass, the cards theirs over it.
    ///
    /// Contrast (T752), each text pair over its worst spot: the mesh's
    /// five colours and its base, under the surface's layers, with the
    /// system material taken as 40% white (light) or 40% #1e1e1e (dark),
    /// as the sidebar measures in the app. WCAG asks 4.5:1 of text.
    ///
    ///                                light           dark
    ///     ink on the sidebar         12.9 (violet)   11.1 (lime)
    ///     muted on the sidebar        6.3 (violet)    7.1 (lime)
    ///     action on the sidebar       4.6 (violet)    4.6 (lime)
    ///     ink on the glass           14.0 (violet)   14.5 (lime)
    ///     muted on the glass          6.8 (violet)    9.3 (lime)
    ///     action on the glass         5.0 (violet)    6.1 (lime)
    ///     attention on the glass      4.8 (violet)    7.0 (lime)
    ///     ink on the title bar       15.1 (violet)   13.0 (lime)
    ///     muted on the title bar      7.4 (violet)    8.3 (lime)
    ///     ink on a card              17.1 (violet)   12.0 (lime)
    ///     muted on a card             8.4 (violet)    7.7 (lime)
    ///     action, question on a card  6.1 (violet)    5.0 (lime)
    ///     approval on a card          5.1 (violet)    6.4 (lime)
    ///     attention on a card         5.4 (violet)    5.8 (lime)
    ///     good on a card              5.2 (violet)    8.0 (lime)
    ///     approval on its card        4.7 (coral)     5.6 (coral)
    ///     question on its card        5.4 (violet)    6.4 (lime)
    ///     onAction on action          6.3             6.9
    ///     onAttention on attention    5.5             7.9
    ///     onApproval on approval      5.2             8.7
    ///     onQuestion on question      6.3             8.8
    ///     onGood on good              5.4             9.4
    ///     onPrompt on prompt          4.9             6.1
    ///
    /// So the tones' colours (approval, attention, good) are never text
    /// on the sidebar (3.8–4.4:1 there in light): a tag there is a filled
    /// pill. Against Bloom's page: light approval, attention and good are
    /// darker (Bloom's e2541c, d83a22, 0e9f6e read 3.5–4.3:1 on the
    /// cards), the light sidebar's tint 60% (30%) and the dark one smoked
    /// glass (black 18%), since here the sidebar has no window glass
    /// under it; dark bubblegum, the mesh's centre, is calmer (8c1f6e).
    static let bloom = Theme(
        mesh: [
            .dynamic(0xff6b4a, 0xc2365a), // coral
            .dynamic(0x7b5cff, 0x4b2fb8), // violet
            .dynamic(0xc6f432, 0x5b7a10), // lime
            .dynamic(0x29d3c3, 0x0f6e6a), // lagoon
            .dynamic(0xff5fb7, 0x5a1d52), // bubblegum
        ],
        base: .dynamic(0xf3d7ff, 0x160f2c),
        ink: .dynamic(0x1c1433, 0xf6f2ff),
        muted: .dynamic(0x4f4670, 0xc9c0ea),
        line: .dynamic(0x1c1433, 0xf6f2ff, alpha: 0.12),
        action: .dynamic(0x5a3cf0, 0xa58fff),
        onAction: .dynamic(0xffffff, 0x160f33),
        good: .dynamic(0x0a7a54, 0x5fe0a8),
        onGood: .dynamic(0xffffff, 0x062a1c),
        idle: .dynamic(0xa49cc0, 0x6c6390),
        attention: .dynamic(0xbb2e18, 0xff8a73),
        onAttention: .dynamic(0xffffff, 0x2a0c06),
        question: .dynamic(0x5a3cf0, 0xb9a8ff),
        onQuestion: .dynamic(0xffffff, 0x160f33),
        approval: .dynamic(0xc2410c, 0xff9a6b),
        onApproval: .dynamic(0xffffff, 0x2a0c06),
        sidebarTint: .dynamic(0xffffff, 0x120e24, alpha: 0.60, darkAlpha: 0.40),
        glass: .dynamic(0xffffff, 0x120e24, alpha: 0.70, darkAlpha: 0.72),
        bar: .dynamic(0xffffff, 0xffffff, alpha: 0.35, darkAlpha: 0.04),
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

    /// The ink on a tone's colour: a filled pill's text.
    func on(_ tone: Attention.Tone) -> Color {
        switch tone {
        case .approval: onApproval
        case .question: onQuestion
        case .attention: onAttention
        case .review, .done: onGood
        case .working: onAction
        case .idle: ink
        }
    }
}
