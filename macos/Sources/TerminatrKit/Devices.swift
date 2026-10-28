import Foundation

/// Settings › Devices (T790, app API 26): the words the devices' list
/// and the Pair iPhone… sheet show, as tm device list and tm device pair
/// say them.
public enum DeviceWords {
    /// The levels by name; the app offers those device.list lists
    /// (view, answer and write).
    public static let view = "view"
    public static let answer = "answer"

    /// A level as the user reads it.
    public static func level(_ level: String) -> String {
        switch level {
        case answer: "Answer"
        case "write": "Write"
        default: "View only"
        }
    }

    /// What a level lets the phone do.
    public static func levelHelp(_ level: String) -> String {
        switch level {
        case answer:
            "Reads, answers questions and approvals (never one that runs a command outside the sandbox), and accepts or sends back tasks, each act behind Face ID."
        case "write":
            "Does what this app does: prompts, new tasks, delegating, starting a coordinator, terminal keys and the rest, each act signed by its write key. Never an approval that runs a command outside the sandbox."
        default:
            "Reads projects, sessions, conversations and panes, and acts on nothing."
        }
    }

    /// Why d can't answer, or runs below its level; nil when it runs at
    /// its level and can be raised.
    public static func limit(_ d: AppAPI.Device) -> String? {
        if !d.writeKey {
            return "No write key: it can only view. Pair it again from the terminatr app to let it answer."
        }
        if d.effective != d.level {
            return "\(level(d.level)) isn't in this release: it runs at \(level(d.effective).lowercased())."
        }
        return nil
    }

    /// A device's link, as tm device list says it: a phone that isn't
    /// open is waited for, not failing.
    public static func link(_ d: AppAPI.Device, running: Bool, now: Date = .now) -> String {
        guard let l = d.link else {
            return running ? "No link yet" : "The server isn't running"
        }
        switch (l.state, l.reason) {
        case ("up", _):
            return "Connected " + since(l.since, now: now)
        case ("offline", "peer"):
            return "Waiting for it"
        case ("offline", _):
            return "Offline" + (l.error.isEmpty ? "" : ": " + l.error)
        default:
            return l.state.prefix(1).uppercased() + l.state.dropFirst()
        }
    }

    /// Whether d's link is up.
    public static func connected(_ d: AppAPI.Device) -> Bool { d.link?.state == "up" }

    /// How long ago t was, roughly.
    public static func since(_ t: Date, now: Date) -> String {
        let s = Int(now.timeIntervalSince(t))
        if s < 60 { return "just now" }
        if s < 3600 { return "for \(s / 60) min" }
        if s < 86400 { return "for \(s / 3600) h \(s % 3600 / 60) min" }
        return "for \(s / 86400) d"
    }

    /// What is left until end, as m:ss; 0:00 once it passed.
    public static func countdown(to end: Date, now: Date = .now) -> String {
        let s = max(0, Int(end.timeIntervalSince(now).rounded(.up)))
        return "\(s / 60):" + String(format: "%02d", s % 60)
    }

    /// The confirmation tm device pair asks, before its question: the
    /// phone, whether it pairs again, and whether it has a write key.
    public static func offer(_ o: AppAPI.DeviceOffer) -> [String] {
        var lines = ["“\(o.phone)”, device id \(o.short), app \(o.version.isEmpty ? "unknown" : o.version)"]
        if !o.again.isEmpty {
            lines.append("This phone is paired already, as “\(o.again)”: pairing again keeps its level.")
        }
        if !o.writeKey {
            lines.append("Its app has no write key: it can only view, whatever its level.")
        }
        return lines
    }

    /// The question itself: "Add it as …, a view-only device?"
    public static func question(name: String, level: String) -> String {
        let l = level == answer ? "an answer" : level == "write" ? "a write" : "a view-only"
        return "Add it as “\(name)”, \(l) device?"
    }

    /// A device method's error as the app says it: the codes the bridge
    /// answers with, in the app's words, else its message.
    public static func error(_ e: Error) -> String {
        guard let e = e as? AppAPI.Error else { return e.localizedDescription }
        switch e.code {
        case "pair-expired": return "The code's time ran out before the phone joined. Make a new code to try again."
        case "pair-ended": return "The pairing ended. Make a new code to try again."
        case "declined": return e.message.isEmpty ? "Not paired." : e.message
        case "bad-code": return "The phone and this server didn't agree on the code: someone in between, or another server. Make a new code."
        case "human-only": return "Only you pair and change devices, from your own account."
        default: return e.message.isEmpty ? e.code : e.message
        }
    }
}
