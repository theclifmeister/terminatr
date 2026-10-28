import SwiftUI
import TerminatrKit
import TerminatrUI

/// The start screen: every need, as the Mac app's Needs you lists them
/// (Need.of), each a card (NeedSummary) that opens its session, else
/// its project. Read only: the answers stay on the Mac for now.
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
                    NavigationLink(value: route(n)) {
                        NeedTile(need: n)
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(16)
        }
        .refreshable { await model.refresh() }
        .navigationTitle("Needs you")
        .backdrop()
    }

    private func route(_ n: Need) -> Route {
        if !n.session.isEmpty, let s = model.sessions.first(where: { $0.id == n.session }), let ref = SessionRef(s) {
            return .session(ref)
        }
        if n.kind == .question, n.id.hasPrefix("questions:") { return .session(SessionRef(project: n.project)) }
        if let slash = n.project.firstIndex(of: "/") {
            return .peerProject(alias: String(n.project[..<slash]), project: String(n.project[n.project.index(after: slash)...]))
        }
        return .project(n.project)
    }
}

/// A need's card, as the Mac dashboard's NeedCard looks, without its
/// buttons: its project, then NeedSummary.
struct NeedTile: View {
    @Environment(\.theme) private var theme
    let need: Need

    var body: some View {
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
                    .lineLimit(3)
                    .padding(.leading, 24)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .frosted(RoundedRectangle(cornerRadius: 12))
        .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 12))
        .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(theme.colour(need.kind).opacity(theme.contrast ? 1 : 0.35), lineWidth: 1))
        .contentShape(RoundedRectangle(cornerRadius: 12))
        .accessibilityElement(children: .combine)
        .accessibilityHint(need.kind.verb)
    }
}

/// What the screens are of, on the start screen: the demo (and the way
/// back out), or the paired server and its link: linking, asleep or
/// offline, lost.
struct SourceBanner: View {
    @Environment(PhoneModel.self) private var model

    var body: some View {
        switch model.source {
        case .none: EmptyView()
        case .demo: DemoBanner()
        case .server: LinkBanner()
        }
    }
}

struct LinkBanner: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    @State private var forgetting = false

    var body: some View {
        let (icon, text, colour) = words
        HStack(spacing: 8) {
            if model.link == .connecting {
                ProgressView().controlSize(.small)
            } else {
                Image(systemName: icon)
                    .foregroundStyle(colour)
                    .accessibilityHidden(true)
            }
            Text(text)
                .font(theme.body(.footnote, .medium))
                .fixedSize(horizontal: false, vertical: true)
            Spacer()
            Menu {
                Button("Forget this server", role: .destructive) { forgetting = true }
            } label: {
                Image(systemName: "ellipsis.circle")
                    .accessibilityLabel("Server")
            }
        }
        .padding(10)
        .background(colour.opacity(0.12), in: RoundedRectangle(cornerRadius: 10))
        .accessibilityElement(children: .combine)
        .confirmationDialog("Forget \(model.serverName)?", isPresented: $forgetting, titleVisibility: .visible) {
            Button("Forget", role: .destructive) { Task { await model.forget() } }
        } message: {
            Text("The phone forgets the server and its own key. To link again, pair again; on the computer, tm device remove takes the phone off.")
        }
    }

    private var words: (String, String, Color) {
        let name = model.serverName
        switch model.link {
        case .connecting: return ("antenna.radiowaves.left.and.right", "Linking to \(name)…", theme.muted)
        case .asleep: return ("moon.zzz", "\(name) is asleep or offline. The app links again once it's back.", theme.attention)
        case .up: return ("checkmark.circle", "\(name), read only", theme.action)
        case .lost(let why): return ("exclamationmark.triangle", "Link lost: \(why) Trying again…", theme.attention)
        }
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
