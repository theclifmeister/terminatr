import Foundation
import Testing

/// The Mac's text prompts are flat sheets, not system alerts (T1067):
/// an alert that holds a TextField (Send back, Rename Device were two)
/// looks and types differently from the rest of the window. Plain
/// confirmations may stay alerts. The app target can't be imported by
/// tests, so this reads its sources.
@Suite struct FlatSheetsTests {
    private static var sources: URL {
        URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .appendingPathComponent("Sources/Terminatr")
    }

    private func files() throws -> [(name: String, text: String)] {
        let urls = try FileManager.default.contentsOfDirectory(at: Self.sources, includingPropertiesForKeys: nil)
            .filter { $0.pathExtension == "swift" }
        return try urls.map { ($0.lastPathComponent, try String(contentsOf: $0, encoding: .utf8)) }
    }

    @Test func noAlertAsksForText() throws {
        let all = try files()
        #expect(all.count > 20)
        for (name, text) in all {
            var rest = Substring(text)
            while let r = rest.range(of: ".alert(") {
                rest = rest[r.upperBound...]
                // The alert's actions end at its message closure or the next modifier.
                let end = rest.range(of: "} message:")?.lowerBound ?? rest.index(rest.startIndex, offsetBy: min(400, rest.count))
                #expect(!rest[..<end].contains("TextField"), "\(name): an alert asks for text; use a FlatSheet")
            }
        }
    }

    @Test func textPromptsAreSheets() throws {
        let all = Dictionary(uniqueKeysWithValues: try files().map { ($0.name, $0.text) })
        #expect(all["NeedCard.swift"]?.contains("SendBackSheet(") == true)
        #expect(all["DeviceSettings.swift"]?.contains("RenameDeviceSheet(") == true)
    }
}
