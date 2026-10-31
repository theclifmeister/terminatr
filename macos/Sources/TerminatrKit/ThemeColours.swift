import Foundation

/// The app's themes (T770): Bloom, the first, and the others the user
/// picks in Settings › Appearance, View › Theme or the palette. Each is
/// a full set of colours for light and dark (ThemeColours); the app's
/// Theme turns one into SwiftUI colours.
public enum ThemeName: String, CaseIterable, Sendable {
    case bloom, calm, graphite, dusk, aurora, sunset, solar

    public static let key = "theme"

    /// The stored choice; anything else is Bloom.
    public static func of(_ raw: String?) -> ThemeName {
        raw.flatMap(ThemeName.init(rawValue:)) ?? .bloom
    }

    public var label: String {
        switch self {
        case .bloom: "Bloom"
        case .calm: "Calm"
        case .graphite: "Graphite"
        case .dusk: "Dusk"
        case .aurora: "Aurora"
        case .sunset: "Sunset"
        case .solar: "Solar"
        }
    }

    /// Its mood in a few words, under its swatch.
    public var mood: String {
        switch self {
        case .bloom: "Vivid mesh, frosted glass"
        case .calm: "Warm and easy on the eye"
        case .graphite: "Neutral and professional"
        case .dusk: "Dimmed, for night work"
        case .aurora: "Neon on deep navy"
        case .sunset: "Coral, amber and pink"
        case .solar: "High legibility"
        }
    }

    public var colours: ThemeColours {
        switch self {
        case .bloom: .bloom
        case .calm: .calm
        case .graphite: .graphite
        case .dusk: .dusk
        case .aurora: .aurora
        case .sunset: .sunset
        case .solar: .solar
        }
    }
}

/// A theme's colour: sRGB hex and alpha, in light and in dark.
public struct Swatch: Hashable, Sendable {
    public var light: UInt32
    public var dark: UInt32
    public var alpha: Double
    public var darkAlpha: Double

    /// One hex for both when dark is left out; darkAlpha as alpha.
    public init(_ light: UInt32, _ dark: UInt32? = nil, alpha: Double = 1, darkAlpha: Double? = nil) {
        self.light = light
        self.dark = dark ?? light
        self.alpha = alpha
        self.darkAlpha = darkAlpha ?? alpha
    }

    public func hex(dark d: Bool) -> UInt32 { d ? dark : light }
    public func alpha(dark d: Bool) -> Double { d ? darkAlpha : alpha }
}

/// The terminal tab's colours. Agents draw their screens for a dark
/// terminal, so every theme's terminal is dark, in light too.
public struct TerminalColours: Hashable, Sendable {
    public var background: Swatch
    public var foreground: Swatch
    public var cursor: Swatch
    /// Over the background, under the selected text.
    public var selection: Swatch
    /// The 16 ANSI colours: black, red, green, yellow, blue, magenta,
    /// cyan, white, then their bright ones.
    public var ansi: [Swatch]
}

/// What Increase Contrast puts in place of a theme's frost (T748 J):
/// opaque panels, so no mesh shows through, stronger edges and muted
/// ink, the rules drawn in ink at 45%.
public struct OpaqueColours: Hashable, Sendable {
    /// The sidebar, the inspector and the detail's glass.
    public var panel: Swatch
    public var bar: Swatch
    public var card: Swatch
    public var questionTint: Swatch
    public var approvalTint: Swatch
    public var edge: Swatch
    public var muted: Swatch
}

/// A theme's colours, token by token: the app's Theme has a SwiftUI
/// colour for each (see there what each is for).
public struct ThemeColours: Hashable, Sendable {
    /// Four corners, then the centre.
    public var mesh: [Swatch]
    public var base: Swatch
    public var ink: Swatch
    public var muted: Swatch
    public var line: Swatch
    public var action: Swatch
    public var onAction: Swatch
    public var good: Swatch
    public var onGood: Swatch
    public var idle: Swatch
    public var attention: Swatch
    public var onAttention: Swatch
    public var question: Swatch
    public var onQuestion: Swatch
    public var approval: Swatch
    public var onApproval: Swatch
    public var sidebarTint: Swatch
    public var glass: Swatch
    public var bar: Swatch
    public var cardTint: Swatch
    public var edge: Swatch
    public var meter: [Swatch]
    public var prompt: [Swatch]
    public var onPrompt: Swatch
    public var agentCard: Swatch
    public var questionTint: Swatch
    public var approvalTint: Swatch
    public var command: Swatch
    public var terminal: TerminalColours
    public var opaque: OpaqueColours
    /// The titles' and mono texts' families; nil is the system's (SF
    /// Pro, SF Mono).
    public var titleFamily: String?
    public var monoFamily: String?
    /// Increase Contrast's: increasedContrast() made it.
    public var contrast = false

