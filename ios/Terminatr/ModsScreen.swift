import SwiftUI
import TerminatrKit
import TerminatrUI

/// A project's mods (T869), as the Mac's Mods page and the TUI's project
/// Mods tab: every installed mod with whether it runs here and where it
/// runs in the project's words, each opening its own screen. Read at the
/// view level; turning one on or off and its settings wait for the write
/// level (mods.set, signed).
struct ModsScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let slug: String
    @State private var mods: [AppAPI.ProjectMod]?
    @State private var failure: String?

    var body: some View {
        List {
            if let failure { Failure(text: failure) }
            if let mods {
                if mods.isEmpty {
                    Text("No mods installed: the TUI's settings (, then 4) install one.")
                        .foregroundStyle(theme.muted)
                }
                Section {
                    ForEach(mods) { m in
                        RouteLink(value: Route.mod(slug, id: m.id)) { ModLine(mod: m) }
                    }
                } footer: {
                    if !model.changesMods {
                        Text("Turning a mod on or off here needs the write level: Settings › Devices on the Mac.")
                    }
                }
            } else if failure == nil {
                ProgressView()
            }
        }
        .task(id: model.generation) { await list() }
        .refreshable { await list() }
        .keepsPlace(Route.mods(slug).listKey)
        .navigationTitle("Mods")
        .navigationBarTitleDisplayMode(.inline)
        .backdrop()
    }

    private func list() async {
        do {
            mods = try await model.mods(slug)
            failure = nil
        } catch {
            failure = error.localizedDescription
        }
    }
}

