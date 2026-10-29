import SwiftUI
import TerminatrKit
import TerminatrUI

/// The start screen: every need, as the Mac app's Needs you lists them
/// (Need.of), each a compact card (NeedSummary) that opens its session,
/// else its project, with its answers below at the answer level
/// (NeedActs): on a closed iPhone Duo one tap answers the quick ones. A
/// session's question or approval opens as its conversation with the
/// card above it (Route.need), beside the list while the Duo is open
/// (T826).
struct NeedsScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme

    var body: some View {
        let needs = model.needs
        ScrollView {
            LazyVStack(spacing: 10) {
                SourceBanner()
                if let f = model.failure { Failure(text: f) }
                if needs.isEmpty {
                    ContentUnavailableView("Nothing needs you", systemImage: "checkmark.circle",
                                           description: Text("Questions, approvals and reviews show here."))
                        .padding(.top, 60)
                }
                ForEach(needs) { n in
                    NeedTile(need: n, route: route(n))
                }
            }
            .padding(16)
        }
        .refreshable { await model.refresh() }
        .keepsPlace("needs")
        .navigationTitle("Needs you")
        .toolbar { SettingsButton() }
        .backdrop()
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

/// A need's card, as the Mac dashboard's NeedCard looks: its project,
/// then NeedSummary, which open route; below, outside the link, its
/// buttons (NeedActs). The agent's words take two lines, one on a short
/// screen (a closed iPhone Duo held sideways), so the buttons stay in
/// reach; the whole of them is a tap away.
struct NeedTile: View {
    @Environment(\.theme) private var theme
    @Environment(\.navigate) private var navigate
    @Environment(\.verticalSizeClass) private var height
    let need: Need
    let route: Route

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            PlainRouteLink(value: route) {
                VStack(alignment: .leading, spacing: 6) {
                    HStack {
                        Text(need.project)
                            .font(theme.mono(.caption2, .medium))
                            .foregroundStyle(theme.muted)
                        Spacer()
                        Image(systemName: "chevron.right")
                            .font(theme.body(.footnote, .semibold))
                            .foregroundStyle(theme.muted)
                            .accessibilityHidden(true)
                    }
                    NeedSummary(need: need)
                    if !need.text.isEmpty {
                        Text(need.text)
                            .font(theme.body(.subheadline))
                            .lines(height == .compact ? 1 : 2)
                            .padding(.leading, 24)
                    }
                }
                .contentShape(Rectangle())
                .accessibilityElement(children: .combine)
                .accessibilityHint(need.kind.verb)
            }
            .buttonStyle(.plain)
            NeedActs(need: need)
                .padding(.leading, 24)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .frosted(RoundedRectangle(cornerRadius: 12))
        .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 12))
        .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(theme.colour(need.kind).opacity(theme.contrast || open ? 1 : 0.35), lineWidth: open ? 2.5 : 1))
        .accessibilityElement(children: .contain)
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