    /// The redesign's deeper glass (T845): a card straight on the frost,
    /// with no panel under it, its tint lighter than a card's in light and
    /// the panel's smoke in dark, so the mesh shows through. Increase
    /// Contrast's opaque card.
    public var deck: Swatch {
        if contrast { return opaque.card }
        return Swatch(cardTint.light, glass.dark, alpha: cardTint.alpha * Self.deckLight, darkAlpha: glass.darkAlpha * Self.deckDark)
    }

    /// The deck's share of a card's tint in light, and of the panel's in
    /// dark.
    static let deckLight = 0.75
    static let deckDark = 0.9

    /// A tone's wash: a soft StateGlyph's and a picked chip's, and an
    /// option picked before it is sent.
    public static let toneWash = 0.16
    public static let pickedWash = 0.18
    /// An option's row and a secondary button: a wash of ink.
    public static let inkWash = 0.05

    /// A tone's colour at a wash's strength.
    public func wash(_ tone: Swatch, _ share: Double) -> Swatch {
        Swatch(tone.light, tone.dark, alpha: share)
    }

    /// The colours as Increase Contrast wants them: the panels opaque,
    /// their edges and rules in the muted ink.
    public func increasedContrast() -> ThemeColours {
        var t = self
        t.sidebarTint = opaque.panel
        t.glass = opaque.panel
        t.bar = opaque.bar
        t.cardTint = opaque.card
        t.agentCard = opaque.card
        t.questionTint = opaque.questionTint
        t.approvalTint = opaque.approvalTint
        t.edge = opaque.edge
        t.line = Swatch(ink.light, ink.dark, alpha: 0.45)
        t.muted = opaque.muted
        t.contrast = true
        return t
    }
}

/// An opaque sRGB colour, 0...1 a channel, for working out contrast.
public struct RGB: Hashable, Sendable {
    public var r, g, b: Double

    public init(hex: UInt32) {
        r = Double((hex >> 16) & 0xff) / 255
        g = Double((hex >> 8) & 0xff) / 255
        b = Double(hex & 0xff) / 255
    }

    public init(r: Double, g: Double, b: Double) {
        self.r = r
        self.g = g
        self.b = b
    }

    /// The swatch drawn over self, in light or dark.
    public func under(_ s: Swatch, dark: Bool) -> RGB {
        let a = s.alpha(dark: dark), t = RGB(hex: s.hex(dark: dark))
        return RGB(r: t.r * a + r * (1 - a), g: t.g * a + g * (1 - a), b: t.b * a + b * (1 - a))
    }

    /// WCAG's relative luminance.
    public var luminance: Double {
        func lin(_ c: Double) -> Double { c <= 0.04045 ? c / 12.92 : pow((c + 0.055) / 1.055, 2.4) }
        return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b)
    }

    /// WCAG's contrast ratio, 1...21.
    public func contrast(_ o: RGB) -> Double {
        let a = luminance, b = o.luminance
        return (max(a, b) + 0.05) / (min(a, b) + 0.05)
    }

    /// As one with a colour vision deficiency sees it (Machado, Oliveira
    /// and Fernandes 2009, full severity).
    public func seen(_ d: Deficiency) -> RGB {
        func lin(_ c: Double) -> Double { c <= 0.04045 ? c / 12.92 : pow((c + 0.055) / 1.055, 2.4) }
        func enc(_ c: Double) -> Double {
            let v = min(max(c, 0), 1)
            return v <= 0.0031308 ? v * 12.92 : 1.055 * pow(v, 1 / 2.4) - 0.055
        }
        let m = d.matrix, x = [lin(r), lin(g), lin(b)]
        let y = (0..<3).map { i in (0..<3).reduce(0) { $0 + m[i * 3 + $1] * x[$1] } }
        return RGB(r: enc(y[0]), g: enc(y[1]), b: enc(y[2]))
    }

    /// CIE76 ΔE between the two in CIELAB (D65): about 2 is just
    /// noticeable, 10 plainly different at a glance.
    public func distance(_ o: RGB) -> Double {
        let a = lab, b = o.lab
        return ((a.0 - b.0) * (a.0 - b.0) + (a.1 - b.1) * (a.1 - b.1) + (a.2 - b.2) * (a.2 - b.2)).squareRoot()
    }

    private var lab: (Double, Double, Double) {
        func lin(_ c: Double) -> Double { c <= 0.04045 ? c / 12.92 : pow((c + 0.055) / 1.055, 2.4) }
        let (R, G, B) = (lin(r), lin(g), lin(b))
        let x = (0.4124 * R + 0.3576 * G + 0.1805 * B) / 0.95047
        let y = 0.2126 * R + 0.7152 * G + 0.0722 * B
        let z = (0.0193 * R + 0.1192 * G + 0.9505 * B) / 1.08883
        func f(_ t: Double) -> Double { t > 216.0 / 24389 ? cbrt(t) : (24389.0 / 27 * t + 16) / 116 }
        return (116 * f(y) - 16, 500 * (f(x) - f(y)), 200 * (f(y) - f(z)))
    }
}

