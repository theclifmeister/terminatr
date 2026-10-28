import Foundation

/// File › New Project… (app API 22), as the TUI's +: the project's name,
/// which tm makes a lower-case slug, then the folder of its repo, if any
/// (project.new).
public struct NewProjectForm: Equatable, Sendable {
    public var name = ""
    /// The repo's folder on this Mac, "" for none.
    public var repo = ""

    public init(name: String = "", repo: String = "") {
        self.name = name
        self.repo = repo
    }

    public var trimmedName: String {
        name.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    public var canMake: Bool {
        !trimmedName.isEmpty && !trimmedName.contains(where: \.isNewline)
    }

    /// project.new's params.
    public var params: AppAPI.ProjectNewParams {
        let r = repo.trimmingCharacters(in: .whitespacesAndNewlines)
        return AppAPI.ProjectNewParams(project: trimmedName, repos: r.isEmpty ? [] : [r])
    }

    /// What the sheet says once project.new answered, as the TUI's
    /// footer.
    public static func done(_ r: AppAPI.ProjectNew) -> String {
        r.noAgent ? "Created project \(r.project); no agent is installed, so it can't run a coordinator or threads until one is (tm doctor)."
            : "Created project \(r.project)."
    }
}

/// A message the user writes as the project on one of its links (app
/// API 22), as the TUI's M: to a link that sends (link.send).
public struct LinkMessageForm: Equatable, Sendable {
    public var peer: String
    public var text = ""

    public init(peer: String, text: String = "") {
        self.peer = peer
        self.text = text
    }

    /// The links of p the user may write on: those that send, a
    /// paired server's too; first, when it is one, first.
    public static func writable(_ p: AppAPI.Project, first: String = "") -> [AppAPI.Link] {
        let to = p.links.filter(\.send)
        guard let i = to.firstIndex(where: { $0.peer == first }), i > 0 else { return to }
        return [to[i]] + to[..<i] + to[(i + 1)...]
    }

    public var canSend: Bool {
        !peer.isEmpty && !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    /// link.send's params for project.
    public func params(project: String) -> AppAPI.LinkSendParams {
        AppAPI.LinkSendParams(project: project, peer: peer, text: text.trimmingCharacters(in: .whitespacesAndNewlines))
    }
}
