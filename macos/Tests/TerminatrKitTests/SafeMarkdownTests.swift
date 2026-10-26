import Foundation
import Testing
@testable import TerminatrKit

private func plain(_ s: AttributedString) -> String { String(s.characters) }

private func links(_ s: AttributedString) -> [URL] { s.runs.compactMap(\.link) }

@Test func markdownBlocks() {
    let b = SafeMarkdown.blocks("""
    # Plan

    Two **ways** forward:

    - in memory
      - per project
    1. on disk
    > quoted
    ---
    ```go
    x := <b>1</b>
    ```
    | a | b |
    |---|---|
    """)
    #expect(b.count == 9)
    guard b.count == 9 else { return }
    #expect(b[0] == .heading(1, AttributedString("Plan")))
    if case .paragraph(let p) = b[1] { #expect(plain(p) == "Two ways forward:") } else { Issue.record("not a paragraph: \(b[1])") }
    if case .item(let m, let d, let t) = b[2] { #expect(m == "•" && d == 0 && plain(t) == "in memory") } else { Issue.record("\(b[2])") }
    if case .item(_, let d, _) = b[3] { #expect(d == 1) } else { Issue.record("\(b[3])") }
    if case .item(let m, _, _) = b[4] { #expect(m == "1.") } else { Issue.record("\(b[4])") }
    if case .quote(let q) = b[5] { #expect(plain(q) == "quoted") } else { Issue.record("\(b[5])") }
    #expect(b[6] == .rule)
    #expect(b[7] == .code("go", "x := <b>1</b>"))
    #expect(b[8] == .code("table", "| a | b |\n|---|---|"))
}

@Test func markdownKeepsHTMLAsText() {
    let s = SafeMarkdown.inline("a <img src=\"https://evil.example/x.png\"> <script>alert(1)</script> b")
    #expect(links(s).isEmpty)
    #expect(s.runs.allSatisfy { $0.imageURL == nil })
    #expect(plain(s).hasPrefix("a "))
    #expect(plain(s).hasSuffix(" b"))
}

@Test func markdownDropsImages() {
    let s = SafeMarkdown.inline("see ![the chart](https://evil.example/track.png) here")
    #expect(s.runs.allSatisfy { $0.imageURL == nil })
    #expect(links(s).isEmpty)
    #expect(plain(s).contains("the chart"))
}

@Test func markdownLinks() {
    let s = SafeMarkdown.inline("[docs](https://example.com/a?b=1) [mail](mailto:x@example.com) [run](file:///etc/passwd) [js](javascript:alert(1)) [app](x-apple-reminder://1)")
    #expect(links(s).map(\.absoluteString) == ["https://example.com/a?b=1", "mailto:x@example.com"])
    #expect(plain(s) == "docs mail run js app")
}

@Test func markdownAllowed() {
    #expect(SafeMarkdown.allowed(URL(string: "HTTPS://example.com")!))
    #expect(!SafeMarkdown.allowed(URL(string: "https:///nohost")!))
    #expect(!SafeMarkdown.allowed(URL(string: "ftp://example.com")!))
}
