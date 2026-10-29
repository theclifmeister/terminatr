import SwiftUI
import TerminatrKit
import TerminatrUI

/// The start screen: every need, as the Mac app's Needs you lists them
/// (Need.of), as a deck of answer cards (T847, direction A). Its header
/// (ScreenHeader) counts them and says where the phone is linked, with
/// Settings; its filter chips (PulseChips) show one kind (Need.Group).
/// Each card (NeedTile) opens its session, else its project, with its
/// answers in reach at the answer level (NeedActs); the first is the
/// lead card, lifted, whose answers a hardware keyboard's keys give. A
/// session's question or approval opens as its conversation with the
/// card above it (Route.need), beside the list while the Duo is open
/// (T826). VoiceOver's Needs rotor goes card to card; haptics follow
/// the switch (Haptics.key), and none comes when a need arrives.
struct NeedsScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    @AppStorage(Haptics.key) private var haptics = true
    /// -needs-filter GROUP opens on a chip, for screenshots.
    @State private var group = Need.Group(rawValue: UserDefaults.standard.string(forKey: "needs-filter") ?? "") ?? .all
    @Namespace private var rotor

    var body: some View {
        let needs = model.needs
        let shown = Need.filter(needs, group)
        let groups = Need.groups(needs)
        ScrollView {
            VStack(alignment: .leading, spacing: Spacing.row) {
                ScreenHeader("Needs you", count: needs.count, subtitle: subtitle.text, symbol: subtitle.symbol) {
                    SettingsButton()
                        .buttonStyle(.circle)
                }
                if groups.count > 2 {
                    PulseChips(groups.map { PulseChip(id: $0.group, label: $0.group.label($0.count), tone: $0.group.tone) },
                               selection: $group)
                        // Kept off a side safe area: a closed iPhone Duo held
                        // sideways has its clock beside the list.
                        .clipped()
                }
                LazyVStack(spacing: Spacing.card) {
                    SourceBanner()
                    if let f = model.failure { Failure(text: f) }
                    if needs.isEmpty {
                        ContentUnavailableView("Nothing needs you", systemImage: "checkmark.circle",
                                               description: Text("Questions, approvals and reviews show here."))
                            .padding(.top, 60)
                    }
                    ForEach(shown) { n in
                        NeedTile(need: n, route: route(n), lead: n.id == shown.first?.id)
                            .accessibilityRotorEntry(id: n.id, in: rotor)
                    }
                }
                .padding(.horizontal, Spacing.gutter)
                .padding(.top, Spacing.tight)
            }
            .padding(.bottom, Spacing.gutter)
        }
        .accessibilityRotor("Needs") {
            ForEach(shown) { n in
                AccessibilityRotorEntry(Text(n.title), id: n.id, in: rotor)
            }
        }
        .refreshable { await model.refresh() }
        .keepsPlace("needs")
        .kept($group, "needs filter")
        .environment(\.haptics, haptics)
        .navigationTitle("Needs you")
        .toolbar(.hidden, for: .navigationBar)
        .backdrop()
    }

    /// The header's line: the server and its link; the demo's banner
    /// says what it is, so none.
    private var subtitle: (text: String, symbol: String?) {
        guard case .server = model.source else { return ("", nil) }
        let (icon, words) = model.linkWords
        return ("\(model.serverName) · \(words)", icon)
    }

    private func route(_ n: Need) -> Route {
        if !n.session.isEmpty, let s = model.sessions.first(where: { $0.id == n.session }), let ref = SessionRef(s) {
            return n.kind == .question || n.kind == .approval ? .need(ref) : .session(ref)
        }
        if n.kind == .question, n.id.hasPrefix("questions:") { return .session(SessionRef(project: n.project)) }
        if let slash = n.project.firstIndex(of: "/") {
            return .peerProject(alias: String(n.project[..<slash]), project: String(n.project[n.project.index(after: slash)...]))
        }
        return .project(n.project)
    }
}

/// A need's card in the deck (NeedCard): its head opens route (the
/// chevron says so); its answers (NeedActs) below, outside the link,
/// and its VoiceOver actions. A paired server's need has none: it is
/// answered there. The lead card is lifted and takes the keys; the one
/// open beside the list (Columns) is ringed.
struct NeedTile: View {
    @Environment(\.navigate) private var navigate
    let need: Need
    let route: Route
    let lead: Bool

    var body: some View {
        NeedActs(need: need) { parts in
            TerminatrUI.NeedCard(need: need, command: parts.command, note: parts.note, lead: lead, open: open,
                                 acts: parts.acts) { head in
                PlainRouteLink(value: route) { head.opens() }
                    .buttonStyle(.plain)
                    .accessibilityHint(need.kind.verb)
            } answers: {
                parts.buttons
            }
        }
    }

    /// Its route is open beside the list (Columns).
    private var open: Bool { navigate?.open == route }
}

/// What the screens are of, on the start screen: the demo (and the way
/// back out). The paired server has none (T843): its name, link, level
/// and Forget are in Settings.
struct SourceBanner: View {
    @Environment(PhoneModel.self) private var model

    var body: some View {
        if model.source == .demo { DemoBanner() }
    }
}

/// The demo says so on its start screen, and leads back out.
struct DemoBanner: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: "play.rectangle")
                .foregroundStyle(theme.action)
                .accessibilityHidden(true)
            Text("Demo: recorded projects, read only")
                .font(theme.body(.footnote, .medium))
            Spacer()
            Button("Leave") { Task { await model.disconnect() } }
                .font(theme.body(.footnote, .semibold))
        }
        .padding(10)
        .background(theme.action.opacity(0.12), in: RoundedRectangle(cornerRadius: 10))
    }
}

struct Failure: View {
    @Environment(\.theme) private var theme
    let text: String

    var body: some View {
        Label(text, systemImage: "exclamationmark.triangle")
            .font(theme.body(.footnote))
            .foregroundStyle(theme.attention)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}
