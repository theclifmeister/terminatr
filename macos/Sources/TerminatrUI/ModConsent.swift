import SwiftUI
import TerminatrKit

/// A mod's consent (T869) as tm asks it, line by line in tm's words
/// (mods.list's consent, the TUI's consent screen): what it is and
/// provides, what it changes in what agents run or read, each permission
/// it asks for in its colour, the defaults it suggests, every rules
/// addendum, agent manifest, task template, tool and guard rule in full
/// (in mono), then its question. The caller scrolls it.
public struct ModConsentText: View {
    @Environment(\.theme) private var theme
    let lines: [AppAPI.ConsentLine]

    public init(_ lines: [AppAPI.ConsentLine]) {
        self.lines = lines
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            ForEach(Array(lines.enumerated()), id: \.offset) { _, l in
                line(l)
            }
        }
        .textSelection(.enabled)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder
    private func line(_ l: AppAPI.ConsentLine) -> some View {
        let words = l.spans.reduce(Text("")) { t, s in t + span(s) }
        switch l.fit {
        case "hard":
            words.font(theme.mono(.footnote)).fixedSize(horizontal: false, vertical: true)
        case "cut":
            words.font(font(l)).lineLimit(1).truncationMode(.tail)
        default:
            if l.spans.allSatisfy({ $0.text.isEmpty }) {
                Text(" ").font(theme.body(.caption))
            } else {
                words.font(font(l)).fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    private func font(_ l: AppAPI.ConsentLine) -> Font {
        l.spans.first?.tone == "head" ? theme.body(.body, .semibold) : theme.body(.subheadline)
    }

    private func span(_ s: AppAPI.ConsentSpan) -> Text {
        Text(s.text).foregroundColor(colour(s.tone))
    }

    /// A tone's colour: tm's theme's head, faint, good, warn, bad, info.
    public func colour(_ tone: String) -> Color {
        Self.colour(tone, theme)
    }

    public static func colour(_ tone: String, _ theme: Theme) -> Color {
        switch tone {
        case "faint": theme.muted
        case "good": theme.good
        case "warn": theme.question
        case "bad": theme.attention
        case "info": theme.action
        default: theme.ink
        }
    }
}

/// One of a mod's settings for a project (T869), as the TUI's settings
/// row: its key, label and default, its value (a switch, a stepper, a
/// choice or a text), "all projects" while the project follows them,
/// and Follow all projects once it sets its own. change is mods.setting
/// with the new value, nil to follow all projects again; nil while the
/// page only reads.
public struct ModSettingRow: View {
    @Environment(\.theme) private var theme
    let setting: AppAPI.ModSetting
    let change: ((AppAPI.ModValue?) -> Void)?
    @State private var draft = ""

    public init(_ setting: AppAPI.ModSetting, change: ((AppAPI.ModValue?) -> Void)?) {
        self.setting = setting
        self.change = change
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                Text(setting.key).font(theme.mono(.subheadline, .medium))
                Spacer(minLength: 8)
                control
            }
            Text(ProjectMods.help(setting) + (ProjectMods.range(setting).isEmpty ? "" : " " + ProjectMods.range(setting).capitalizedFirst + "."))
                .font(theme.body(.footnote))
                .foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
            HStack {
                if setting.own {
                    Text("set for this project").font(theme.body(.footnote)).foregroundStyle(theme.good)
                    if let change {
                        Button("Follow all projects") { change(nil) }
                            .buttonStyle(.borderless)
                            .font(theme.body(.footnote))
                    }
                } else {
                    Text(ProjectMods.from(setting)).font(theme.body(.footnote)).foregroundStyle(theme.muted)
                }
            }
        }
        .padding(.vertical, 4)
        .onAppear { draft = ProjectMods.words(setting.value) }
        .onChange(of: setting.value) { draft = ProjectMods.words(setting.value) }
        .accessibilityElement(children: .contain)
    }

    @ViewBuilder
    private var control: some View {
        let ro = change == nil
        switch setting.type {
        case "bool":
            Toggle(setting.key, isOn: Binding(get: { setting.value.bool }, set: { change?(ProjectMods.bool($0)) }))
                .labelsHidden()
                .disabled(ro)
        case "int":
            Stepper(value: Binding(get: { setting.value.int }, set: { n in
                if ProjectMods.fits(setting, n) { change?(ProjectMods.int(n)) }
            })) {
                Text(String(setting.value.int)).font(theme.mono(.subheadline))
            }
            .fixedSize()
            .disabled(ro)
        case "choice":
            Picker(setting.key, selection: Binding(get: { setting.value.text }, set: { change?(ProjectMods.text($0)) })) {
                ForEach(setting.options, id: \.self) { Text($0).tag($0) }
            }
            .labelsHidden()
            .fixedSize()
            .disabled(ro)
        default:
            if ro {
                Text(setting.value.text).font(theme.mono(.subheadline)).lineLimit(1)
            } else {
                TextField(setting.key, text: $draft)
                    .labelsHidden()
                    .textFieldStyle(.roundedBorder)
                    .frame(maxWidth: 260)
                    .onSubmit { save() }
                if draft != setting.value.text {
                    Button("Save") { save() }.buttonStyle(.borderless)
                }
            }
        }
    }

    private func save() {
        let t = String(draft.prefix(setting.maxLen > 0 ? setting.maxLen : 500))
        if t != setting.value.text { change?(ProjectMods.text(t)) }
    }
}

extension String {
    var capitalizedFirst: String { prefix(1).uppercased() + dropFirst() }
}
