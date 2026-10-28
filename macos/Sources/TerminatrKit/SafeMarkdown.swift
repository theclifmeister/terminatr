import Foundation

/// An agent's Markdown as the conversation view draws it (design T734,
/// R6: agent text is data). Blocks are split here, line by line; inline
/// spans go through Foundation's parser and are then cleaned: raw HTML
/// stays literal text (nothing here or in SwiftUI's Text renders it),
/// an image keeps only its alt text (no remote image is ever loaded),
/// and a link survives only for http, https or mailto, whose click the
/// view confirms with the full URL before it opens anything.
public enum SafeMarkdown {
    public enum Block: Equatable, Sendable {
        case paragraph(AttributedString)
        case heading(Int, AttributedString)
        /// A list item: its marker ("•", "1.") and depth.
        case item(String, Int, AttributedString)
        case quote(AttributedString)
        /// A fenced block, drawn monospaced as-is.
        case code(String, String)
        case rule
        case table(Table)
    }

    /// A GitHub pipe table: its header, its columns' alignment from
    /// the delimiter row, and its body, every row as many cells as the
    /// header (a short row padded with empty cells, a long one cut).
    public struct Table: Equatable, Sendable {
        public enum Align: Equatable, Sendable { case none, left, center, right }
        public var align: [Align]
        public var header: [AttributedString]
        public var rows: [[AttributedString]]

        /// The columns' widths for room: each its own width, or, when they
        /// don't fit, none wider than a cap found so that they do (and not
        /// under floor).
        public static func widths(_ own: [CGFloat], fit room: CGFloat?, floor: CGFloat) -> [CGFloat] {
            guard let room, own.reduce(0, +) > room else { return own }
            // The cap: the widest columns are cut to it, the rest kept.
            var rest = room
            var left = own.count
            var cap = floor
            for w in own.sorted() {
                let even = rest / CGFloat(left)
                if w <= even {
                    rest -= w
                    left -= 1
                } else {
                    cap = even
                    break
                }
            }
            cap = max(cap.rounded(.down), floor)
            return own.map { min($0, cap) }
        }
    }

    /// The link schemes a click may open.
    public static let schemes: Set<String> = ["http", "https", "mailto"]

    /// blocks(text), kept for the texts drawn lately: a conversation
    /// shown again, or scrolled back to, doesn't parse its turns again.
    public static func cached(_ text: String) -> [Block] {
        let key = text as NSString
        if let hit = cache.object(forKey: key) { return hit.blocks }
        let b = blocks(text)
        cache.setObject(Parsed(blocks: b), forKey: key, cost: text.utf8.count)
        return b
    }

    private final class Parsed {
        let blocks: [Block]
        init(blocks: [Block]) { self.blocks = blocks }
    }

    /// NSCache is thread-safe; it holds a few MB of parsed text.
    nonisolated(unsafe) private static let cache: NSCache<NSString, Parsed> = {
        let c = NSCache<NSString, Parsed>()
        c.totalCostLimit = 4 << 20
        return c
    }()

    public static func blocks(_ text: String) -> [Block] {
        var out: [Block] = []
        var para: [String] = []
        var lines = text.replacingOccurrences(of: "\r\n", with: "\n").split(separator: "\n", omittingEmptySubsequences: false).map(String.init)[...]

        func flush() {
            if !para.isEmpty {
                out.append(.paragraph(inline(para.joined(separator: "\n"))))
                para = []
            }
        }

        while let line = lines.popFirst() {
            let t = line.trimmingCharacters(in: .whitespaces)
            if let fence = fenceOf(t) {
                flush()
                var body: [String] = []
                while let l = lines.popFirst() {
                    if l.trimmingCharacters(in: .whitespaces).hasPrefix(fence.marker) { break }
                    body.append(l)
                }
                out.append(.code(fence.lang, body.joined(separator: "\n")))
            } else if t.isEmpty {
                flush()
            } else if let (level, rest) = heading(t) {
                flush()
                out.append(.heading(level, inline(rest)))
            } else if isRule(t) {
                flush()
                out.append(.rule)
            } else if let header = cells(t), let next = lines.first, let align = delimiter(next), align.count == header.count {
                flush()
                lines.removeFirst()
                var rows: [[String]] = []
                while let l = lines.first, !endsTable(l) {
                    lines.removeFirst()
                    let r = cells(l.trimmingCharacters(in: .whitespaces)) ?? [l.trimmingCharacters(in: .whitespaces)]
                    rows.append(Array((r + Array(repeating: "", count: max(0, align.count - r.count))).prefix(align.count)))
                }
                out.append(.table(Table(align: align, header: header.map(inline), rows: rows.map { $0.map(inline) })))
            } else if let (marker, depth, rest) = item(line) {
                flush()
                out.append(.item(marker, depth, inline(rest)))
            } else if t.hasPrefix(">") {
                flush()
                var quoted = [String(t.dropFirst()).trimmingCharacters(in: .whitespaces)]
                while let l = lines.first?.trimmingCharacters(in: .whitespaces), l.hasPrefix(">") {
                    quoted.append(String(l.dropFirst()).trimmingCharacters(in: .whitespaces))
                    lines.removeFirst()
                }
                out.append(.quote(inline(quoted.joined(separator: "\n"))))
            } else {
                para.append(line)
            }
        }
        flush()
        return out
    }

    /// One span of inline Markdown, cleaned.
    public static func inline(_ text: String) -> AttributedString {
        let opts = AttributedString.MarkdownParsingOptions(
            allowsExtendedAttributes: false,
            interpretedSyntax: .inlineOnlyPreservingWhitespace,
            failurePolicy: .returnPartiallyParsedIfPossible
        )
        var s = (try? AttributedString(markdown: text, options: opts)) ?? AttributedString(text)
        for run in s.runs {
            if run.imageURL != nil {
                s[run.range].imageURL = nil
            }
            if let url = run.link, !allowed(url) {
                s[run.range].link = nil
            }
        }
        return s
    }

