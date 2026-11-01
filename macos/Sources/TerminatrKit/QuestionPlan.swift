import Foundation

/// What a question card offers for one question of a session's open
/// menu, and how its answer goes (session.answer). tm's own question
/// (an approved command that asks first) takes any of its questions in
/// any order, several options too. The agent's own menu (screen:
/// Claude's AskUserQuestion) is typed on its screen, so one question at
/// a time, in order, a single option or the user's words; a
/// multi-select's options go as picks where tm types them (app API 39,
/// Compatibility.answersPicks), else in the Terminal tab, and its own
/// words only there.
public struct QuestionPlan: Equatable, Sendable {
    /// The cards a menu shows: every open question of tm's own, the
    /// first open one of the agent's (its screen shows only that one).
    public static func shown(_ q: AppAPI.Question) -> [Int] {
        let open = q.questions.indices.filter { !q.questions[$0].answered }
        return q.screen ? Array(open.prefix(1)) : open
    }

    /// The card's tag: the question's header, its place among several
    /// ("Question 2 of 3"), and an escape's mark.
    public static func tag(_ q: AppAPI.Question, index: Int) -> String {
        if q.escape { return "Question · outside the sandbox" }
        let header = q.questions[index].header
        var parts = [header.isEmpty ? "Question" : header]
        if q.questions.count > 1 { parts.append("\(index + 1) of \(q.questions.count)") }
        return parts.joined(separator: " · ")
    }

    /// The menu's options as buttons, each a guarded answer.
    public let buttons: Bool
    /// The options as checkboxes with Submit, the labels joined.
    public let checkboxes: Bool
    /// The checkboxes can't be sent: the user picks them in the pane.
    public let paneOnly: Bool
    /// "Other…": the user's own words.
    public let words: Bool

    /// picks: the server takes a multi-select's options by index
    /// (Compatibility.answersPicks).
    public init(_ q: AppAPI.Question, index: Int, picks: Bool = false) {
        let item = q.questions[index]
        buttons = !item.multiSelect
        checkboxes = item.multiSelect
        paneOnly = q.screen && item.multiSelect && !picks
        sendsPicks = q.screen && item.multiSelect && picks
        words = !(q.screen && item.multiSelect)
    }

    /// The checkboxes go as picks, typed into the agent's menu.
    public let sendsPicks: Bool

    /// What the card says about it under the options, if anything.
    public var note: String? {
        paneOnly ? "This question takes several options, which the app can't type into the agent's menu: pick them in the Terminal tab." : nil
    }

    /// A multi-select's answer: the picked labels in the menu's order,
    /// joined with ", " as the agent joins them.
    public static func joined(_ item: AppAPI.QuestionItem, picked: Set<String>) -> String {
        item.options.map(\.label).filter(picked.contains).joined(separator: ", ")
    }

    /// A multi-select's picks: the indexes of the labels picked, in the
    /// menu's order.
    public static func picks(_ item: AppAPI.QuestionItem, picked: Set<String>) -> [Int] {
        item.options.indices.filter { picked.contains(item.options[$0].label) }
    }

    /// An option for VoiceOver: its number, label and description.
    public static func spoken(_ item: AppAPI.QuestionItem, option i: Int) -> String {
        let o = item.options[i]
        let head = "Option \(i + 1) of \(item.options.count): \(o.label)"
        return o.description.isEmpty ? head : "\(head). \(o.description)"
    }

    /// The card's words while an answer is in flight, or once the server
    /// took it: never a spinner alone.
    public static func sending(_ answer: String, screen: Bool) -> String {
        screen ? "Typing “\(answer)” into the agent's menu…" : "Sending “\(answer)”…"
    }

    /// The line under a phone card's options after a tap (T887): the
    /// server's refusal (said, failed) first; while the answer goes, how
    /// it goes (sending); once taken, nothing (the card goes with the
    /// next session list). Nil when nothing was tapped or said.
    public static func tapped(sending option: String?, busy: Bool, screen: Bool,
                              said: (text: String, failed: Bool)?) -> (text: String, failed: Bool)? {
        if let said { return said }
        guard busy, let option else { return nil }
        return (sending(option, screen: screen), false)
    }

    /// The menu's state in words once every question has its answer but
    /// it is still open: the agent hasn't taken them yet, or its menu
    /// waits on the screen (a review step).
    public static func allAnswered(_ q: AppAPI.Question) -> String? {
        guard !q.questions.isEmpty, q.questions.allSatisfy(\.answered) else { return nil }
        return q.screen
            ? "Answers typed. If the agent's menu is still open (it may ask you to submit them), finish it in the Terminal tab."
            : "Answers sent: waiting for the agent to take them."
    }

    /// Whether an answer's failure is one the Terminal tab answers
    /// instead: the screen showed another menu, or the shape is the
    /// pane's.
    public static func useTerminal(_ code: String) -> Bool {
        ["screen-only", "no-question"].contains(code)
    }

    /// Whether an answer the server refused with code was to a question
    /// gone meanwhile (answered in the pane, the menu closed): no-question,
    /// and the session's question as listed after it (now) no longer
    /// asks it unanswered. The card then goes quietly (QuestionGone); a
    /// menu still open whose screen didn't match keeps the error.
    public static func gone(_ code: String, asked: String, now: AppAPI.Question?) -> Bool {
        guard code == "no-question" else { return false }
        return !(now?.questions.contains { !$0.answered && (asked.isEmpty || $0.question == asked) } ?? false)
    }
}

/// An answer to a question gone meanwhile (QuestionPlan.gone): the card
/// it came from goes with the next session list, and says nothing.
public struct QuestionGone: Error, Equatable, Sendable {
    public init() {}
}

extension Transcript.Entry {
    /// The tool call puts a question menu to the user (AskUserQuestion).
    public var asksUser: Bool { kind == .tool && tool == "AskUserQuestion" }

    /// How a question menu ended, once its result came: each question
    /// answered, as the result's answers say (app API 40), or declined
    /// (the call failed: the user cancelled it) with the result's line.
    /// Nil for any other entry, and for an answered menu a tm before
    /// app API 40 sent without its answers: its tool line shows.
    public enum QuestionOutcome: Sendable, Equatable {
        case answered([AppAPI.TranscriptAnswer])
        case declined(String)
    }

    public var questionOutcome: QuestionOutcome? {
        guard let r = result else { return nil }
        if !r.answers.isEmpty { return .answered(r.answers) }
        if asksUser && r.error { return .declined(r.text) }
        return nil
    }
}

extension AppAPI.TranscriptAnswer {
    /// What the user answered, in words: the labels picked, then their
    /// own words in quotes ("Server, Mac app, “docs too”").
    public var words: String {
        (picked + (typed.isEmpty ? [] : ["\u{201C}\(typed)\u{201D}"])).joined(separator: ", ")
    }

    /// What VoiceOver hears for it: "Parts: Which parts? Answered Server, Mac app."
    public var spoken: String {
        let q = header.isEmpty ? question : "\(header): \(question)"
        return "\(q) Answered \(words.isEmpty ? "nothing" : words)." + (notes.isEmpty ? "" : " Notes: \(notes).")
    }
}
