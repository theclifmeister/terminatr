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
/// colour of their own, so a theme is one more ThemeColours (T770). Each
/// colour has a light and a dark value and follows the system appearance.
/// Flat (T999): every surface is one solid colour.
public struct Theme: Sendable {
    /// The window, the sidebar, the title bar and the status bar.
    public var chrome: Color
    /// Behind the panes.
    public var gutter: Color
    /// A pane and what stands on one: a card, a sheet's group.
    public var pane: Color
    public var line: Color
    /// A line between rows in one group, under the title bar.
    public var lineSoft: Color
    /// A selected row, the user's prompts.
    public var selection: Color
    /// A selected tab, a button's fill.
    public var tab: Color
    /// The line round the focused pane.
    public var focus: Color

    public var ink: Color
    public var muted: Color
    /// What isn't read closely: a placeholder, a key hint.
    public var faint: Color
    /// Actions, a project's tile.
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

    /// A command or a path set in mono.
    public var command: Color
    /// The terminal tab.
    public var terminalBackground: Color
    public var terminalForeground: Color

    /// Which theme, and its colours as data: the terminal takes its
    /// ANSI colours from there.
    public var name: ThemeName
    public var colours: ThemeColours
    /// Increase Contrast is on: stronger lines and muted ink.
    public var contrast: Bool
    /// Every font's size times this: Settings › Appearance › Text size,
    /// and in the sidebar the system's sidebar size as well.
    public var scale: CGFloat = 1
    /// How much of scale's growth a display size (above 20 pt: a header,
    /// a project's name) takes: 1 on the Mac; on the phone less, as
    /// Apple's large titles grow under Dynamic Type (34 to 60 pt where
    /// the body triples), so a title word fits a row at accessibility
    /// sizes rather than breaking (T851).
    public var titleGrowth: CGFloat = 1
    /// The phone's Bold Text is on (Theme.mono).
    public var boldText = false

    public init(_ name: ThemeName, colours: ThemeColours? = nil) {
        let c = colours ?? name.colours
        func k(_ s: Swatch) -> Color { Color(s) }
        chrome = k(c.chrome)
        gutter = k(c.gutter)
        pane = k(c.pane)
        line = k(c.line)
        lineSoft = k(c.lineSoft)
        selection = k(c.selection)
        tab = k(c.tab)
        focus = k(c.focus)
        ink = k(c.ink)
        muted = k(c.muted)
        faint = k(c.faint)
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
        command = k(c.command)
        terminalBackground = k(c.terminal.background)
        terminalForeground = k(c.terminal.foreground)
        self.name = name
        self.colours = c
        contrast = c.contrast
    }

    /// A style's size: its points times the scale, a display size's
    /// times titleGrowth's share of it, so Dynamic Type (the phone's text
    /// size, in scale) grows every font once.
    public func size(_ style: TextStyle) -> CGFloat {
        let p = CGFloat(style.points)
        return p > 20 ? p * (1 + (scale - 1) * titleGrowth) : p * scale
    }

    /// The system font at the flat look's measured size (T1000), times
    /// the text size.
    public func sized(_ points: CGFloat, _ weight: Font.Weight = .regular) -> Font {
        .system(size: points * scale, weight: weight)
    }

    /// A page's big title: the design page's weight 620, a little over
    /// semibold, which a Font.Weight can't say.
    public func display(_ points: CGFloat) -> Font {
        #if canImport(UIKit)
        Font(UIFont.systemFont(ofSize: points * scale, weight: UIFont.Weight(rawValue: 0.32)))
        #else
        sized(points, .semibold)
        #endif
    }

    /// A title: the system font, as all text is (T999).
    public func title(_ style: TextStyle, _ weight: Font.Weight = .bold) -> Font {
        .system(size: size(style), weight: weight)
    }

    /// JetBrains Mono (T1029) at a style's size: code, paths, ids. Labels,
    /// buttons and lists stay the system's.
    public func mono(_ style: TextStyle, _ weight: Font.Weight = .regular) -> Font {
        JetBrainsMono.swiftUI(size: size(style), weight: legible(weight))
    }

    /// JetBrains Mono at a measured size, times the text size.
    public func monoSized(_ points: CGFloat, _ weight: Font.Weight = .regular) -> Font {
        JetBrainsMono.swiftUI(size: points * scale, weight: legible(weight))
    }

    /// A conversation's messages and the composer's text: mono, a size
    /// under the system's running text as its letters are wider.
    public var conversation: Font { monoSized(Flat.conversation) }

    /// The same for UIKit's text view.
    public var conversationFont: PlatformFont {
        JetBrainsMono.font(size: Flat.conversation * scale, weight: legible(.regular))
    }

    /// What Bold Text makes of a mono weight: the system's own fonts take
    /// it themselves, a bundled one doesn't.
    private func legible(_ weight: Font.Weight) -> Font.Weight {
        guard boldText else { return weight }
        return weight == .regular ? .semibold : .bold
    }

    /// The body's font, the system's.
    public func body(_ style: TextStyle, _ weight: Font.Weight = .regular) -> Font {
        .system(size: size(style), weight: weight)
    }

    /// The theme as Increase Contrast wants it (T748 J): lines and muted
    /// ink stronger (ThemeColours.increasedContrast).
    public func increasedContrast() -> Theme {
        var t = Theme(name, colours: colours.increasedContrast())
        t.scale = scale
        t.titleGrowth = titleGrowth
        return t
    }

    /// Carbon, the default (T999).
    public static let carbon = Theme(.carbon)

    private static let all = Dictionary(uniqueKeysWithValues: ThemeName.allCases.map { ($0, Theme($0)) })

    /// The theme by name, its colours made once.
    public static func named(_ n: ThemeName) -> Theme {
        all[n] ?? carbon
    }
}

private struct ThemeKey: EnvironmentKey {
    public static let defaultValue = Theme.carbon
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
        case .idle, .asleep: idle
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
        case .idle, .asleep: ink
        }
    }
}