/// A mod's line in the list: ● where it runs here, its name and what it
/// provides, where it runs and what of its settings the project sets.
struct ModLine: View {
    @Environment(\.theme) private var theme
    let mod: AppAPI.ProjectMod

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Image(systemName: mod.runs ? "circle.fill" : "circle")
                .font(.system(size: 9))
                .foregroundStyle(mod.runs ? theme.good : theme.muted)
            VStack(alignment: .leading, spacing: 2) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(mod.name).font(theme.body(.body, .medium)).lines(1)
                    Text(mod.kinds).font(theme.body(.caption)).foregroundStyle(theme.muted).lines(1)
                }
                Text([mod.where, mod.own].filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(theme.body(.footnote))
                    .foregroundStyle(ModConsentText.colour(mod.whereTone, theme))
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// One of a project's mods (T869): its description, where it runs, its
/// button turning it on or off for the project, and its settings for
/// the project. Turning it on asks its consent first when it is off or
/// wants more: tm's lines in full in a sheet, scrolled, Enable at the
/// end; what Enable grants is what the sheet showed (tm refuses a stale
/// one: the screen reads again and says so). A mod that runs for every
/// project asks before it is limited to the others.
struct ModScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let slug: String
    let id: String
    @State private var mod: AppAPI.ProjectMod?
    @State private var failure: String?
    @State private var said: String?
    @State private var busy = false
    @State private var consenting: AppAPI.ProjectMod?
    @State private var asking = false

    var body: some View {
        List {
            if let failure { Failure(text: failure) }
            if let m = mod {
                Section {
                    VStack(alignment: .leading, spacing: 6) {
                        Text(m.kinds + (m.version.isEmpty ? "" : " · " + m.version))
                            .font(theme.body(.footnote))
                            .foregroundStyle(theme.muted)
                        if !m.description.isEmpty {
                            Text(m.description).fixedSize(horizontal: false, vertical: true)
                        }
                        Text(m.where)
                            .font(theme.body(.subheadline, .medium))
                            .foregroundStyle(ModConsentText.colour(m.whereTone, theme))
                    }
                    .padding(.vertical, 2)
                    if model.changesMods, let words = ProjectMods.button(m) {
                        Button {
                            press(m)
                        } label: {
                            HStack {
                                Text(words)
                                Spacer()
                                if busy { ProgressView() }
                            }
                        }
                        .disabled(busy)
                    }
                    if case .refused(let why) = ProjectMods.act(m) {
                        Text(why).font(theme.body(.footnote)).foregroundStyle(theme.muted)
                    }
                    if let said {
                        Text(said).font(theme.body(.footnote)).foregroundStyle(theme.muted)
                    }
                    ForEach(m.bad, id: \.self) { b in
                        Text(b).font(theme.body(.footnote)).foregroundStyle(theme.question)
                    }
                }
                Section {
                    if ProjectMods.hasSettings(m) {
                        ForEach(m.settings, id: \.key) { s in
                            ModSettingRow(s, change: model.changesMods ? { v in Task { await setting(m, s.key, v) } } : nil)
                        }
                    } else {
                        Text(m.settingsNote).foregroundStyle(theme.muted)
                    }
                } header: {
                    Text("Settings for \(slug)")
                } footer: {
                    if ProjectMods.hasSettings(m) {
                        Text("Each one this project doesn't set follows all projects.")
                    }
                }
            } else if failure == nil {
                ProgressView()
            }
        }
        .task(id: model.generation) { await read() }
        .refreshable { await read() }
        .navigationTitle(mod?.name ?? id)
        .navigationBarTitleDisplayMode(.inline)
        .backdrop()
        .sheet(item: $consenting) { m in
            PhoneConsentSheet(mod: m) {
                consenting = nil
                Task { await set(m) }
            } cancel: {
                consenting = nil
                said = "unchanged"
            }
        }
        .confirmationDialog(mod?.toggle.title ?? "", isPresented: $asking, titleVisibility: .visible, presenting: mod) { m in
            Button("Turn off", role: .destructive) { Task { await set(m) } }
            Button("Cancel", role: .cancel) { said = "unchanged" }
        } message: { m in
            Text(m.toggle.question)
        }
    }

    private func press(_ m: AppAPI.ProjectMod) {
        said = nil
        switch ProjectMods.act(m) {
        case .refused(let why): failure = why
        case .on, .off: Task { await set(m) }
        case .ask: asking = true
        case .consent: Task { await consent(m) }
        }
    }

    /// Reads the consent again and shows it.
    private func consent(_ m: AppAPI.ProjectMod) async {
        do {
            guard let f = try await model.mods(slug, consent: m.id).first(where: { $0.id == m.id }) else { return }
            mod = f
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
            said = try await model.setMod(m, project: slug)
            failure = nil
        } catch {
            failure = error.localizedDescription
        }
        await read()
    }

    private func setting(_ m: AppAPI.ProjectMod, _ key: String, _ v: AppAPI.ModValue?) async {
        do {
            said = try await model.setModSetting(m, project: slug, key: key, v)
            failure = nil
        } catch {
            failure = error.localizedDescription
        }
        await read()
    }

    private func read() async {
        do {
            mod = try await model.mods(slug).first { $0.id == id }
            if mod == nil { failure = "\(id) is no longer installed." }
        } catch {
            failure = error.localizedDescription
        }
    }
}

/// A mod's consent on the phone: tm's lines in full, scrolled, the
/// question at their end, then Enable (Grant, for what an enabled mod
/// wants more) and Cancel.
struct PhoneConsentSheet: View {
    @Environment(\.theme) private var theme
    let mod: AppAPI.ProjectMod
    let enable: () -> Void
    let cancel: () -> Void

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(mod.name, leading: {
                Button("Cancel", action: cancel).buttonStyle(.ghost)
            }, trailing: { EmptyView() })
            ScrollView {
                ModConsentText(mod.toggle.consent)
                    .padding(.horizontal, Spacing.gutter)
                    .padding(.bottom, Spacing.gutter)
            }
            Button(mod.toggle.question.hasPrefix("Grant") ? "Grant" : "Enable", action: enable)
                .buttonStyle(.primary())
                .padding(Spacing.gutter)
        }
        .presentationDetents([.large])
        .backdrop()
    }
}
