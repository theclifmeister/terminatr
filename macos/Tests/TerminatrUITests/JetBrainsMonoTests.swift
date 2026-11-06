import AppKit
import SwiftUI
import Testing
@testable import TerminatrUI

/// A missing or misnamed face falls back without a word, so these look.
@Suite struct JetBrainsMonoTests {
    @Test func everyWeightAskedForIsAFaceOfTheFont() {
        #expect(JetBrainsMono.register())
        let weights: [Font.Weight] = [.regular, .medium, .semibold, .bold]
        for weight in weights {
            let name = JetBrainsMono.face(weight)
            let font = NSFont(name: name, size: 13)
            #expect(font?.familyName == "JetBrains Mono", "\(name)")
            #expect(font?.fontName == name)
        }
    }

    /// What the terminal's bold and italic cells and Markdown's spans
    /// draw: the family's own faces, not a skewed or smeared regular.
    @Test func boldAndItalicFindTheFamilysFaces() {
        let base = JetBrainsMono.font(size: 13)
        let m = NSFontManager.shared
        #expect(m.convert(base, toHaveTrait: .boldFontMask).fontName == "JetBrainsMono-Bold")
        #expect(m.convert(base, toHaveTrait: .italicFontMask).fontName == "JetBrainsMono-Italic")
        #expect(m.convert(base, toHaveTrait: [.boldFontMask, .italicFontMask]).fontName == "JetBrainsMono-BoldItalic")
    }

    /// The bundle carries the licence the font's OFL asks to go with it.
    @Test func theLicenceShipsBesideTheFaces() throws {
        let bundle = Bundle.module
        let url = try #require(bundle.url(forResource: "OFL", withExtension: "txt", subdirectory: "Fonts"))
        #expect(try String(contentsOf: url, encoding: .utf8).contains("SIL Open Font License"))
    }

    /// A cell is what PaneZoom's fit expects of a mono font.
    @Test func aCellIsSixTenthsOfTheSize() {
        let f = JetBrainsMono.font(size: 100)
        #expect(abs(("W" as NSString).size(withAttributes: [.font: f]).width - 60) < 0.5)
    }
}
