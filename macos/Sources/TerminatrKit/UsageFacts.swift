import Foundation

/// What a thread's or task's usage says in the details (T1053), as the
/// design's "3 turns · 126k tokens written · 18.7M with cache reads":
/// turns and tokens written, which is what the agent made new (input,
/// output and cache writes), then the total with cache reads, and the
/// cost only when the record has one. Nothing on rows, and nothing at all
/// for no usage (an older thread, or an agent that gives none).
extension AppAPI.Usage {
    /// Tokens the agent wrote or sent fresh: all but cache reads.
    public var written: Int { input + output + cacheCreation }

    /// Every token counted, cache reads included.
    public var withCache: Int { written + cacheRead }

    /// Whether there is anything to say.
    public var isUsed: Bool { turns > 0 || withCache > 0 || costUsd > 0 || hasPlan }

    /// The cost as the record has it: "$3.41", "$3.41 partial" when some
    /// tokens had no price, "plan 12%" for an agent that reports its plan
    /// limit instead; nil when it has none.
    public var costText: String? {
        if hasPlan { return String(format: "plan %.0f%%", planPct) }
        guard costUsd > 0 else { return nil }
        let partial = uncosted > 0 && uncosted < withCache
        return String(format: "$%.2f", costUsd) + (partial ? " partial" : "")
    }

    /// The label and value of each line, for a fact list; empty for none.
    public var factRows: [(label: String, value: String)] {
        guard isUsed else { return [] }
        var rows: [(String, String)] = []
        if turns > 0 { rows.append(("Turns", "\(turns)")) }
        if withCache > 0 {
            rows.append(("Written", "\(Self.compact(written)) tokens"))
            if cacheRead > 0 { rows.append(("With cache", "\(Self.compact(withCache)) tokens")) }
        }
        if let cost = costText { rows.append((hasPlan ? "Plan" : "Cost", hasPlan ? String(cost.dropFirst(5)) : cost)) }
        return rows
    }

    /// The same on one line: "3 turns · 126k tokens written · 18.7M with
    /// cache reads · $12.40"; nil for none.
    public var line: String? {
        guard isUsed else { return nil }
        var parts: [String] = []
        if turns > 0 { parts.append("\(turns) turn\(turns == 1 ? "" : "s")") }
        if withCache > 0 {
            parts.append("\(Self.compact(written)) tokens written")
            if cacheRead > 0 { parts.append("\(Self.compact(withCache)) with cache reads") }
        }
        if let cost = costText { parts.append(cost) }
        return parts.joined(separator: " · ")
    }

    /// A fold's summary: "3 turns · 126k written · $3.41"; nil for none.
    public var short: String? {
        guard isUsed else { return nil }
        var parts: [String] = []
        if turns > 0 { parts.append("\(turns) turn\(turns == 1 ? "" : "s")") }
        if withCache > 0 { parts.append("\(Self.compact(written)) written") }
        if let cost = costText { parts.append(cost) }
        return parts.joined(separator: " · ")
    }

    /// 940, 5k, 12k, 126k, 1.2M, 18.7M.
    static func compact(_ n: Int) -> String {
        func trim(_ v: Double) -> String {
            let s = String(format: "%.1f", v)
            return s.hasSuffix(".0") ? String(s.dropLast(2)) : s
        }
        switch n {
        case 1_000_000...: return trim(Double(n) / 1e6) + "M"
        case 10_000...: return "\(Int((Double(n) / 1e3).rounded()))k"
        case 1_000...: return trim(Double(n) / 1e3) + "k"
        default: return "\(n)"
        }
    }
}
