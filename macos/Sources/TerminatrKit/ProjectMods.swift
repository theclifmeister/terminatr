import Foundation

/// A project's Mods page (T869), as the TUI's project popup Mods tab
/// (mods.list, mods.set, mods.setting, app API 38): every installed mod
/// with where it runs for the project, its button turning it on or off
/// there, what is asked first (the mod's consent, in tm's lines, or the
/// question limiting a mod that runs for every project), and its
/// settings for the project. The words are tm's; this says which
/// button, which question, and how a setting reads and changes.
public enum ProjectMods {
    /// What a mod's button does, from its toggle's do.
    public enum Act: Equatable, Sendable {
        /// Nothing: why says why (invalid, or tm can't do without it).
        case refused(why: String)
        /// Off for the project at once.
        case off
        /// On for the project at once: it runs for others and wants
        /// nothing more.
        case on
        /// Off for the project after a question (title, question): it
        /// runs for every project.
        case ask(title: String, question: String)
        /// On for the project after its consent: the consent's lines
        /// read with mods.list's consent, then question.
        case consent(question: String)
    }

    public static func act(_ m: AppAPI.ProjectMod) -> Act {
        let t = m.toggle
        switch t.do {
        case "off": return .off
        case "on": return .on
        case "limit", "off-all": return .ask(title: t.title, question: t.question)
        case "consent": return .consent(question: t.question)
        default: return .refused(why: t.why.isEmpty ? "\(m.name) can't be changed here" : t.why)
        }
    }

    /// Whether the button turns the mod on (else off).
    public static func turnsOn(_ m: AppAPI.ProjectMod) -> Bool {
        m.toggle.do == "on" || m.toggle.do == "consent"
    }

    /// The button's words: "Turn on", "Turn on…" (it asks first), "Turn
    /// off", "Turn off…"; nil for a mod that can't change here.
    public static func button(_ m: AppAPI.ProjectMod) -> String? {
        switch act(m) {
        case .refused: nil
        case .on: "Turn on"
        case .consent: "Turn on…"
        case .off: "Turn off"
        case .ask: "Turn off…"
        }
    }

    /// mods.set's params for the button, with the key of what was asked.
    public static func set(_ m: AppAPI.ProjectMod, project: String) -> AppAPI.ModsSetParams {
        AppAPI.ModsSetParams(project: project, mod: m.id, on: turnsOn(m), key: m.toggle.key)
    }

    /// The consent's words, a line each: what a test, or VoiceOver,
    /// reads.
    public static func text(_ lines: [AppAPI.ConsentLine]) -> String {
        lines.map { $0.spans.map(\.text).joined() }.joined(separator: "\n")
    }

    /// Whether the mod shows a settings form.
    public static func hasSettings(_ m: AppAPI.ProjectMod) -> Bool { !m.settings.isEmpty }

    /// A setting's value as the TUI shows it: on/off, a number, a text
    /// or choice as it is.
    public static func words(_ v: AppAPI.ModValue) -> String {
        switch v.type {
        case "bool": v.bool ? "on" : "off"
        case "int": String(v.int)
        default: v.text
        }
    }

    /// "Default 3." and its label first, as the TUI's settings row.
    public static func help(_ s: AppAPI.ModSetting) -> String {
        let d = "Default \(s.type == "string" || s.type == "choice" ? "\"\(words(s.default))\"" : words(s.default))."
        return s.label.isEmpty ? d : "\(s.label). \(d)"
    }

    /// The setting's range in words, "" for none: "1 to 9".
    public static func range(_ s: AppAPI.ModSetting) -> String {
        switch (s.hasMin, s.hasMax) {
        case (true, true): "\(s.min) to \(s.max)"
        case (true, false): "at least \(s.min)"
        case (false, true): "at most \(s.max)"
        default: ""
        }
    }

    /// Whether n is in the setting's range.
    public static func fits(_ s: AppAPI.ModSetting, _ n: Int) -> Bool {
        (!s.hasMin || n >= s.min) && (!s.hasMax || n <= s.max)
    }

    /// A value for setting s.
    public static func bool(_ b: Bool) -> AppAPI.ModValue { AppAPI.ModValue(type: "bool", bool: b) }
    public static func int(_ n: Int) -> AppAPI.ModValue { AppAPI.ModValue(type: "int", int: n) }
    public static func text(_ t: String) -> AppAPI.ModValue { AppAPI.ModValue(type: "text", text: t) }

    /// mods.setting's params: key of mod m for project to value, or with
    /// nil back to all projects' value.
    public static func setting(_ m: AppAPI.ProjectMod, project: String, key: String, _ value: AppAPI.ModValue?) -> AppAPI.ModsSettingParams {
        AppAPI.ModsSettingParams(project: project, mod: m.id, key: key, value: value)
    }

    /// What a setting's row says beside its value: "all projects" while
    /// the project follows them, else nothing.
    public static func from(_ s: AppAPI.ModSetting) -> String { s.own ? "" : "all projects" }

    /// The error code mods.set answers when what it would do, or ask,
    /// changed since the page read it: read the list again.
    public static let changed = "mod-changed"
}