public enum Deficiency: CaseIterable, Sendable {
    case protanopia, deuteranopia, tritanopia

    fileprivate var matrix: [Double] {
        switch self {
        case .protanopia: [0.152286, 1.052583, -0.204868, 0.114503, 0.786281, 0.099216, -0.003882, -0.048116, 1.051998]
        case .deuteranopia: [0.367322, 0.860646, -0.227968, 0.280085, 0.672501, 0.047413, -0.011820, 0.042940, 0.968881]
        case .tritanopia: [1.255528, -0.076749, -0.178779, -0.078411, 0.930809, 0.147602, 0.004733, 0.691367, 0.303900]
        }
    }
}

/// One text colour over one surface, at its worst spot.
public struct ContrastCheck: Sendable, CustomStringConvertible {
    public let text: String
    public let surface: String
    public let ratio: Double
    /// What it must reach: 4.5 for text (WCAG AA), 3 for a cursor or
    /// the terminal's dim black.
    public let least: Double

    public var passes: Bool { ratio >= least }

    public var description: String {
        String(format: "%@ on %@: %.2f (≥ %.1f)", text, surface, ratio, least)
    }
}

extension ThemeColours {
    /// The system material under the panels, as the sidebar measures in
    /// the app (T752): 40% white in light, 40% #1e1e1e in dark.
    static let material = Swatch(0xffffff, 0x1e1e1e, alpha: 0.4)

