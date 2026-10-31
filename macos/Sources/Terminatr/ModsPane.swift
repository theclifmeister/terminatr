import SwiftUI
import TerminatrKit
import TerminatrUI

/// The project's Mods page (T869), as the TUI's project popup Mods tab:
/// every installed mod in a list, with whether it runs here and where it
/// runs in the project's words; the picked one beside it with its
/// description, its button turning it on or off for this project, and
/// its settings for this project. Turning it on asks its consent first
/// when it is off or wants more, in tm's words, scrolled in full, and is
/// done only for what the sheet showed (tm refuses a stale one: the page
/// reads again and says so); a mod that runs for every project asks
/// before it is limited to the others. A tm without mods.set lists them
/// only.
struct ModsPane: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: String
    @State private var mods: [AppAPI.ProjectMod]?
    @State private var picked: String?
    @State private var failure: String?
    @State private var said: String?
    @State private var busy = false
    /// The mod whose consent shows, read with its lines.
    @State private var consenting: AppAPI.ProjectMod?
    /// The mod whose question shows (limit, off-all).
    @State private var asking: AppAPI.ProjectMod?

    var body: some View {
        HSplitView {
            List(selection: $picked) {
                ForEach(mods ?? []) { m in
                    ModRow(mod: m).tag(m.id)
                }
            }
            .frame(minWidth: 240, idealWidth: 300, maxWidth: 420)
            ScrollView {
                Group {
                    if let mods, let m = mods.first(where: { $0.id == picked }) {
                        detail(m)
                    } else if mods?.isEmpty == true {
                        Text("No mods installed: the TUI's settings (, then 4) install one.")
                            .foregroundStyle(theme.muted)
                    } else if let failure {
                        Text(failure).foregroundStyle(theme.attention)
                    } else {
                        ProgressView().controlSize(.small)
                    }
                }
                .padding(24)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .scrollEdges()
            .frame(minWidth: 360)
        }
        .task(id: project) { await list() }
        .sheet(item: $consenting) { m in
            ConsentSheet(mod: m) {
                consenting = nil
                Task { await set(m) }
            } cancel: {
                consenting = nil
                said = "unchanged"
            }
        }
        .alert(asking?.toggle.title ?? "", isPresented: Binding(get: { asking != nil }, set: { if !$0 { asking = nil } }), presenting: asking) { m in
            Button("Turn off", role: .destructive) { Task { await set(m) } }
            Button("Cancel", role: .cancel) { said = "unchanged" }
        } message: { m in
            Text(m.toggle.question)
        }
    }

    @ViewBuilder
    private func detail(_ m: AppAPI.ProjectMod) -> some View {
        VStack(alignment: .leading, spacing: 14) {
            VStack(alignment: .leading, spacing: 4) {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(m.name).font(theme.title(.title2))
                    if !m.version.isEmpty {
                        Text(m.version).font(theme.mono(.subheadline)).foregroundStyle(theme.muted)
                    }
                }
                Text(m.kinds).font(theme.body(.subheadline)).foregroundStyle(theme.muted)
            }
            if !m.description.isEmpty {
                Text(m.description).fixedSize(horizontal: false, vertical: true)
            }
            HStack(spacing: 12) {
                Text(m.where).foregroundStyle(ModConsentText.colour(m.whereTone, theme))
                Spacer(minLength: 8)
                if busy { ProgressView().controlSize(.small) }
                if app.changesMods, let words = ProjectMods.button(m) {
                    Button(words) { press(m) }
                        .disabled(busy)
                }
            }
            if case .refused(let why) = ProjectMods.act(m), m.required || m.toggle.do == "refuse" {
                Text(why).font(theme.body(.footnote)).foregroundStyle(theme.muted)
            }
            if let said {
                Text(said).font(theme.body(.footnote)).foregroundStyle(theme.muted)
            }
            if let failure {
                Text(failure).font(theme.body(.footnote)).foregroundStyle(theme.attention)
            }
            ForEach(m.bad, id: \.self) { b in
                Text(b).font(theme.body(.footnote)).foregroundStyle(theme.question)
            }
            Divider()
            Text("Settings for \(project)").font(theme.body(.headline))
            if ProjectMods.hasSettings(m) {
                Text("Each one this project doesn't set follows all projects.")
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
                ForEach(m.settings, id: \.key) { s in
                    ModSettingRow(s, change: app.changesMods ? { v in Task { await setting(m, s.key, v) } } : nil)
                    Divider()
                }
            } else {
                Text(m.settingsNote).font(theme.body(.subheadline)).foregroundStyle(theme.muted)
            }
        }
    }

    private func press(_ m: AppAPI.ProjectMod) {
        said = nil
        switch ProjectMods.act(m) {
        case .refused(let why): failure = why
        case .on, .off: Task { await set(m) }
        case .ask: asking = m
        case .consent: Task { await consent(m) }
        }
    }

    /// Reads the mod's consent and shows it: what Enable grants is what
    /// this read showed.
    private func consent(_ m: AppAPI.ProjectMod) async {
        do {
            let fresh = try await app.mods(project, consent: m.id)
            mods = fresh
            guard let f = fresh.first(where: { $0.id == m.id }) else { return }
            if case .consent = ProjectMods.act(f), !f.toggle.consent.isEmpty {
                consenting = f
            } else {
                said = "\(f.name) changed: look again"
            }
            failure = nil
        } catch {
            failure = error.localizedDescription
        }
    }

    private func set(_ m: AppAPI.ProjectMod) async {
        busy = true
        defer { busy = false }
        do {
            said = try await app.setMod(m, project: project)
            failure = nil
        } catch let e as AppAPI.Error where e.code == ProjectMods.changed {
            failure = e.message
        } catch {
            failure = error.localizedDescription
        }
        await list()
    }

    private func setting(_ m: AppAPI.ProjectMod, _ key: String, _ v: AppAPI.ModValue?) async {
        do {
            said = try await app.setModSetting(m, project: project, key: key, v)
            failure = nil
        } catch {
            failure = error.localizedDescription
        }
        await list()
    }

    private func list() async {
        do {
            let ms = try await app.mods(project)
            mods = ms
            if picked == nil || !ms.contains(where: { $0.id == picked }) { picked = ms.first?.id }
        } catch {
            failure = error.localizedDescription
        }
    }
}

