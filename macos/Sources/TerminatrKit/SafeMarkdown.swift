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
        /// A fenced block, or a table's lines, drawn monospaced as-is.
        case code(String, String)
        case rule
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
            } else if t.hasPrefix("|") {
                flush()
                var rows = [t]
                while let l = lines.first, l.trimmingCharacters(in: .whitespaces).hasPrefix("|") {
                    rows.append(l.trimmingCharacters(in: .whitespaces))
                    lines.removeFirst()
                }
                out.append(.code("table", rows.joined(separator: "\n")))
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
