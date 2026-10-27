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

/// Bloom's numbers as T752 measured them in Theme.swift's table: the
/// model the checks use is the one Bloom was tuned with.
@Test func bloomMeasuresAsBefore() {
    let checks = ThemeColours.bloom.checks(dark: false)
    func ratio(_ text: String, _ surface: String) -> Double {
        checks.first { $0.text == text && $0.surface == surface }!.ratio
    }
    #expect(abs(ratio("ink", "the sidebar") - 12.9) < 0.1)
    #expect(abs(ratio("action", "the glass") - 5.0) < 0.1)
    #expect(abs(ratio("muted", "a card") - 8.4) < 0.2)
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
        #expect(t.mesh.count == 5, "\(n)")
        #expect(t.terminal.ansi.count == 16, "\(n)")
        #expect(t.prompt.count >= 1 && t.meter.count >= 2, "\(n)")
    }
    // Each its own: no two share a mesh.
    #expect(Set(ThemeName.allCases.map(\.colours.mesh)).count == ThemeName.allCases.count)
}

@Test func storedTheme() {
    #expect(ThemeName.of(nil) == .bloom)
    #expect(ThemeName.of("dusk") == .dusk)
    #expect(ThemeName.of("neon") == .bloom)
}

@Test func paletteThemes() {
    let rows = Palette.themes(current: .dusk)
    #expect(rows.map(\.title).first == "Theme: Bloom")
    #expect(rows.count == ThemeName.allCases.count)
    #expect(rows.first { $0.command == .theme(.dusk) }?.detail.hasSuffix("in use") == true)
    // Found by a word, not listed on an empty query.
    #expect(Palette.search(rows, "").isEmpty)
    #expect(Palette.search(rows, "dusk").map(\.command) == [.theme(.dusk)])
    #expect(Palette.search(rows, "theme").count == min(rows.count, Palette.perGroup))
}
