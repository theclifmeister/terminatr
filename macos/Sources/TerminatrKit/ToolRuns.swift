import Foundation

extension Transcript {
    /// What the conversation draws in one place: an entry, or a run of
    /// consecutive tool calls (no prompt, text, result or note between
    /// them) folded into one group. A single call stays an entry.
    public enum Row: Sendable, Equatable, Identifiable {
        case entry(Entry)
        case tools(ToolRun)

        /// A run's is its first call's, so it stays while calls append.
        public var id: Int {
            switch self {
            case .entry(let e): e.id
            case .tools(let r): r.id
            }
        }
    }

    /// Two or more consecutive tool calls.
    public struct ToolRun: Sendable, Equatable, Identifiable {
        public enum State: Sendable, Equatable {
            case running
            case failed
            case done
        }

        public let calls: [Entry]

        public var id: Int { calls[0].id }

        /// How many calls of each tool, in the order they first came.
        public var counts: [(tool: String, n: Int)] {
            var order: [String] = []
            var n: [String: Int] = [:]
            for c in calls {
                let t = c.tool.isEmpty ? "tool" : c.tool
                if n[t] == nil { order.append(t) }
                n[t, default: 0] += 1
            }
            return order.map { ($0, n[$0]!) }
        }

        /// "Read ×5 · Bash ×3 · Edit ×2".
        public var headline: String {
            counts.map { "\($0.tool) ×\($0.n)" }.joined(separator: " · ")
        }

        /// What VoiceOver hears: "8 tool calls: Read 5, Bash 3".
        public var spoken: String {
            "\(calls.count) tool calls: " + counts.map { "\($0.tool) \($0.n)" }.joined(separator: ", ")
        }

        /// Running while a call waits for its result, else failed when
        /// one failed, else done.
        public var state: State {
            if calls.contains(where: { $0.result == nil }) { return .running }
            if calls.contains(where: { $0.result?.error == true }) { return .failed }
            return .done
        }

        /// The last call while it still runs: shown under the folded
        /// header so its progress stays in sight.
        public var live: Entry? {
            calls.last.flatMap { $0.result == nil ? $0 : nil }
        }

        public func contains(_ id: Int) -> Bool {
            calls.contains { $0.id == id }
        }
    }

    /// The rows for entries: each run of two or more consecutive tool
    /// calls as one, the rest as they are.
    public static func rows(_ entries: some Collection<Entry>) -> [Row] {
        var rows: [Row] = []
        var run: [Entry] = []
        func flush() {
            if run.count > 1 {
                rows.append(.tools(ToolRun(calls: run)))
            } else if let e = run.first {
                rows.append(.entry(e))
            }
            run = []
        }
        for e in entries {
            if e.kind == .tool {
                run.append(e)
            } else {
                flush()
                rows.append(.entry(e))
            }
        }
        flush()
        return rows
    }

    /// The row an entry is drawn in: its own, or its run's.
    public static func row(of id: Int, in rows: [Row]) -> Int? {
        rows.first { r in
            switch r {
            case .entry(let e): e.id == id
            case .tools(let t): t.contains(id)
            }
        }?.id
    }
}
