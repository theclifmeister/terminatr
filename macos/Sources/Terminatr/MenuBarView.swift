import SwiftUI
import TerminatrKit

/// The menu bar's dropdown (MenuBarExtra, .window style): what needs
/// you, then the projects, this server's and the paired servers'
/// shared ones, over the theme's backdrop. A row shows what it names in
/// the app's window.
struct MenuBarView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.theme) private var theme
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            switch model.phase {
            case .connecting:
                Panel {
                    HStack(spacing: 8) {
                        ProgressView().controlSize(.small)
                        Text("Connecting to terminatr…").foregroundStyle(theme.muted)
                    }
                    .padding(12)
                }
            case .failed(let why):
                Panel {
                    VStack(alignment: .leading, spacing: 6) {
                        Text("Can't reach terminatr").font(theme.title(15, .bold))
                        Text(why)
                            .font(.system(size: 12))
                            .foregroundStyle(theme.muted)
                            .lineLimit(4)
                        Button("Retry") { model.connect() }
                            .buttonStyle(.borderedProminent)
                            .tint(theme.action)
                    }
                    .padding(12)
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
            case .connected:
                if let behind = model.compatibility.message {
                    Panel {
                        Label(behind, systemImage: "arrow.down.circle")
                            .font(.system(size: 12))
                            .foregroundStyle(theme.attention)
                            .padding(10)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
                needs
                projects
            }
            footer
        }
        .padding(10)
        .frame(width: 360)
        .foregroundStyle(theme.ink)
        .background { Backdrop() }
    }

    private func open(_ project: String?, session: String = "") {
        model.open(project: project, session: session)
        dismiss()
    }

    private func open(_ ref: SessionRef) {
        model.select(.session(ref))
        dismiss()
    }

    // MARK: needs you

    private var needs: some View {
        Panel {
            VStack(alignment: .leading, spacing: 2) {
                Heading("Needs you", count: model.needs.count)
                if model.needs.isEmpty {
                    Text("Nothing waits on you.")
                        .font(.system(size: 12.5))
                        .foregroundStyle(theme.muted)
                        .padding(.horizontal, 12)
                        .padding(.bottom, 10)
                } else {
                    ScrollView {
                        VStack(spacing: 2) {
                            ForEach(model.needs) { n in
                                NeedRow(need: n) { open(n.project, session: n.session) }
                            }
                        }
                        .padding(.horizontal, 4)
                        .padding(.bottom, 6)
                    }
                    .frame(maxHeight: 260)
                    .fixedSize(horizontal: false, vertical: true)
                }
            }
        }
    }

    // MARK: projects

    private var projects: some View {
        Panel {
            VStack(alignment: .leading, spacing: 2) {
                Heading("Projects", count: nil)
                ScrollView {
                    VStack(spacing: 2) {
                        ForEach(Array(model.projects.enumerated()), id: \.element.slug) { i, p in
                            MenuRow(action: { open(p.slug) }) {
                                Text(p.slug).font(.system(size: 13, weight: .semibold))
                                Text(Self.summary(p))
                                    .font(.system(size: 12))
                                    .foregroundStyle(theme.muted)
                                Spacer(minLength: 6)
                                Flag(count: model.needs(of: p.slug).count)
                                if i < 9 {
                                    Text("⌘\(i + 1)")
                                        .font(theme.mono(11))
                                        .foregroundStyle(theme.muted)
                                }
                            }
                            .keyboardShortcut(i < 9 ? KeyboardShortcut(KeyEquivalent(Character("\(i + 1)"))) : nil)
                        }
                        ForEach(model.peers, id: \.alias) { peer in
                            ForEach(peer.projects, id: \.project) { p in
                                let coordinator = SessionRef(project: p.project, server: peer.alias)
                                if coordinator.session(in: model.peers) != nil {
                                    MenuRow(action: { open(coordinator) }) {
                                        PeerRow(peer: peer, project: p)
                                    }
                                } else {
                                    PeerRow(peer: peer, project: p)
                                        .padding(.horizontal, 8)
                                        .padding(.vertical, 6)
                                }
                            }
                        }
                    }
                    .padding(.horizontal, 4)
                    .padding(.bottom, 6)
                }
                .frame(maxHeight: 220)
                .fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    /// "4 threads working", "1 thread", "idle".
    static func summary(_ p: AppAPI.Project) -> String {
        let open = p.threads.filter { !$0.record.done }
        if open.isEmpty { return "idle" }
        let working = open.filter { $0.record.state == "working" }.count
        let n = open.count == 1 ? "1 thread" : "\(open.count) threads"
        return working == 0 ? n : "\(n) · \(working) working"
    }

    // MARK: footer

    private var footer: some View {
        HStack(spacing: 6) {
            Button {
                open(nil)
            } label: {
                Text("Open terminatr")
                    .font(.system(size: 12.5, weight: .semibold))
                    .padding(.horizontal, 10)
                    .padding(.vertical, 5)
                    .background(theme.action, in: RoundedRectangle(cornerRadius: 8))
                    .foregroundStyle(theme.onAction)
            }
            .buttonStyle(.plain)
            .keyboardShortcut("o")
            Spacer()
            SettingsLink {
                Image(systemName: "gearshape")
            }
            .buttonStyle(.borderless)
            .help("Settings")
            Button {
                NSApp.terminate(nil)
            } label: {
                Image(systemName: "power")
            }
            .buttonStyle(.borderless)
            .keyboardShortcut("q")
            .help("Quit terminatr (the server keeps running)")
        }
        .foregroundStyle(theme.muted)
        .padding(.horizontal, 4)
    }
}

/// The icon in the menu bar: a template symbol, and how many needs wait.
/// It also hands the model the scene's openWindow, since it is drawn as
/// long as the app runs: the one window comes to the front, opened
/// again when it was closed.
struct MenuBarLabel: View {
    @Environment(AppModel.self) private var model
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        HStack(spacing: 3) {
            Image(systemName: model.needs.isEmpty ? "square.stack.3d.up" : "square.stack.3d.up.badge.automatic")
            if !model.needs.isEmpty {
                Text("\(model.needs.count)")
            }
        }
        .accessibilityLabel(model.needs.isEmpty ? "terminatr" : "terminatr, \(model.needs.count) need you")
        .task {
            let open = openWindow
            model.showWindow = {
                open(id: TerminatrApp.mainWindow)
                NSApp.activate()
            }
        }
    }
}

/// A frosted block of the dropdown.
private struct Panel<Content: View>: View {
    @Environment(\.theme) private var theme
    @ViewBuilder let content: Content

    var body: some View {
        content
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 12))
            .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 12))
            .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(theme.edge, lineWidth: 1))
    }
}

