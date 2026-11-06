import CoreText
import Foundation
import SwiftUI
#if os(macOS)
import AppKit
/// The platform's font: NSFont on the Mac, UIFont on iOS.
public typealias PlatformFont = NSFont
#else
import UIKit
public typealias PlatformFont = UIFont
#endif

/// The apps' mono font, JetBrains Mono NL 2.304 (T1029), the family without
/// ligatures, so a command reads as typed: code, diffs, file
/// views, the terminal, ids and the conversation's text. The six static
/// faces in Fonts/ (with OFL.txt, its licence) are the ones drawn: the
/// weights Theme.mono is asked for, and bold and italic for Markdown's
/// spans and the terminal's cells. Views go through Theme.mono,
/// Theme.monoSized or Theme.conversation; the terminal and the Mac's code
/// view through `font(size:)`.
public enum JetBrainsMono {
    /// The faces are in the process's font list: reading it registers them
    /// once, so no app has to at launch.
    @discardableResult
    public static func register() -> Bool { registered }

    private static let registered: Bool = {
        guard let folder = folder else { return false }
        let urls = (try? FileManager.default.contentsOfDirectory(at: folder, includingPropertiesForKeys: nil))?
            .filter { $0.pathExtension == "ttf" } ?? []
        var all = !urls.isEmpty
        for url in urls {
            var error: Unmanaged<CFError>?
            if !CTFontManagerRegisterFontsForURL(url as CFURL, .process, &error) { all = false }
            error?.release()
        }
        return all
    }()

    /// The target's resource folder. Not Bundle.module: SwiftPM's accessor
    /// looks beside Terminatr.app's own bundle URL, not in
    /// Contents/Resources where make app puts it, and traps when it finds
    /// nothing. The phone's bundle sits at the app's top level, and a test
    /// run's beside the .xctest.
    private static var folder: URL? {
        let name = "Terminatr_TerminatrUI.bundle"
        let code = Bundle(for: Marker.self)
        let places = [Bundle.main.resourceURL, Bundle.main.bundleURL, code.resourceURL, code.bundleURL.deletingLastPathComponent()]
        for place in places.compactMap({ $0 }) {
            let bundle = place.appendingPathComponent(name)
            for inner in [bundle.appendingPathComponent("Fonts"), bundle.appendingPathComponent("Contents/Resources/Fonts")]
            where FileManager.default.fileExists(atPath: inner.path) {
                return inner
            }
        }
        return nil
    }

    private final class Marker {}

    /// A face's PostScript name, for a weight.
    public static func face(_ weight: Font.Weight) -> String {
        switch weight {
        case .bold, .heavy, .black: "JetBrainsMonoNL-Bold"
        case .semibold: "JetBrainsMonoNL-SemiBold"
        case .medium: "JetBrainsMonoNL-Medium"
        default: "JetBrainsMonoNL-Regular"
        }
    }

    /// The font at exactly this size. Fixed: the theme's scale already
    /// holds the Dynamic Type and text size, and Font.custom's relative
    /// size would grow it a second time. `.bold()` and `.italic()` on it,
    /// and Markdown's spans, find the family's own faces.
    public static func swiftUI(size: CGFloat, weight: Font.Weight = .regular) -> Font {
        register()
        return .custom(face(weight), fixedSize: size)
    }

    /// The font for AppKit and UIKit text: the terminal, the Mac's code
    /// view, the phone's composer. The system's mono if the faces are
    /// somehow missing, so a cell grid never goes proportional.
    public static func font(size: CGFloat, weight: Font.Weight = .regular) -> PlatformFont {
        register()
        return PlatformFont(name: face(weight), size: size) ?? .monospacedSystemFont(ofSize: size, weight: .regular)
    }
}
