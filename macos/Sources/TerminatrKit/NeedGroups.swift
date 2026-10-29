import Foundation

/// The phone's Needs you header (T847): its filter chips, a group of
/// needs each, "All" first; and what a need's card says of its kind and
/// age.
extension Need {
    /// A filter chip's needs.
    public enum Group: String, CaseIterable, Sendable {
        case all, approval, question, review, blocked, link, paired

        /// The chip's words for count of it: "1 approval", "2 asks".
        public func label(_ count: Int) -> String {
            let one = count == 1
            switch self {
            case .all: return "All \(count)"
            case .approval: return one ? "1 approval" : "\(count) approvals"
            case .question: return one ? "1 ask" : "\(count) asks"
            case .review: return "\(count) to review"
            case .blocked: return "\(count) blocked"
            case .link: return one ? "1 link" : "\(count) links"
            case .paired: return "\(count) paired"
            }
        }

        /// Its chip's dot; none for All.
        public var tone: Attention.Tone? {
            switch self {
            case .all: nil
            case .approval: .approval
            case .question: .question
            case .review: .review
            case .blocked: .attention
            case .link: .working
            case .paired: .idle
            }
        }
    }

    /// The chip it is under, besides All.
    public var group: Group {
        switch kind {
        case .approval: .approval
        case .question: .question
        case .review: .review
        case .blocked, .checks, .held, .refusal: .blocked
        case .link: .link
        case .network: .paired
        }
    }

    /// The chips for needs: All, then each group that has any, in
    /// Group's order, with its count.
    public static func groups(_ needs: [Need]) -> [(group: Group, count: Int)] {
        var out: [(group: Group, count: Int)] = [(.all, needs.count)]
        for g in Group.allCases where g != .all {
            let n = needs.reduce(0) { $0 + ($1.group == g ? 1 : 0) }
            if n > 0 { out.append((g, n)) }
        }
        return out
    }

    /// needs under group, in their order; All's when no need is (the
    /// last of a group answered).
    public static func filter(_ needs: [Need], _ group: Group) -> [Need] {
        let some = needs.filter { group == .all || $0.group == group }
        return some.isEmpty ? needs : some
    }

    /// How long it has waited, short, as a card's corner says it: "now",
    /// "4m", "2h", "3d"; "" when unknown.
    public func age(now: Date = Date()) -> String {
        Self.age(since: since, now: now)
    }

    /// The same of anything waiting since then (a session's question, T848).
    public static func age(since: Date?, now: Date = Date()) -> String {
        guard let since else { return "" }
        let s = max(0, Int(now.timeIntervalSince(since)))
        switch s {
        case ..<60: return "now"
        case ..<3600: return "\(s / 60)m"
        case ..<86400: return "\(s / 3600)h"
        default: return "\(s / 86400)d"
        }
    }
}

extension Need.Kind {
    /// Its card's tone: its glyph, chip dot and answer's colour. What
    /// the phone can't answer here (a paired server's, a link's) is not
    /// one that waits, so its glyph is soft.
    public var tone: Attention.Tone {
        switch self {
        case .approval: .approval
        case .question: .question
        case .review: .review
        case .blocked, .checks, .held, .refusal: .attention
        case .link: .working
        case .network: .idle
        }
    }

    /// Its card's word, over the project: "Approval", "Asks".
    public var word: String {
        switch self {
        case .approval: "Approval"
        case .question: "Asks"
        case .review: "Review"
        case .blocked: "Blocked"
        case .checks: "Checks fail"
        case .held: "Held"
        case .refusal: "Refused"
        case .link: "Link"
        case .network: "Paired"
        }
    }
}
