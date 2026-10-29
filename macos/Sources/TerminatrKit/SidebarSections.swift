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

    /// A row's key as its kind and the rest: "t:acme-web/t-1" is ("t",
    /// "acme-web/t-1").
    public static func split(_ key: String) -> (kind: String, rest: String) {
        guard let i = key.firstIndex(of: ":") else { return (key, "") }
        return (String(key[..<i]), String(key[key.index(after: i)...]))
    }
}
