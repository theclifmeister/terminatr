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
    if case .table(let t) = b[8] { #expect(t.header.map(plain) == ["a", "b"] && t.rows.isEmpty) } else { Issue.record("\(b[8])") }
}

private func table(_ text: String) -> SafeMarkdown.Table? {
    let b = SafeMarkdown.blocks(text)
    for x in b { if case .table(let t) = x { return t } }
    return nil
}

@Test func tableAlignAndInline() throws {
    let t = try #require(table("""
    | Name | Kind | Size |  Note |
    |:-----|:----:|-----:|-------|
    | `go` | **bin** | 12 | [docs](https://example.com) |
    """))
    #expect(t.align == [.left, .center, .right, .none])
    #expect(t.header.map(plain) == ["Name", "Kind", "Size", "Note"])
    #expect(t.rows.map { $0.map(plain) } == [["go", "bin", "12", "docs"]])
    #expect(t.rows[0][0].runs.contains { $0.inlinePresentationIntent?.contains(.code) == true })
    #expect(t.rows[0][1].runs.contains { $0.inlinePresentationIntent?.contains(.stronglyEmphasized) == true })
    #expect(links(t.rows[0][3]).map(\.absoluteString) == ["https://example.com"])
}

@Test func tableRaggedRows() throws {
    let t = try #require(table("""
    | a | b | c |
    |---|---|---|
    | 1 |
    | 1 | 2 | 3 | 4 | 5 |
    | | x |
    """))
    #expect(t.rows.map { $0.map(plain) } == [["1", "", ""], ["1", "2", "3"], ["", "x", ""]])
}

@Test func tableEscapedPipes() throws {
    let t = try #require(table("""
    | expr | means |
    | --- | --- |
    | a \\| b | or |
    | `x \\| y` | code |
    """))
    #expect(t.rows.map { $0.map(plain) } == [["a | b", "or"], ["x | y", "code"]])
}

@Test func tableCodeSpanPipes() throws {
    let t = try #require(table("""
    | cmd | what |
    |-----|------|
    | `ls | wc -l` | count |
    | ``a `|` b`` | ticks |
    | `open | unclosed |
    """))
    #expect(t.rows.map { $0.map(plain) } == [["ls | wc -l", "count"], ["a `|` b", "ticks"], ["`open", "unclosed"]])
}

@Test func tableNeedsDelimiterRow() {
    #expect(table("| a | b |\n| c | d |") == nil)
    #expect(table("| a | b |\n|---|") == nil, "a delimiter row of another width")
    #expect(table("| a | b |\n|-x-|---|") == nil)
    #expect(table("a\n---") == nil, "a rule, not a table")
    let b = SafeMarkdown.blocks("| a | b |\n| c | d |")
    #expect(b.count == 1)
    if case .paragraph(let p) = b.first { #expect(plain(p).contains("| c | d |")) } else { Issue.record("\(b)") }
}

@Test func tableWithoutOuterPipes() throws {
    let b = SafeMarkdown.blocks("""
    Results:
    a | b
    --|:-
    1 | 2
    3 | 4
    - next
    """)
    #expect(b.count == 3)
    if case .paragraph(let p) = b.first { #expect(plain(p) == "Results:") } else { Issue.record("\(b)") }
    let t = try #require(table("a | b\n--|:-\n1 | 2"))
    #expect(t.align == [.none, .left])
    if case .table(let t) = b[1] { #expect(t.rows.map { $0.map(plain) } == [["1", "2"], ["3", "4"]]) } else { Issue.record("\(b[1])") }
    if case .item = b[2] {} else { Issue.record("a list ends the table: \(b[2])") }
}

@Test func tableEndsAtBlankOrBlock() throws {
    let b = SafeMarkdown.blocks("| a |\n| - |\n| 1 |\nplain row\n\nafter\n| a |\n|---|\n# Head")
    #expect(b.count == 4)
    if case .table(let t) = b[0] { #expect(t.rows.map { $0.map(plain) } == [["1"], ["plain row"]]) } else { Issue.record("\(b[0])") }
    if case .paragraph(let p) = b[1] { #expect(plain(p) == "after") } else { Issue.record("\(b[1])") }
    if case .table = b[2] {} else { Issue.record("\(b[2])") }
    if case .heading = b[3] {} else { Issue.record("\(b[3])") }
}

@Test func tableCellsAreCleaned() throws {
    let t = try #require(table("| x |\n|---|\n| [run](file:///etc/passwd) ![i](https://evil.example/t.png) <b>hi</b> |"))
    #expect(links(t.rows[0][0]).isEmpty)
    #expect(t.rows[0][0].runs.allSatisfy { $0.imageURL == nil })
}

@Test func tableWidths() {
    let w = SafeMarkdown.Table.widths
    #expect(w([50, 80], nil, 96) == [50, 80], "no room given: their own")
    #expect(w([50, 80], 300, 96) == [50, 80], "they fit")
    #expect(w([50, 400, 300], 450, 96) == [50, 200, 200], "the widest give way, evenly")
    #expect(w([50, 400, 120], 450, 96) == [50, 280, 120])
    #expect(w([400, 400, 400], 150, 96) == [96, 96, 96], "past floor: wider than room, so it scrolls")
    #expect(w([], 100, 96) == [])
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
