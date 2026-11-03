import Foundation

/// The conversation scroll tests' demo (T976): the demo's recording
/// with a long coordinator conversation and a thread that streams.
///
/// - The coordinator's (`coordinator`, acme-web's) is `long` items:
///   prompts, the agent's answers from a line to a long Markdown card,
///   runs of tool calls, so its rows differ in height as a real one's.
///   It comes as over a relay: its first part `delay` seconds after the
///   screen opens, the rest (`late` items) in a few parts after.
/// - The working thread's (`streaming`, acme-web/t-0104) opens with
///   `long / 2` items, then one more answer comes every `drip` seconds,
///   `streamed` of them.
///
/// The last item of each says "Message N." first, N its index, so a
/// test knows the last message by its words.
public enum ScrollFixture {
    public static let coordinator = "s-c1"
    public static let streaming = "s-104"
    public static let long = 240
    public static let streamed = 400
    public static let drip = 0.35
    public static let delay = 1.2
    public static let late = 12

    /// The recording, from the demo's.
    public static func recording(from demo: Data) throws -> Data {
        guard var r = try JSONSerialization.jsonObject(with: demo) as? [String: Any],
              var transcripts = r["transcripts"] as? [String: Any] else {
            throw BridgeError.badLine("the demo's recording has no transcripts")
        }
        var coord = stream(transcripts[coordinator], items: items(long - late))
        coord["delay"] = delay
        coord["drip"] = 0.25
        var parts = coord["events"] as? [[String: Any]] ?? []
        for i in stride(from: long - late, to: long, by: 4) {
            parts.append(["event": "transcript.items", "data": ["items": (i..<min(i + 4, long)).map(item)]])
        }
        coord["events"] = parts
        transcripts[coordinator] = coord
        var thread = stream(transcripts[streaming], items: items(long / 2))
        thread["drip"] = drip
        var events = thread["events"] as? [[String: Any]] ?? []
        for i in 0..<streamed {
            events.append(["event": "transcript.items", "data": ["items": [item(long / 2 + i)]]])
        }
        thread["events"] = events
        transcripts[streaming] = thread
        r["transcripts"] = transcripts
        return try JSONSerialization.data(withJSONObject: r, options: [.sortedKeys])
    }

    /// The words the n-th item starts with.
    public static func words(_ n: Int) -> String { "Message \(n)." }

    private static func stream(_ was: Any?, items: [[String: Any]]) -> [String: Any] {
        var s = was as? [String: Any] ?? [:]
        if s["result"] == nil { s["result"] = [String: Any]() }
        s["events"] = [["event": "transcript.reset", "data": ["items": items, "status": ["agent": "claude", "state": "mapped"]]]]
        return s
    }

    private static func items(_ n: Int) -> [[String: Any]] {
        (0..<n).map(item)
    }

    /// Item n: a run of tool calls every seventh, a prompt every fifth,
    /// else the agent's text, its length cycling from a line to a long
    /// card with a list and code; the last of any count is text.
    private static func item(_ n: Int) -> [String: Any] {
        let at = "2026-10-27T08:\(String(format: "%02d", (n / 60) % 60)):\(String(format: "%02d", n % 60))Z"
        let lines = [1, 3, 8, 2, 14, 5][n % 6]
        var text = words(n)
        for l in 0..<lines {
            switch l % 4 {
            case 0: text += " The checkout flow reads the payments flag before it draws the button, and the tests cover both ways."
            case 1: text += "\n\n- step \(l): read the config\n- step \(l + 1): run the tests"
            case 2: text += "\n\n```\nnpm test -- cart --repeat \(l)\n```"
            default: text += "\n\nA **bold** word, a `code` one, and a line long enough to wrap on the narrowest phone in the list."
            }
        }
        if n % 7 == 3 {
            return ["call": "f\(n)", "kind": "tool", "text": "src/checkout/pay\(n).ts", "tool": "Read"]
        }
        if n % 5 == 1 {
            return ["at": at, "kind": "prompt", "text": "\(words(n)) What's next?"]
        }
        return ["at": at, "kind": "text", "text": text]
    }
}
