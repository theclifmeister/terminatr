import SwiftUI
import TerminatrKit
import TerminatrUI

/// A row of the iPad's sidebar (T849): the route it opens in the
/// Projects tab's columns, its words, its state and, while it waits on
/// the user, its state's word as a badge.
struct SidebarRow: Identifiable {
    let id: String
    let route: Route
    let title: String
    let tone: Attention.Tone
    let badge: String?

    /// The row SidebarTree.rows keys as key: a coordinator ("c:",
    /// "rc:"), a thread ("t:", "rt:") or an inactive project ("p:"),
    /// its route on model's server.
    @MainActor static func of(_ key: String, model: PhoneModel) -> SidebarRow? {
        let (kind, rest) = SidebarTree.split(key)
        let parts = rest.split(separator: "/").map(String.init)
        let id = model.key + " " + key
        func row(_ route: Route, _ title: String, _ a: Attention) -> SidebarRow {
            SidebarRow(id: id, route: route.on(model.key), title: title, tone: a.tone, badge: a.waits ? a.word : nil)
        }
        switch (kind, parts.count) {
        case ("p", 1):
            return SidebarRow(id: id, route: Route.project(rest).on(model.key), title: rest, tone: .idle, badge: nil)
        case ("c", 1):
            guard let p = model.project(rest) else { return nil }
            return row(.session(SessionRef(project: rest)), "Coordinator", .of(coordinator: model.coordinatorSession(rest), in: p))
        case ("t", 2):
            guard let t = model.project(parts[0])?.threads.first(where: { $0.record.id == parts[1] }) else { return nil }
            let title = [t.task?.ref, t.record.title].compactMap { $0 }.joined(separator: " ")
            return row(.session(SessionRef(project: parts[0], thread: parts[1])), title, model.attention(t))
        case ("rc", 2), ("rt", 3):
            let ref = SessionRef(project: parts[1], thread: kind == "rt" ? parts[2] : "", server: parts[0])
            guard let p = model.peers.first(where: { $0.alias == ref.server })?.projects.first(where: { $0.project == ref.project }),
                  let e = SidebarTree.sessions(of: p, alias: ref.server).first(where: { $0.ref == ref }) else { return nil }
            let title = ref.isCoordinator ? "Coordinator" : [e.task, e.title.isEmpty ? ref.thread : e.title].filter { !$0.isEmpty }.joined(separator: " ")
            return row(.session(ref), title, .of(peer: e.session))
        default:
            return nil
        }
    }
}

/// A section of the iPad's sidebar, of one server's.
struct SidebarGroup: Identifiable {
    let id: String
    let title: String
    let rows: [SidebarRow]
}

extension PhoneModel {
    /// The iPad sidebar's sections: the TUI's rows (SidebarTree.rows),
    /// its folds as they start, so the inactive projects listed but
    /// not their threads; a server the phone pairs with directly not
    /// again under this one (shownPeers).
    var sidebar: [(section: SidebarSection, rows: [SidebarRow])] {
        var nav = Navigation()
        nav.showInactive = true
        let keys = SidebarTree.rows(projects, sessions: sessions, peers: shownPeers, nav: nav, needsFirst: needsFirst)
        return SidebarTree.sections(keys).map { s in (s, s.rows.compactMap { SidebarRow.of($0, model: self) }) }
    }
}

extension Phone {
    /// Every server's sections in turn; with more than one, each
    /// section's title leads with its server's name (T852).
    var sidebar: [SidebarGroup] {
        models.flatMap { m in
            m.sidebar.map { s in
                SidebarGroup(id: m.key + " " + s.section.id, title: several ? m.serverName + " · " + s.section.title : s.section.title, rows: s.rows)
            }
        }
    }
}

/// The sidebar's head: the wordmark and the servers it reads.
struct SidebarHead: View {
    @Environment(Phone.self) private var phone
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: Spacing.hair) {
            Text("terminatr")
                .font(theme.title(.title, .bold))
                .foregroundStyle(theme.ink)
            Text(model.source == .demo ? "Demo" : phone.several ? phone.models.map(\.serverName).joined(separator: ", ") : model.serverName)
                .font(theme.body(.subheadline))
                .foregroundStyle(theme.muted)
                .lines(1)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}
