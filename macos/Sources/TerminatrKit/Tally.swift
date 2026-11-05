import Foundation

extension Attention.Tone {
    /// The tones in a ThreadMeter's order (T846): what waits on the
    /// user first, then working, idle and done.
    public static let order: [Attention.Tone] = [.approval, .question, .attention, .review, .working, .idle, .asleep, .done]

    /// A legend's words for n threads in this tone: "1 allow?", "2
    /// asks", "1 needs you".
    public func counted(_ n: Int) -> String {
        switch self {
        case .approval: "\(n) allow?"
        case .question: "\(n) asks"
        case .attention: n == 1 ? "1 needs you" : "\(n) need you"
        case .review: "\(n) review"
        case .working: "\(n) working"
        case .idle: "\(n) idle"
        case .done: "\(n) done"
        case .asleep: "\(n) asleep"
        }
    }

    /// The tones sorted in order, and how many of each, the empty ones
    /// left out.
    public static func tally(_ tones: [Attention.Tone]) -> [(tone: Attention.Tone, count: Int)] {
        order.compactMap { t in
            let n = tones.count { $0 == t }
            return n > 0 ? (t, n) : nil
        }
    }
}

/// A project's monogram (T846), on its card and over its screen.
public enum Monogram {
    /// The first letters of the name's first two words ("acme-web":
    /// aw), else its first two letters ("billing": bi), lower case.
    public static func letters(_ name: String) -> String {
        let words = name.split { !$0.isLetter && !$0.isNumber }
        let letters = words.count >= 2 ? words.prefix(2).compactMap(\.first).map(String.init).joined()
            : String((words.first ?? Substring(name)).prefix(2))
        return letters.lowercased()
    }
}

extension Attention.Tone {
    /// A project's threads in words after its role's state (T1001): "no
    /// threads", "1 working" when they all do one thing, else "3 threads".
    public static func summary(_ tones: [Attention.Tone]) -> String {
        let t = tally(tones)
        if t.isEmpty { return "no threads" }
        if t.count == 1, t[0].tone != .idle, t[0].tone != .done { return t[0].tone.counted(t[0].count) }
        return tones.count == 1 ? "1 thread" : "\(tones.count) threads"
    }

    /// The dot of a row that speaks for a project: its strongest tone, in
    /// the tally's order, what waits on the user before what works, an idle
    /// role's dot taking its threads' work (T1001).
    public static func strongest(_ own: Attention.Tone, _ tones: [Attention.Tone]) -> Attention.Tone {
        tally([own] + tones).first?.tone ?? own
    }
}
