import SwiftUI
import TerminatrKit
import TerminatrUI

/// The menu bar's dropdown (MenuBarExtra, .window style; T748 milestone
/// F): the first needs, each named with its project, and a line for the
/// rest; then this server's active projects that have something going,
/// with their ⌘1–⌘9, a line counting the others, and the paired
/// servers' shared projects, over the theme's backdrop (MenuBarList). A
/// row shows what it names in the app's window.
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
                        Text("Can't reach terminatr").font(theme.title(.headline, .bold))
                        Text(why)
                            .font(theme.body(.subheadline))
                            .foregroundStyle(theme.muted)
                            .lineLimit(4)
                        Button("Retry") { model.connect() }
                            .accessibilityLabel("Retry the connection to terminatr")
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
                            .font(theme.body(.subheadline))
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
                        .font(theme.body(.callout))
                        .foregroundStyle(theme.muted)
                        .padding(.horizontal, 12)
                        .padding(.bottom, 10)
                } else {
                    let (shown, rest) = MenuBarList.needs(model.needs)
                    VStack(spacing: 2) {
                        ForEach(shown) { n in
                            NeedRow(need: n) { open(n.project, session: n.session) }
                            if let o = model.linkOutcomes[n.id] {
                                LinkOutcomeRow(outcome: o)
                                    .padding(.horizontal, 8)
                            }
                        }
                        ForEach(model.settledLinks(nil), id: \.id) {
                            LinkOutcomeRow(outcome: $0.outcome, settled: true)
                                .padding(.horizontal, 8)
                        }
                        if !rest.isEmpty {
                            MenuRow(action: { open(nil) }) {
                                Text(SidebarTree.moreNeeds(rest))
                                    .font(theme.body(.subheadline))
                                    .foregroundStyle(theme.muted)
                                Spacer(minLength: 6)
                                Text(MenuBarList.projects(of: rest))
                                    .font(theme.body(.subheadline))
                                    .foregroundStyle(theme.muted)
                                    .lineLimit(1)
                                    .truncationMode(.middle)
                            }
                        }
                    }
                    .padding(.horizontal, 4)
                    .padding(.bottom, 6)
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
                        let (shown, more) = MenuBarList.projects(model.activeProjects, needs: model.needs)
                        ForEach(shown, id: \.slug) { row in
                            if let p = model.project(row.slug) {
                                MenuRow(action: { open(p.slug) }) {
                                    Text(p.slug).font(theme.body(.body, .semibold))
                                    Text(Self.summary(p))
                                        .font(theme.body(.subheadline))
                                        .foregroundStyle(theme.muted)
                                    Spacer(minLength: 6)
                                    Flag(count: model.needs(of: p.slug).count)
                                    if row.index < 9 {
                                        Text("⌘\(row.index + 1)")
                                            .font(theme.mono(.caption))
                                            .foregroundStyle(theme.muted)
                                    }
                                }
                                .keyboardShortcut(row.index < 9 ? KeyboardShortcut(KeyEquivalent(Character("\(row.index + 1)"))) : nil)
                            }
                        }
                        let inactive = model.inactiveProjects.count
                        if let others = MenuBarList.others(more: more, inactive: inactive) {
                            // Only inactive ones left out: the sidebar unfolds them.
                            MenuRow(action: { if more == 0 { model.nav.showInactive = true }; open(nil) }) {
                                Text(others)
                                    .font(theme.body(.subheadline))
                                    .foregroundStyle(theme.muted)
                                Spacer(minLength: 6)
                                Text("in the window")
                                    .font(theme.body(.subheadline))
                                    .foregroundStyle(theme.muted)
                            }
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
                    .font(theme.body(.callout, .semibold))
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
            .accessibilityLabel("Settings")
            .buttonStyle(.borderless)
            .help("Settings")
            Button {
                NSApp.terminate(nil)
            } label: {
                Image(systemName: "power")
            }
            .accessibilityLabel("Quit terminatr")
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
        // None while the bridge is down: the window keeps the old ones.
        let count = model.isConnected ? model.needs.count : 0
        HStack(spacing: 3) {
            Image(nsImage: count == 0 ? MenuBarGlyph.plain : MenuBarGlyph.needs)
            if count > 0 {
                Text("\(count)")
            }
        }
        .accessibilityLabel(count == 0 ? "terminatr" : "terminatr, \(count) need you")
        .task {
            let open = openWindow
            model.showWindow = {
                open(id: TerminatrApp.mainWindow)
                NSApp.activate()
            }
        }
    }
}