private struct Heading: View {
    @Environment(\.theme) private var theme
    let text: String
    let count: Int?

    init(_ text: String, count: Int?) {
        self.text = text
        self.count = count
    }

    var body: some View {
        HStack(spacing: 6) {
            Text(text).font(theme.title(14, .bold))
            if let count, count > 0 {
                Flag(count: count)
            }
        }
        .padding(.horizontal, 12)
        .padding(.top, 10)
        .padding(.bottom, 4)
    }
}

/// A count of what needs you, in the attention colour; nothing at 0.
private struct Flag: View {
    @Environment(\.theme) private var theme
    let count: Int

    var body: some View {
        if count > 0 {
            Text("\(count)")
                .font(theme.mono(11, .medium))
                .padding(.horizontal, 6)
                .padding(.vertical, 1)
                .background(theme.attention, in: Capsule())
                .foregroundStyle(theme.onAttention)
                .accessibilityLabel("\(count) need you")
        }
    }
}

/// A clickable row that lights up under the pointer.
private struct MenuRow<Content: View>: View {
    @Environment(\.theme) private var theme
    @State private var hover = false
    let action: () -> Void
    @ViewBuilder let content: Content

    var body: some View {
        Button(action: action) {
            HStack(spacing: 8) { content }
                .padding(.horizontal, 8)
                .padding(.vertical, 6)
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(Rectangle())
                .background(hover ? theme.action.opacity(0.12) : .clear, in: RoundedRectangle(cornerRadius: 8))
        }
        .buttonStyle(.plain)
        .onHover { hover = $0 }
    }
}

struct NeedRow: View {
    @Environment(\.theme) private var theme
    let need: Need
    let action: () -> Void

    var body: some View {
        MenuRow(action: action) {
            Image(systemName: Self.symbol(need.kind))
                .font(.system(size: 13, weight: .semibold))
                .foregroundStyle(colour)
                .frame(width: 18)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 1) {
                Text(need.title)
                    .font(.system(size: 13, weight: .semibold))
                    .lineLimit(1)
                Text(need.detail)
                    .font(.system(size: 11.5))
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
            }
            Spacer(minLength: 4)
            Text(Self.verb(need.kind))
                .font(.system(size: 11.5, weight: .semibold))
                .foregroundStyle(theme.action)
        }
        .accessibilityElement(children: .combine)
    }

    private var colour: Color { Self.colour(need.kind, theme) }

    static func colour(_ k: Need.Kind, _ theme: Theme) -> Color {
        switch k {
        case .question: theme.question
        case .approval: theme.approval
        case .review: theme.good
        case .blocked, .checks: theme.attention
        case .held, .network: theme.muted
        }
    }

    static func symbol(_ k: Need.Kind) -> String {
        switch k {
        case .question: "questionmark.bubble"
        case .approval: "hand.raised"
        case .review: "checkmark.seal"
        case .blocked: "exclamationmark.octagon"
        case .checks: "xmark.circle"
        case .held: "pause.circle"
        case .network: "point.3.connected.trianglepath.dotted"
        }
    }

    /// What opening it is for.
    static func verb(_ k: Need.Kind) -> String {
        switch k {
        case .question: "Answer"
        case .approval: "Approve"
        case .review: "Review"
        case .blocked, .checks, .held, .network: "Open"
        }
    }
}

/// A paired server's shared project; with a coordinator running there,
/// a click opens its pane, through the peer link.
private struct PeerRow: View {
    @Environment(\.theme) private var theme
    let peer: AppAPI.Peer
    let project: AppAPI.PeerProject

    var body: some View {
        HStack(spacing: 8) {
            Text("\(peer.name.isEmpty ? peer.alias : peer.name)/\(project.project)")
                .font(.system(size: 13, weight: .semibold))
            Text(summary)
                .font(.system(size: 12))
                .foregroundStyle(theme.muted)
            Spacer(minLength: 6)
            if !project.stale {
                Flag(count: project.needsYou)
            }
        }
        .lineLimit(1)
        .opacity(project.stale ? 0.55 : 1)
        .help(project.goal)
    }

    private var summary: String {
        var f = ["paired", project.threads == 1 ? "1 thread" : "\(project.threads) threads"]
        if project.paused { f.append("paused") }
        if project.stale { f.append("stale") }
        return f.joined(separator: " · ")
    }
}
