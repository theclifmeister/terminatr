import SwiftUI
import TerminatrKit
#if os(macOS)
import AppKit
/// The platform's colour: NSColor on the Mac, UIColor on iOS.
public typealias PlatformColor = NSColor
#else
import UIKit
public typealias PlatformColor = UIColor
#endif

/// The app's look as named tokens. Views read only these, never a
/// colour of their own, so a theme is one more ThemeColours (T770:
/// Bloom, Calm, Graphite, Dusk, Aurora, Sunset, Solar). Each colour has
/// a light and a dark value and follows the system appearance.
public struct Theme: Sendable {
    /// The backdrop's mesh: four corners, then the centre.
    public var mesh: [Color]
    /// Under the mesh, and alone when Reduce Transparency is on.
    public var base: Color

    public var ink: Color
    public var muted: Color
    public var line: Color
    /// Actions and the selection (Bloom's ink violet).
    public var action: Color
    public var onAction: Color
    public var good: Color
    public var onGood: Color
    public var idle: Color
    /// Something that needs the user.
    public var attention: Color
    public var onAttention: Color
    public var question: Color
    public var onQuestion: Color
    public var approval: Color
    public var onApproval: Color

    /// Frost tints over the system material: the sidebar and the
    /// inspector, the detail's panel (Bloom's window: frost in light,
    /// smoked glass in dark), the title bar over it, and the cards.
    public var sidebarTint: Color
    public var glass: Color
    public var bar: Color
    public var cardTint: Color
    public var edge: Color
    /// The redesign's deeper glass (T845): a deck card on the frost.
    public var deck: Color
    public var meter: [Color]

    /// The user's prompts in the conversation: a gradient, and its ink.
    public var prompt: [Color]
    public var onPrompt: Color
    /// The agent's text cards.
    public var agentCard: Color
    /// The question and approval cards' wash, over the frost.
    public var questionTint: Color
    public var approvalTint: Color
    /// A command or a path set in mono.
    public var command: Color
    /// The terminal tab.
    public var terminalBackground: Color
    public var terminalForeground: Color

    /// Which theme, and its colours as data: the terminal takes its
    /// ANSI colours from there.
    public var name: ThemeName
    public var colours: ThemeColours
    /// Increase Contrast is on: panels are opaque, and the system
    /// material under them is left out.
    public var contrast: Bool
    /// Every font's size times this: Settings › Appearance › Text size,
    /// and in the sidebar the system's sidebar size as well.
    public var scale: CGFloat = 1

    public init(_ name: ThemeName, colours: ThemeColours? = nil) {
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
        deck = k(c.deck)
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

    /// A style's size: its points times the scale. The custom faces are
    /// fixed at it, as SF is, so Dynamic Type (the phone's text size,
    /// in scale) grows every font once and alike.
    public func size(_ style: TextStyle) -> CGFloat {
        CGFloat(style.points) * scale
    }

    /// The theme's display face (Bloom's Bricolage Grotesque), or SF Pro.
    public func title(_ style: TextStyle, _ weight: Font.Weight = .bold) -> Font {
        guard let f = colours.titleFamily else { return .system(size: size(style), weight: weight) }
        return .custom(f, fixedSize: size(style)).weight(weight)
    }

    /// The theme's mono (Bloom's DM Mono), or SF Mono.
    public func mono(_ style: TextStyle, _ weight: Font.Weight = .regular) -> Font {
        guard let f = colours.monoFamily else { return .system(size: size(style), weight: weight, design: .monospaced) }
        return .custom(f, fixedSize: size(style)).weight(weight)
    }

    /// The body's font, SF Pro (Bloom: "body stays SF Pro").
    public func body(_ style: TextStyle, _ weight: Font.Weight = .regular) -> Font {
        .system(size: size(style), weight: weight)
    }

    /// The theme as Increase Contrast wants it (T748 J): the panels
    /// opaque, so no mesh shows through, and their edges and rules
    /// drawn in the muted ink (ThemeColours.increasedContrast).
    public func increasedContrast() -> Theme {
        var t = Theme(name, colours: colours.increasedContrast())
        t.scale = scale
        return t
    }

    /// Bloom (design T744, concept 02), the first theme and the default.
    public static let bloom = Theme(.bloom)

    private static let all = Dictionary(uniqueKeysWithValues: ThemeName.allCases.map { ($0, Theme($0)) })

    /// The theme by name, its colours made once.
    public static func named(_ n: ThemeName) -> Theme {
        all[n] ?? bloom
    }
}

private struct ThemeKey: EnvironmentKey {
    public static let defaultValue = Theme.bloom
}

extension EnvironmentValues {
    public var theme: Theme {
        get { self[ThemeKey.self] }
        set { self[ThemeKey.self] = newValue }
    }
}

extension Color {
    public init(hex: UInt32, alpha: Double = 1) {
        self.init(PlatformColor(hex: hex, alpha: alpha))
    }

    /// A colour with a light and a dark value, swapped with the
    /// appearance the view is drawn in.
    public static func dynamic(_ light: UInt32, _ dark: UInt32, alpha: Double = 1, darkAlpha: Double? = nil) -> Color {
        #if os(macOS)
        Color(nsColor: NSColor(name: nil) { appearance in
            appearance.bestMatch(from: [.aqua, .darkAqua]) == .darkAqua
                ? NSColor(hex: dark, alpha: darkAlpha ?? alpha)
                : NSColor(hex: light, alpha: alpha)
        })
        #else
        Color(uiColor: UIColor { traits in
            traits.userInterfaceStyle == .dark
                ? UIColor(hex: dark, alpha: darkAlpha ?? alpha)
                : UIColor(hex: light, alpha: alpha)
        })
        #endif
    }

    /// A theme's swatch: one colour when light and dark agree.
    public init(_ s: Swatch) {
        if s.light == s.dark && s.alpha == s.darkAlpha {
            self.init(hex: s.light, alpha: s.alpha)
        } else {
            self = .dynamic(s.light, s.dark, alpha: s.alpha, darkAlpha: s.darkAlpha)
        }
    }
}

extension PlatformColor {
    public convenience init(hex: UInt32, alpha: Double) {
        let r = CGFloat((hex >> 16) & 0xff) / 255, g = CGFloat((hex >> 8) & 0xff) / 255, b = CGFloat(hex & 0xff) / 255
        #if os(macOS)
        self.init(srgbRed: r, green: g, blue: b, alpha: alpha)
        #else
        self.init(red: r, green: g, blue: b, alpha: alpha)
        #endif
    }
}

extension Theme {
    /// An attention tone's colour: the dot, the tag.
    public func colour(_ tone: Attention.Tone) -> Color {
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
    public func on(_ tone: Attention.Tone) -> Color {
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
