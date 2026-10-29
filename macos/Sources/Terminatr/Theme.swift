import AppKit
import CoreText
import SwiftUI
import TerminatrKit
import TerminatrUI

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
