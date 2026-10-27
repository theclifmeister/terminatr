import AppKit
import CoreText
import SwiftUI
import TerminatrKit

/// The app's look as named tokens. Views read only these, never a
/// colour of their own, so a theme is one more ThemeColours (T770:
/// Bloom, Calm, Graphite, Dusk, Aurora, Sunset, Solar). Each colour has
/// a light and a dark value and follows the system appearance.
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

    /// Which theme, and its colours as data: the terminal takes its
    /// ANSI colours from there.
    var name: ThemeName
    var colours: ThemeColours
    /// Increase Contrast is on: panels are opaque, and the system
    /// material under them is left out.
    var contrast: Bool
    /// Every font's size times this: Settings › Appearance › Text size,
    /// and in the sidebar the system's sidebar size as well.
    var scale: CGFloat = 1

    init(_ name: ThemeName, colours: ThemeColours? = nil) {
        let c = colours ?? name.colours
        func k(_ s: Swatch) -> Color { Color(s) }
        mesh = c.mesh.map(k)
        base = k(c.base)
        ink = k(c.ink)
        muted = k(c.muted)
        line = k(c.line)
        action = k(c.action)
        onAction = k(c.onAction)
        good = k(c.good)
        onGood = k(c.onGood)
        idle = k(c.idle)
        attention = k(c.attention)
        onAttention = k(c.onAttention)
        question = k(c.question)
        onQuestion = k(c.onQuestion)
        approval = k(c.approval)
        onApproval = k(c.onApproval)
        sidebarTint = k(c.sidebarTint)
        glass = k(c.glass)
        bar = k(c.bar)
        cardTint = k(c.cardTint)
        edge = k(c.edge)
        meter = c.meter.map(k)
        prompt = c.prompt.map(k)
        onPrompt = k(c.onPrompt)
        agentCard = k(c.agentCard)
        questionTint = k(c.questionTint)
        approvalTint = k(c.approvalTint)
        command = k(c.command)
        terminalBackground = k(c.terminal.background)
        terminalForeground = k(c.terminal.foreground)
        self.name = name
        self.colours = c
        contrast = c.contrast
    }

    func size(_ style: TextStyle) -> CGFloat {
        CGFloat(style.points) * scale
    }

    /// The theme's display face (Bloom's Bricolage Grotesque), or SF Pro.
    func title(_ style: TextStyle, _ weight: Font.Weight = .bold) -> Font {
        guard let f = colours.titleFamily else { return .system(size: size(style), weight: weight) }
        return .custom(f, size: size(style)).weight(weight)
    }

    /// The theme's mono (Bloom's DM Mono), or SF Mono.
    func mono(_ style: TextStyle, _ weight: Font.Weight = .regular) -> Font {
        guard let f = colours.monoFamily else { return .system(size: size(style), weight: weight, design: .monospaced) }
        return .custom(f, size: size(style)).weight(weight)
    }

    /// The body's font, SF Pro (Bloom: "body stays SF Pro").
    func body(_ style: TextStyle, _ weight: Font.Weight = .regular) -> Font {
        .system(size: size(style), weight: weight)
    }

    /// The theme as Increase Contrast wants it (T748 J): the panels
    /// opaque, so no mesh shows through, and their edges and rules
    /// drawn in the muted ink (ThemeColours.increasedContrast).
    func increasedContrast() -> Theme {
        var t = Theme(name, colours: colours.increasedContrast())
        t.scale = scale
        return t
    }

    /// Bloom (design T744, concept 02), the first theme and the default.
    static let bloom = Theme(.bloom)

    private static let all = Dictionary(uniqueKeysWithValues: ThemeName.allCases.map { ($0, Theme($0)) })

    /// The theme by name, its colours made once.
    static func named(_ n: ThemeName) -> Theme {
        all[n] ?? bloom
    }
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

    /// A theme's swatch: one colour when light and dark agree.
    init(_ s: Swatch) {
        if s.light == s.dark && s.alpha == s.darkAlpha {
            self.init(hex: s.light, alpha: s.alpha)
        } else {
            self = .dynamic(s.light, s.dark, alpha: s.alpha, darkAlpha: s.darkAlpha)
        }
    }
}

extension NSColor {
    /// A theme's swatch as drawn in light or dark (the terminal's, set
    /// once per appearance).
    convenience init(_ s: Swatch, dark: Bool) {
        self.init(hex: s.hex(dark: dark), alpha: s.alpha(dark: dark))
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
