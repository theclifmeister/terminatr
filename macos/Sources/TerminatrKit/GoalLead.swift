import Foundation
import NaturalLanguage

/// A goal as a header shows it (T996): plain text that opens on its first
/// sentence and never ends in an ellipsis. The view shows the first of
/// whole, sentence and cuts that fits its two lines; anything but whole
/// comes with More.
public struct GoalLead: Hashable, Sendable {
    public let whole: String
    /// The first sentence: the whole goal when it has only one.
    public let sentence: String
    /// A first sentence too long for two lines, cut short, longest first:
    /// at its clauses, then at its words, without their closing commas.
    public let cuts: [String]

    /// Clauses shorter than this say too little to stand for the goal.
    static let shortestClause = 24

    public init(_ goal: String) {
        whole = goal.trimmingCharacters(in: .whitespacesAndNewlines)
        sentence = Self.firstSentence(whole)
        cuts = Self.cuts(of: sentence)
    }

    /// Whether the first sentence leaves some of the goal unshown.
    public var hasMore: Bool { sentence != whole }

    private static func firstSentence(_ text: String) -> String {
        let line = text.split(whereSeparator: \.isNewline).first.map(String.init) ?? text
        let tokenizer = NLTokenizer(unit: .sentence)
        tokenizer.string = line
        var first = line
        tokenizer.enumerateTokens(in: line.startIndex..<line.endIndex) { range, _ in
            first = String(line[range])
            return false
        }
        return first.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private static func cuts(of sentence: String) -> [String] {
        let words = sentence.split(separator: " ")
        guard words.count > 1 else { return [] }
        let heads = (1..<words.count).reversed().map { words[0..<$0].joined(separator: " ") }
        let clauses = heads.filter { h in
            h.count >= shortestClause && (h.last.map { ",;:".contains($0) } ?? false)
        }
        var seen = Set<String>()
        return (clauses + heads).map(tidy).filter { !$0.isEmpty && seen.insert($0).inserted }
    }

    /// A cut without what only leads on: a closing comma, a dash, dots.
    private static func tidy(_ s: String) -> String {
        s.trimmingCharacters(in: CharacterSet(charactersIn: ",;:-–—….").union(.whitespaces))
    }
}
