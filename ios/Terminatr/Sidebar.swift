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
    /// A coordinator's or director's (T1003): its mark takes the icon's
    /// place, in its state's colour, and its state's word the badge's.
    var role: SessionRole?
    /// Its state; working too on a row folded over a session that works
    /// (T985): a Tab's label has its icon's place for a state, and its
    /// badge's only for words.
    var tone: Attention.Tone
    let badge: String?
    /// A program member's fold, which its chevron and Fold/Unfold write.
    var fold: Fold?

    /// A member project's fold key (Navigation.unfolded) and whether its
    /// coordinator and threads show under it now.
    struct Fold: Equatable {
        let server: String
        let key: String
        let open: Bool
    }

    /// The row SidebarTree.rows keys as key: a coordinator ("c:",
    /// "rc:"), a thread ("t:", "rt:") or an inactive project ("p:"),
    /// its route on model's server.
    @MainActor static func of(_ key: String, model: PhoneModel) -> SidebarRow? {
        // A program's member's row, "g:<program>/<its key>" (B19): the
        // row its key has outside the program, kept apart by its id.
        if key.hasPrefix("g:"), let slash = key.firstIndex(of: "/") {
            guard let r = of(String(key[key.index(after: slash)...]), model: model) else { return nil }
            return SidebarRow(id: model.key + " " + key, route: r.route, title: r.title, role: r.role, tone: r.tone, badge: r.badge)
        }
        if key.hasPrefix("r:"), key.dropFirst(2).split(separator: "/").count == 2 {
            // A paired server's project a program lists (as "g:<program>/r:...").
            let parts = key.dropFirst(2).split(separator: "/").map(String.init)
            return SidebarRow(id: model.key + " " + key, route: Route.peerProject(alias: parts[0], project: parts[1]).on(model.key),
                              title: parts[0] + "/" + parts[1], tone: .idle, badge: nil)
        }
        let (kind, rest) = SidebarTree.split(key)
        let parts = rest.split(separator: "/").map(String.init)
        let id = model.key + " " + key
        func row(_ route: Route, _ title: String, _ a: Attention) -> SidebarRow {
            SidebarRow(id: id, route: route.on(model.key), title: title, tone: a.tone, badge: a.waits ? a.word : nil)
        }
        func role(_ route: SessionRef, _ a: Attention) -> SidebarRow {
            let says = a.waits || a.tone == .working || a.tone == .asleep
            return SidebarRow(id: id, route: Route.session(route).on(model.key), title: route.role?.word ?? "", role: route.role, tone: a.tone, badge: says ? a.word : nil)
        }
        switch (kind, parts.count) {
        case ("g", 1):
            // A program: its screen, what waits in it as the badge.
            guard let g = model.program(rest) else { return nil }
            let n = model.programNeeds(g).count
            return SidebarRow(id: id, route: Route.program(rest).on(model.key), title: rest, tone: n > 0 ? .attention : .idle,
                              badge: n > 0 ? "\(n)" : nil)
        case ("r", 1):
            // Its director: its session, as a coordinator's row (T973).
            guard let g = model.program(rest) else { return nil }
            return role(.director(rest), .of(director: model.directorSession(g), questions: g.questions))
        case ("p", 1):
            return SidebarRow(id: id, route: Route.project(rest).on(model.key), title: rest, tone: .idle, badge: nil)
        case ("c", 1):
            guard let p = model.project(rest) else { return nil }
            return role(SessionRef(project: rest), model.coordinator(of: p))
        case ("t", 2):
            guard let t = model.project(parts[0])?.threads.first(where: { $0.record.id == parts[1] }) else { return nil }
            let title = [t.task?.ref, t.record.title].compactMap { $0 }.joined(separator: " ")
            return row(.session(SessionRef(project: parts[0], thread: parts[1])), title, model.attention(t))
        case ("rc", 2), ("rt", 3):
            let ref = SessionRef(project: parts[1], thread: kind == "rt" ? parts[2] : "", server: parts[0])
            guard let p = model.peers.first(where: { $0.alias == ref.server })?.projects.first(where: { $0.project == ref.project }),
                  let e = SidebarTree.sessions(of: p, alias: ref.server).first(where: { $0.ref == ref }) else { return nil }
            if ref.isCoordinator { return role(ref, .of(peer: e.session)) }
            let title = [e.task, e.title.isEmpty ? ref.thread : e.title].filter { !$0.isEmpty }.joined(separator: " ")
            return row(.session(ref), title, .of(peer: e.session))
        default:
            return nil
        }
    }
}

/// A section's rows in runs (T978): a program member's row with the
/// rows of its coordinator and threads after it, which fold with it;
/// the rows before the first member, and any other row, run alone.
struct SidebarChunk: Identifiable {
    let head: SidebarRow?
    var rows: [SidebarRow]
    var id: String { rows.first?.id ?? "" }

    static func of(_ rows: [SidebarRow]) -> [SidebarChunk] {
        var out: [SidebarChunk] = []
        for r in rows {
            if r.fold != nil {
                out.append(SidebarChunk(head: r, rows: [r]))
            } else if let last = out.last, last.head != nil {
                out[out.count - 1].rows.append(r)
            } else {
                out.append(SidebarChunk(head: nil, rows: [r]))
            }
        }
        return out
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
        var shown = nav
        shown.showInactive = true
        let keys = SidebarTree.rows(projects, sessions: sessions, peers: shownPeers, programs: programs, nav: shown, needsFirst: needsFirst)
        let busy = SidebarTree.busyRows(projects, sessions: sessions, peers: shownPeers, programs: programs, nav: shown)
        return SidebarTree.sections(keys, programs: programs).map { s in
            (s, s.rows.compactMap { key in
                SidebarRow.of(key, model: self).map { r in
                    var r = r
                    r.fold = fold(of: key)
                    if busy.contains(key) { r.tone = .working }
                    return r
                }
            })
        }
    }

    /// The fold of a program member's row (T978): a local member's "p:"
    /// row, a paired one's "g:<program>/r:<alias>/<slug>"; nil for the
    /// rest (a standalone project, the inactive divider keep theirs).
    func fold(of key: String) -> SidebarRow.Fold? {
        let (kind, rest) = SidebarTree.split(key)
        let k: String
        if kind == "p", let g = programOf(rest), projects.first(where: { $0.slug == rest })?.active == true {
            k = SidebarTree.foldKey(member: rest, of: g)
        } else if kind == "g", let slash = rest.firstIndex(of: "/"), let g = program(String(rest[..<slash])) {
            let m = rest[rest.index(after: slash)...]
            guard m.hasPrefix("r:") else { return nil }
            let parts = m.dropFirst(2).split(separator: "/").map(String.init)
            guard parts.count == 2 else { return nil }
            k = SidebarTree.foldKey(member: parts[1], alias: parts[0], of: g)
        } else {
            return nil
        }
        return SidebarRow.Fold(server: self.key, key: k, open: nav.unfolded.contains(k))
    }

    /// Folds or unfolds a program member's rows, kept across launches.
    func toggle(_ fold: SidebarRow.Fold) {
        nav.toggle(fold.key, open: fold.open)
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