/// A mod's row: ● where it runs here, its name and what it provides,
/// where it runs and what of its settings the project sets.
private struct ModRow: View {
    @Environment(\.theme) private var theme
    let mod: AppAPI.ProjectMod

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: mod.runs ? "circle.fill" : "circle")
                .font(.system(size: 8))
                .foregroundStyle(mod.runs ? theme.good : theme.muted)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(mod.name).lineLimit(1)
                    Text(mod.kinds).font(theme.body(.caption)).foregroundStyle(theme.muted).lineLimit(1)
                }
                Text([mod.where, mod.own].filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(theme.body(.caption))
                    .foregroundStyle(ModConsentText.colour(mod.whereTone, theme))
                    .lineLimit(1)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// A mod's consent in a sheet: tm's lines in full, scrolled, then
/// Enable (or Grant, for what an enabled mod wants more) and Cancel.
private struct ConsentSheet: View {
    @Environment(\.theme) private var theme
    let mod: AppAPI.ProjectMod
    let enable: () -> Void
    let cancel: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ScrollView {
                ModConsentText(mod.toggle.consent)
                    .padding(24)
            }
            .frame(minWidth: 520, idealWidth: 600, minHeight: 360, idealHeight: 520)
            Divider()
            HStack {
                Spacer()
                Button("Cancel", role: .cancel, action: cancel)
                    .keyboardShortcut(.cancelAction)
                Button(mod.toggle.question.hasPrefix("Grant") ? "Grant" : "Enable", action: enable)
            }
            .padding(16)
        }
    }
}