/// The menu bar's glyph (T766): the app icon's ›_ (macos/Icon,
/// site/favicon.svg) on an 18 pt box, one colour, a template image so the
/// menu bar draws it in its own colour, light or dark. needs adds a dot
/// at the top right, cut clear of the ›_, while something needs the user.
enum MenuBarGlyph {
    static let plain = image(dot: false)
    static let needs = image(dot: true)

    private static func image(dot: Bool) -> NSImage {
        let image = NSImage(size: NSSize(width: 18, height: 18), flipped: true) { _ in
            let glyph = NSBezierPath()
            glyph.move(to: NSPoint(x: 3, y: 4.5))
            glyph.line(to: NSPoint(x: 7.5, y: 9))
            glyph.line(to: NSPoint(x: 3, y: 13.5))
            glyph.move(to: NSPoint(x: 9.5, y: 13.5))
            glyph.line(to: NSPoint(x: 15.5, y: 13.5))
            glyph.lineWidth = 2
            glyph.lineCapStyle = .round
            glyph.lineJoinStyle = .round
            NSColor.black.set()
            glyph.stroke()
            if dot {
                // The ring clears the ›_ under the dot; the dot fills it.
                NSGraphicsContext.current?.compositingOperation = .clear
                NSBezierPath(ovalIn: NSRect(x: 15 - 4.3, y: 4 - 4.3, width: 8.6, height: 8.6)).fill()
                NSGraphicsContext.current?.compositingOperation = .sourceOver
                NSBezierPath(ovalIn: NSRect(x: 15 - 2.8, y: 4 - 2.8, width: 5.6, height: 5.6)).fill()
            }
            return true
        }
        image.isTemplate = true
        image.accessibilityDescription = "terminatr"
        return image
    }
}

/// A frosted block of the dropdown: a card over the theme's glass, as
/// in the window (over the material alone, dark text read under 3:1 on
/// the mesh's brighter corners).
private struct Panel<Content: View>: View {
    @Environment(\.theme) private var theme
    @ViewBuilder let content: Content

    var body: some View {
        content
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 12))
            .background(theme.glass, in: RoundedRectangle(cornerRadius: 12))
            .frosted(RoundedRectangle(cornerRadius: 12))
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
            Text(text).font(theme.title(.heading, .bold))
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
                .font(theme.mono(.caption, .medium))
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

/// A need in the dropdown: a click shows it in the window; a link need
/// or a share request has its buttons beside it, outside the row's
/// click (LinkActs).
struct NeedRow: View {
    @Environment(\.theme) private var theme
    let need: Need
    let action: () -> Void

    var body: some View {
        if let l = need.link {
            HStack(spacing: 4) {
                row(verb: false)
                LinkActs(need: need, act: l)
                    .controlSize(.small)
                    .padding(.trailing, 8)
            }
        } else {
            row(verb: true)
        }
    }

    private func row(verb: Bool) -> some View {
        MenuRow(action: action) {
            Image(systemName: need.kind.symbol)
                .font(theme.body(.body, .semibold))
                .foregroundStyle(colour)
                .frame(width: 18)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 1) {
                Text(need.title)
                    .font(theme.body(.body, .semibold))
                    .lineLimit(1)
                Text(need.detail)
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
            }
            Spacer(minLength: 4)
            if verb {
                Text(need.kind.verb)
                    .font(theme.body(.footnote, .semibold))
                    .foregroundStyle(theme.action)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var colour: Color { theme.colour(need.kind) }
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
                .font(theme.body(.body, .semibold))
            Text(summary)
                .font(theme.body(.subheadline))
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
