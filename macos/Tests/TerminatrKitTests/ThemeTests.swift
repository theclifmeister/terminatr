import Foundation
import Testing
@testable import TerminatrKit

// The themes (T770): every text colour reads 4.5:1 or more over each
// surface it is drawn on, in every theme, light and dark, with and
// without Increase Contrast; the status dots stay apart for
// colour-blind eyes; the stored choice.

@Test(arguments: ThemeName.allCases)
func themeContrast(_ name: ThemeName) {
    for increased in [false, true] {
        let t = increased ? name.colours.increasedContrast() : name.colours
        for dark in [false, true] {
            for c in t.checks(dark: dark) where !c.passes {
                Issue.record("\(name.label) \(dark ? "dark" : "light")\(increased ? ", Increase Contrast" : ""): \(c)")
            }
        }
    }
}

/// Carbon's dark is the reference window's, exactly (flat-look's table).
@Test func carbonIsTheReference() {
    let c = ThemeColours.carbon
    let measured: [(Swatch, UInt32)] = [
        (c.chrome, 0x101213), (c.gutter, 0x121418), (c.pane, 0x17181c), (c.selection, 0x262628), (c.tab, 0x2c2d31),
        (c.line, 0x292b2f), (c.lineSoft, 0x202224), (c.focus, 0xa6aebe), (c.ink, 0xdddfe0), (c.muted, 0x8a8c90), (c.action, 0x22a89d),
    ]
    for (swatch, hex) in measured {
        #expect(swatch.dark == hex)
        #expect(swatch.darkAlpha == 1)
    }
}

/// Flat: the window's surfaces are solid in every theme, in both modes,
/// Increase Contrast's too.
@Test(arguments: ThemeName.allCases)
func surfacesAreSolid(_ name: ThemeName) {
    for t in [name.colours, name.colours.increasedContrast()] {
        for s in [t.chrome, t.gutter, t.pane, t.selection, t.tab] {
            #expect(s.alpha == 1 && s.darkAlpha == 1)
        }
    }
}

@Test(arguments: ThemeName.allCases)
func themeDotsApart(_ name: ThemeName) {
    // Bloom's dots are the measure: no theme leans on hue more.
    let least = min(ThemeColours.bloom.dotDistance(dark: false).distance, ThemeColours.bloom.dotDistance(dark: true).distance)
    for dark in [false, true] {
        let d = name.colours.dotDistance(dark: dark)
        #expect(d.distance >= min(least, 12), "\(name.label) \(dark ? "dark" : "light"): \(d.pair) ΔE \(d.distance)")
    }
}

@Test func themeFullSets() {
    for n in ThemeName.allCases {
        let t = n.colours
        #expect(t.terminal.ansi.count == 16, "\(n)")
    }
    // Each its own: no two share a pane.
    #expect(Set(ThemeName.allCases.map(\.colours.pane)).count == ThemeName.allCases.count)
}

@Test func storedTheme() {
    // New installs get Carbon; a stored choice stays.
    #expect(ThemeName.of(nil) == .carbon)
    #expect(ThemeName.of("bloom") == .bloom)
    #expect(ThemeName.of("dusk") == .dusk)
    #expect(ThemeName.of("neon") == .carbon)
}

@Test func paletteThemes() {
    let rows = Palette.themes(current: .dusk)
    #expect(rows.map(\.title).first == "Theme: Carbon")
    #expect(rows.count == ThemeName.allCases.count)
    #expect(rows.first { $0.command == .theme(.dusk) }?.detail.hasSuffix("in use") == true)
    // Found by a word, not listed on an empty query.
    #expect(Palette.search(rows, "").isEmpty)
    #expect(Palette.search(rows, "dusk").map(\.command) == [.theme(.dusk)])
    #expect(Palette.search(rows, "theme").count == min(rows.count, Palette.perGroup))
}

// T1002: agents draw for a dark terminal, so it is dark in light too; in
// dark it is the pane's colour.
@Test func terminalGround() {
    func luminance(_ hex: UInt32) -> Double { Double((hex >> 16) & 0xff) + Double((hex >> 8) & 0xff) + Double(hex & 0xff) }
    for name in ThemeName.allCases {
        let c = name.colours
        #expect(c.terminal.background.dark == c.pane.dark, "\(name)")
        #expect(luminance(c.terminal.background.light) < 3 * 64, "\(name)")
    }
}