    /// Every text colour against the surfaces it is drawn on, each over
    /// its worst spot: the mesh's five colours and its base, under the
    /// surface's layers (Bloom's measure, T752). No text sits on the raw
    /// mesh. With Increase Contrast the panels are opaque, so the mesh
    /// no longer counts.
    public func checks(dark: Bool) -> [ContrastCheck] {
        let spots = (mesh + [base]).map { RGB(hex: $0.hex(dark: dark)) }
        func layered(_ layers: [Swatch]) -> [RGB] {
            spots.map { s in layers.reduce(s) { $0.under($1, dark: dark) } }
        }
        let frost = contrast ? [Swatch]() : [Self.material]
        let surfaces: [(String, [RGB])] = [
            ("the sidebar", layered(frost + [sidebarTint])),
            ("the glass", layered(frost + [glass])),
            ("the title bar", layered(frost + [glass, bar])),
            // The menu bar's panels too: glass under their tint.
            ("a card", layered(frost + [glass, cardTint])),
            ("a sidebar card", layered(frost + [sidebarTint, cardTint])),
            ("an agent card", layered(frost + [glass, agentCard])),
            ("a question card", layered(frost + [glass, questionTint])),
            ("an approval card", layered(frost + [glass, approvalTint])),
            ("a command", layered(frost + [glass, cardTint, command])),
            // The redesign's surfaces (T845): a deck card on the frost,
            // a tone's wash on it (a soft StateGlyph), an option picked
            // before sending, and the command block in a deck card.
            ("a deck card", layered(frost + [deck])),
            ("a deck command", layered(frost + [deck, command])),
            ("an option", layered(frost + [deck, wash(ink, Self.inkWash)])),
            ("a picked option", layered(frost + [deck, wash(question, Self.pickedWash)])),
            // The phone's sheets (T850): Settings and pairing on the
            // glass, their deck cards over it, a fingerprint chip and the
            // level chip on those.
            ("a sheet card", layered(frost + [glass] + frost + [deck])),
            ("a sheet chip", layered(frost + [glass] + frost + [deck, wash(ink, Self.inkWash)])),
            ("a level chip", layered(frost + [glass] + frost + [deck, wash(good, Self.toneWash)])),
            // The session's (T848): the header's title and a run chip on
            // the glass, a key strip's cap (the glass with an ink wash),
            // and the dock's Queue / Steer in its field.
            ("a key", layered(frost + [glass, wash(ink, Self.inkWash)])),
            ("a dock's modes", layered(frost + [deck, wash(ink, Self.inkWash)])),
        ] + [
            ("approval", approval), ("question", question), ("attention", attention),
            ("good", good), ("action", action), ("idle", idle),
        ].map { ("a \($0.0) wash", layered(frost + [deck, wash($0.1, Self.toneWash)])) }
        let washes = Set(["approval", "question", "attention", "good", "action", "idle"].map { "a \($0) wash" })
        let texts: [(String, Swatch, Set<String>?)] = [
            ("ink", ink, nil),
            ("muted", muted, Set(surfaces.map(\.0)).subtracting(washes)),
            ("action", action, ["the sidebar", "the glass", "a card", "a deck card"]),
            ("attention", attention, ["the glass", "a card"]),
            ("good", good, ["a card"]),
            ("approval", approval, ["a card", "an approval card"]),
            ("question", question, ["a card", "a question card"]),
        ]
        var out: [ContrastCheck] = []
        for (name, colour, on) in texts {
            let c = RGB(hex: colour.hex(dark: dark))
            for (surface, rgbs) in surfaces where on?.contains(surface) ?? true {
                out.append(ContrastCheck(text: name, surface: surface, ratio: rgbs.map(c.contrast).min()!, least: 4.5))
            }
        }
        let pills: [(String, Swatch, Swatch)] = [
            ("onAction", onAction, action), ("onGood", onGood, good), ("onAttention", onAttention, attention),
            ("onQuestion", onQuestion, question), ("onApproval", onApproval, approval),
            // A picked chip: the base on ink; an idle pill: ink on idle.
            ("base", base, ink), ("ink", ink, idle),
        ] + prompt.enumerated().map { ("onPrompt", onPrompt, $0.element) }
        for (name, text, fill) in pills {
            let r = RGB(hex: text.hex(dark: dark)).contrast(RGB(hex: fill.hex(dark: dark)))
            out.append(ContrastCheck(text: name, surface: "its fill", ratio: r, least: 4.5))
        }
        // A screen's header title (ScreenHeader) is the one text on the
        // raw mesh: large text, so 3:1 (WCAG AA). Its other lines sit on
        // glass.
        let title = RGB(hex: ink.hex(dark: dark))
        out.append(ContrastCheck(text: "a header's title", surface: "the mesh", ratio: spots.map(title.contrast).min()!, least: 3))
        // A ThreadMeter's segments and a StateGlyph's fill (T846) are
        // graphics on a deck card: 3:1 (WCAG 1.4.11). Its legend's
        // words carry the states too.
        let deckCard = layered(frost + [deck])
        for (name, tone) in [("approval", approval), ("question", question), ("attention", attention), ("good", good), ("action", action)] {
            let c = RGB(hex: tone.hex(dark: dark))
            out.append(ContrastCheck(text: "a \(name) segment", surface: "a deck card", ratio: deckCard.map(c.contrast).min()!, least: 3))
        }
        let term = terminal, bg = RGB(hex: term.background.hex(dark: dark))
        out.append(ContrastCheck(text: "terminal text", surface: "the terminal", ratio: RGB(hex: term.foreground.hex(dark: dark)).contrast(bg), least: 7))
        out.append(ContrastCheck(text: "terminal text", surface: "the selection",
                                 ratio: RGB(hex: term.foreground.hex(dark: dark)).contrast(bg.under(term.selection, dark: dark)), least: 4.5))
        out.append(ContrastCheck(text: "the cursor", surface: "the terminal", ratio: RGB(hex: term.cursor.hex(dark: dark)).contrast(bg), least: 3))
        for (i, c) in term.ansi.enumerated() where i != 0 {
            out.append(ContrastCheck(text: "ANSI \(i)", surface: "the terminal", ratio: RGB(hex: c.hex(dark: dark)).contrast(bg), least: i == 8 ? 3 : 4.5))
        }
        return out
    }

    /// The status dots told apart by colour alone (working, idle, done
    /// and needs you on a sidebar row): their least ΔE across normal
    /// vision and the three dichromacies. Waiting rows carry a word too.
    public func dotDistance(dark: Bool) -> (pair: String, distance: Double) {
        let dots: [(String, Swatch)] = [("action", action), ("idle", idle), ("good", good), ("attention", attention)]
        var worst = ("", Double.infinity)
        for i in dots.indices {
            for j in dots.indices where j > i {
                let a = RGB(hex: dots[i].1.hex(dark: dark)), b = RGB(hex: dots[j].1.hex(dark: dark))
                let d = ([a.distance(b)] + Deficiency.allCases.map { a.seen($0).distance(b.seen($0)) }).min()!
                if d < worst.1 { worst = ("\(dots[i].0)/\(dots[j].0)", d) }
            }
        }
        return worst
    }
}
