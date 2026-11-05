import Foundation

/// The app's themes (T770): Carbon, the default, and the others the user
/// picks in Settings › Appearance, View › Theme or the palette. Each is
/// a full set of colours for light and dark (ThemeColours); the app's
/// Theme turns one into SwiftUI colours.
public enum ThemeName: String, CaseIterable, Sendable {
    case carbon, bloom, calm, graphite, dusk, aurora, sunset, solar

    public static let key = "theme"

    /// The stored choice; anything else is Carbon.
    public static func of(_ raw: String?) -> ThemeName {
        raw.flatMap(ThemeName.init(rawValue:)) ?? .carbon
    }

    public var label: String {
        switch self {
        case .carbon: "Carbon"
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
        case .carbon: "Neutral greys, one teal"
        case .bloom: "Plum and vivid violet"
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
        case .carbon: .carbon
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
/// terminal, so every theme's terminal is dark, in light too: in dark its
/// ground is the theme's pane, in light the theme's own dark (T1002).
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

/// What Increase Contrast changes in a theme besides its lines: stronger
/// question and approval cards, and a stronger muted ink.
public struct IncreasedColours: Hashable, Sendable {
    public var questionTint: Swatch
    public var approvalTint: Swatch
    public var muted: Swatch
}

/// A theme's colours, token by token: the app's Theme has a SwiftUI
/// colour for each (see there what each is for). Flat: every surface is
/// a solid colour, so what a text sits on is one colour too.
public struct ThemeColours: Hashable, Sendable {
    public var chrome: Swatch
    public var gutter: Swatch
    public var pane: Swatch
    public var line: Swatch
    public var lineSoft: Swatch
    public var selection: Swatch
    public var tab: Swatch
    public var focus: Swatch
    public var ink: Swatch
    public var muted: Swatch
    public var faint: Swatch
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
    public var questionTint: Swatch
    public var approvalTint: Swatch
    public var command: Swatch
    public var terminal: TerminalColours
    public var increased: IncreasedColours
    /// Increase Contrast's: increasedContrast() made it.
    public var contrast = false

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

    /// The colours as Increase Contrast wants them: lines drawn in ink at
    /// 45%, stronger cards and muted ink.
    public func increasedContrast() -> ThemeColours {
        var t = self
        t.questionTint = increased.questionTint
        t.approvalTint = increased.approvalTint
        t.line = Swatch(ink.light, ink.dark, alpha: 0.45)
        t.lineSoft = t.line
        t.muted = increased.muted
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
    /// Every text colour against the surfaces it is drawn on. No surface
    /// is translucent over anything but a solid pane, so each is one
    /// colour.
    public func checks(dark: Bool) -> [ContrastCheck] {
        func solid(_ s: Swatch) -> RGB { RGB(hex: s.hex(dark: dark)) }
        let paneRGB = solid(pane)
        func on(_ layers: Swatch...) -> RGB {
            layers.reduce(paneRGB) { $0.under($1, dark: dark) }
        }
        let grounds: [(String, RGB)] = [
            ("the window", solid(chrome)), ("the gutter", solid(gutter)), ("a pane", paneRGB),
        ]
        let cards: [(String, RGB)] = [
            ("a question card", on(questionTint)),
            ("an approval card", on(approvalTint)),
            ("a command", on(command)),
            ("an option", on(wash(ink, Self.inkWash))),
            ("a picked option", on(wash(question, Self.pickedWash))),
        ]
        // A soft StateGlyph's fill and a level chip, with ink on them.
        let washes: [(String, RGB)] = [
            ("approval", approval), ("question", question), ("attention", attention),
            ("good", good), ("action", action), ("idle", idle),
        ].map { ("a \($0.0) wash", on(wash($0.1, Self.toneWash))) }
        // Selected rows, tabs and buttons: their text is ink, never muted
        // (the reference's muted reads about 4.4:1 and 4.1:1 on them); a
        // picked option's as well.
        let raised: [(String, RGB)] = [("a selected row", solid(selection)), ("a tab", solid(tab))]
        let groundNames = Set(grounds.map(\.0))
        let surfaces = grounds + cards + washes + raised
        let plain = Set((grounds + cards).map(\.0)).subtracting(["a picked option"])
        let texts: [(String, Swatch, Set<String>)] = [
            ("ink", ink, Set(surfaces.map(\.0))),
            ("muted", muted, plain),
            ("action", action, groundNames),
            ("attention", attention, ["a pane"]),
            ("good", good, ["a pane"]),
            ("approval", approval, ["a pane", "an approval card"]),
            ("question", question, ["a pane", "a question card"]),
        ]
        var out: [ContrastCheck] = []
        for (name, colour, where_) in texts {
            let c = solid(colour)
            for (surface, rgb) in surfaces where where_.contains(surface) {
                out.append(ContrastCheck(text: name, surface: surface, ratio: c.contrast(rgb), least: 4.5))
            }
        }
        // Faint is for what isn't read closely: a placeholder, a key hint.
        for (surface, rgb) in grounds {
            out.append(ContrastCheck(text: "faint", surface: surface, ratio: solid(faint).contrast(rgb), least: 2.5))
        }
        let pills: [(String, Swatch, Swatch)] = [
            ("onAction", onAction, action), ("onGood", onGood, good), ("onAttention", onAttention, attention),
            ("onQuestion", onQuestion, question), ("onApproval", onApproval, approval),
            // A picked chip: the pane on ink; an idle pill: ink on idle.
            ("pane", pane, ink), ("ink", ink, idle),
        ]
        for (name, text, fill) in pills {
            out.append(ContrastCheck(text: name, surface: "its fill", ratio: solid(text).contrast(solid(fill)), least: 4.5))
        }
        // A ThreadMeter's segments and a StateGlyph's fill (T846) are
        // graphics on a pane: 3:1 (WCAG 1.4.11). Its legend's words carry
        // the states too.
        for (name, tone) in [("approval", approval), ("question", question), ("attention", attention), ("good", good), ("action", action)] {
            out.append(ContrastCheck(text: "a \(name) segment", surface: "a pane", ratio: solid(tone).contrast(paneRGB), least: 3))
        }
        // The focus line round a pane: a graphic too.
        out.append(ContrastCheck(text: "the focus line", surface: "a pane", ratio: solid(focus).contrast(paneRGB), least: 3))
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
