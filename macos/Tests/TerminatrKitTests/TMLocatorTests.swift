import Foundation
import Testing
@testable import TerminatrKit

@Test func locatorOrder() throws {
    let l = TMLocator(home: "/Users/u", isExecutable: { _ in false })
    #expect(l.candidates == ["/opt/homebrew/bin/tm", "/usr/local/bin/tm", "/Users/u/.local/bin/tm"])
    #expect(throws: TMNotFound(looked: l.candidates, configured: false)) { try l.locate() }

    let local = TMLocator(home: "/Users/u", isExecutable: { $0 == "/Users/u/.local/bin/tm" || $0 == "/usr/local/bin/tm" })
    #expect(try local.locate().path == "/usr/local/bin/tm")
}

@Test func locatorConfigured() throws {
    let l = TMLocator(configured: " /opt/tm/bin/tm ", home: "/Users/u", isExecutable: { $0 == "/opt/tm/bin/tm" })
    #expect(try l.locate().path == "/opt/tm/bin/tm")
    // A configured path that isn't there is an error, not a fallback.
    let missing = TMLocator(configured: "/nowhere/tm", home: "/Users/u", isExecutable: { _ in true })
    #expect(try missing.locate().path == "/nowhere/tm")
    let gone = TMLocator(configured: "/nowhere/tm", home: "/Users/u", isExecutable: { $0 != "/nowhere/tm" })
    #expect(throws: TMNotFound(looked: ["/nowhere/tm"], configured: true)) { try gone.locate() }
}