    public static func allowed(_ url: URL) -> Bool {
        guard let scheme = url.scheme?.lowercased(), schemes.contains(scheme) else { return false }
        return scheme == "mailto" || url.host?.isEmpty == false
    }

    /// A table row's cells, trimmed, or nil when the line has no pipe
    /// to split on. Pipes split cells but for an escaped one ("\\|",
    /// read as "|", in a code span too) and one inside a code span:
    /// GitHub splits there, but an agent seldom escapes a pipe in
    /// `a | b`, and the cell it meant is the better guess.
    static func cells(_ t: String) -> [String]? {
        let chars = Array(t)
        var out: [String] = []
        var cell = ""
        var piped = false
        var i = 0
        while i < chars.count {
            let c = chars[i]
            if c == "\\", i + 1 < chars.count, chars[i + 1] == "|" {
                cell.append("|")
                i += 2
            } else if c == "`" {
                let run = chars[i...].prefix { $0 == "`" }.count
                if let close = closingTicks(chars, from: i + run, run: run) {
                    cell += String(chars[i..<close + run]).replacingOccurrences(of: "\\|", with: "|")
                    i = close + run
                } else {
                    cell += String(repeating: "`", count: run)
                    i += run
                }
            } else if c == "|" {
                piped = true
                out.append(cell)
                cell = ""
                i += 1
            } else {
                cell.append(c)
                i += 1
            }
        }
        out.append(cell)
        guard piped else { return nil }
        // A leading and a trailing pipe open and close the row.
        if t.hasPrefix("|") { out.removeFirst() }
        if t.hasSuffix("|"), out.count > 1, out.last?.isEmpty == true { out.removeLast() }
        return out.map { $0.trimmingCharacters(in: .whitespaces) }
    }

    /// Where a code span opened by run backticks closes: the next run
    /// of exactly as many.
    private static func closingTicks(_ chars: [Character], from: Int, run: Int) -> Int? {
        var j = from
        while j < chars.count {
            guard chars[j] == "`" else { j += 1; continue }
            let n = chars[j...].prefix { $0 == "`" }.count
            if n == run { return j }
            j += n
        }
        return nil
    }

    /// A delimiter row's alignments ("---", ":--", ":-:", "--:"), or nil
    /// when the line isn't one. It needs a pipe, so "---" stays a rule.
    static func delimiter(_ line: String) -> [Table.Align]? {
        guard let cs = cells(line.trimmingCharacters(in: .whitespaces)), !cs.isEmpty else { return nil }
        var out: [Table.Align] = []
        for c in cs {
            let left = c.hasPrefix(":"), right = c.hasSuffix(":")
            let dashes = c.dropFirst(left ? 1 : 0).dropLast(right && c.count > 1 ? 1 : 0)
            guard !dashes.isEmpty, dashes.allSatisfy({ $0 == "-" }) else { return nil }
            out.append(left && right ? .center : left ? .left : right ? .right : .none)
        }
        return out
    }

    /// A table runs to a blank line or the start of another block.
    private static func endsTable(_ line: String) -> Bool {
        let t = line.trimmingCharacters(in: .whitespaces)
        return t.isEmpty || fenceOf(t) != nil || heading(t) != nil || t.hasPrefix(">") || (cells(t) == nil && (isRule(t) || item(line) != nil))
    }

    private static func fenceOf(_ t: String) -> (marker: String, lang: String)? {
        for m in ["```", "~~~"] where t.hasPrefix(m) {
            return (m, String(t.dropFirst(3)).trimmingCharacters(in: .whitespaces))
        }
        return nil
    }

    private static func heading(_ t: String) -> (Int, String)? {
        let hashes = t.prefix { $0 == "#" }.count
        guard (1...6).contains(hashes) else { return nil }
        let rest = t.dropFirst(hashes)
        guard rest.first == " " else { return nil }
        return (hashes, rest.trimmingCharacters(in: .whitespaces).trimmingCharacters(in: CharacterSet(charactersIn: "#")).trimmingCharacters(in: .whitespaces))
    }

    private static func isRule(_ t: String) -> Bool {
        let chars = t.filter { $0 != " " }
        guard chars.count >= 3, let c = chars.first, "-*_".contains(c) else { return false }
        return chars.allSatisfy { $0 == c }
    }

    /// A list item: "- x", "* x", "+ x", "1. x" or "1) x", indented by
    /// two spaces a level.
    private static func item(_ line: String) -> (String, Int, String)? {
        let indent = line.prefix { $0 == " " || $0 == "\t" }.reduce(0) { $0 + ($1 == "\t" ? 4 : 1) }
        let t = line.drop { $0 == " " || $0 == "\t" }
        if let c = t.first, "-*+".contains(c), t.dropFirst().first == " " {
            var rest = String(t.dropFirst(2))
            var marker = "•"
            if rest.hasPrefix("[ ] ") { marker = "☐"; rest = String(rest.dropFirst(4)) }
            if rest.hasPrefix("[x] ") || rest.hasPrefix("[X] ") { marker = "☑"; rest = String(rest.dropFirst(4)) }
            return (marker, indent / 2, rest)
        }
        let digits = t.prefix { $0.isASCII && $0.isNumber }
        if !digits.isEmpty, digits.count <= 9 {
            let after = t.dropFirst(digits.count)
            if let d = after.first, d == "." || d == ")", after.dropFirst().first == " " {
                return ("\(digits).", indent / 2, String(after.dropFirst(2)))
            }
        }
        return nil
    }
}
