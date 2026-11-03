import Foundation

/// A section of the iPad's sidebar (T849): a project with its rows, a
/// paired server's shared project with its rows, or the inactive
/// projects, each a row of its own.
public struct SidebarSection: Equatable, Sendable, Identifiable {
    /// Its first row's key ("p:acme-web", "r:laptop/docs"), or
    /// "d:inactive".
    public let id: String
    /// The project's slug; for a paired server's, its alias too.
    public let title: String
    /// Its rows' keys, as SidebarTree.rows keys them.
    public let rows: [String]

    public init(id: String, title: String, rows: [String]) {
        self.id = id
        self.title = title
        self.rows = rows
    }
}

extension SidebarTree {
    /// The tree's rows (rows(…)) as the iPad's sidebar sections them: a
    /// project's ("p:") or a paired server's project's ("r:") header
    /// starts a section and its coordinator and threads are its rows;
    /// after the inactive divider each project is a row of one
    /// "Inactive" section. Servers' group rows ("g:") are left out: a
    /// paired server's sections carry its alias. The order is the
    /// rows' own, so the sidebar's is the TUI's and the Mac's.
    public static func sections(_ rows: [String]) -> [SidebarSection] {
        var out: [SidebarSection] = []
        var id = "", title = "", keys: [String] = [], inactive = false
        func close() {
            if !id.isEmpty { out.append(SidebarSection(id: id, title: title, rows: keys)) }
            keys = []
        }
        for r in rows {
            let (kind, rest) = split(r)
            switch kind {
            case "g": continue
            case "d":
                close()
                (id, title, inactive) = (r, "Inactive", true)
            case "p" where inactive:
                keys.append(r)
            case "p":
                close()
                (id, title) = (r, rest)
            case "r":
                close()
                let p = rest.split(separator: "/", maxSplits: 1).map(String.init)
                (id, title) = (r, p.count == 2 ? "\(p[1]) · \(p[0])" : rest)
            default:
                keys.append(r)
            }
        }
        close()
        return out
    }

    /// sections(rows) with programs (T978): a program is a section of its
    /// own, id and title its slug, its rows the program's ("g:<slug>"),
    /// its director's ("r:<slug>") and then each member's row, a local
    /// member's "p:<slug>" and a paired one's "g:<slug>/r:<alias>/<slug>",
    /// each with its coordinator and threads while unfolded. A folded
    /// member keeps its row, which the plain sections would drop with
    /// its header.
    public static func sections(_ rows: [String], programs: [AppAPI.Program]) -> [SidebarSection] {
        let known = Dictionary(programs.map { ($0.slug, $0) }, uniquingKeysWith: { a, _ in a })
        var out: [SidebarSection] = []
        var run: [String] = []
        func flush() {
            out += sections(run)
            run = []
        }
        var i = 0
        while i < rows.count {
            let (kind, rest) = split(rows[i])
            guard kind == "g", let g = known[rest] else {
                run.append(rows[i])
                i += 1
                continue
            }
            flush()
            let local = Set(g.members.filter { $0.server.isEmpty }.map(\.project))
            var block = [rows[i]]
            i += 1
            while i < rows.count {
                let (k, r) = split(rows[i])
                let inside: Bool
                switch k {
                case "r": inside = r == g.slug
                case "g": inside = r.hasPrefix(g.slug + "/")
                case "p": inside = local.contains(r)
                case "c": inside = local.contains(r)
                case "t": inside = local.contains(String(r.split(separator: "/").first ?? ""))
                default: inside = false
                }
                if !inside { break }
                block.append(rows[i])
                i += 1
            }
            out.append(SidebarSection(id: block[0], title: g.slug, rows: block))
        }
        flush()
        return out
    }

    /// A row's key as its kind and the rest: "t:acme-web/t-1" is ("t",
    /// "acme-web/t-1").
    public static func split(_ key: String) -> (kind: String, rest: String) {
        guard let i = key.firstIndex(of: ":") else { return (key, "") }
        return (String(key[..<i]), String(key[key.index(after: i)...]))
    }
}
