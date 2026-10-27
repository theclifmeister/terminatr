import Foundation
import Observation
import TerminatrKit

/// One session's streams: its watch and its transcript, (re)opened
/// whenever the ref names another session or the bridge restarts, and
/// the acts the session view sends as the human. The app keeps it while
/// the session is shown and for a few selections after (AppModel.model). The server
/// checks every act; the window only shows what it answered. A paired
/// server's session has no watch: its pane crosses the link
/// (TerminalTab), and its transcript too while that server shares it
/// (RemoteConversation), through this server.
@MainActor
@Observable
final class SessionModel {
    let ref: SessionRef
    private(set) var sessionID: String?
    private(set) var watch: AppAPI.Watch?
    private(set) var transcript = Transcript()
    /// Why the transcript stream broke or was refused.
    private(set) var transcriptFailure: String?
    var tab = SessionTab.conversation
    /// The user picked the tab: the window no longer switches it.
    var tabPicked = false
    /// Repaints asked for from View › Repaint Terminal: the shown
    /// Terminal tab asks its server for one each time this goes up.
    private(set) var repaints = 0
    /// The groups of tool calls the user unfolded, by their first
    /// call's id (Transcript.ToolRun), while the transcript lasts.
    var unfoldedRuns: Set<Int> = []

    @ObservationIgnored private var streams: [Task<Void, Never>] = []
    /// A transcript stream runs, or ran to its end without one since:
    /// a paired server's opens again once it shares it again.
    @ObservationIgnored private var reading = false
    /// The bridge the streams run on.
    @ObservationIgnored private var bridge: Bridge?
    @ObservationIgnored private weak var app: AppModel?

    init(ref: SessionRef) {
        self.ref = ref
    }

    /// Follows the session app names for ref: a new one restarts the
    /// streams, none stops them. While the bridge is down the streams
    /// stop but what they showed stays, until the next bridge.
    func follow(_ app: AppModel) {
        self.app = app
        guard app.bridge != nil else {
            stop()
            return
        }
        let id = app.sessionID(ref)
        let reads = ref.isRemote ? app.remoteConversation(ref).reads : app.caps.contains("transcript")
        // A paired server that stops sharing keeps what it showed: its
        // stream ends by itself, and opens again once it shares again.
        guard id != sessionID || app.bridge !== bridge || (reads && !reading) else { return }
        stop()
        sessionID = id
        bridge = app.bridge
        watch = nil
        transcript = Transcript()
        unfoldedRuns = []
        transcriptFailure = nil
        reading = false
        guard let id, let bridge = app.bridge else { return }
        if !ref.isRemote {
            streams.append(Task { await self.watchSession(bridge, id) })
        }
        if reads {
            reading = true
            streams.append(Task { await self.watchTranscript(bridge, id) })
        }
    }

    func stop() {
        streams.forEach { $0.cancel() }
        streams = []
        bridge = nil
    }

    /// The user picks tab: the view no longer switches it.
    func pick(_ t: SessionTab) {
        tab = t
        tabPicked = true
    }

    /// ⌥⌘R: a fresh repaint of the shown pane.
    func askRepaint() {
        repaints += 1
    }

    /// Opens on the conversation; falls back to the terminal while it
    /// can't be shown, unless the user picked a tab.
    func settle(_ state: ConversationState) {
        guard !tabPicked else { return }
        tab = state.available ? .conversation : .terminal
    }

    private func watchSession(_ bridge: Bridge, _ id: String) async {
        do {
            let sub = try await bridge.subscribe(AppAPI.Methods.sessionWatch, AppAPI.SessionIDParams(id: id))
            watch = sub.result
            for try await e in sub.events {
                watch = e.data
                app?.listSessions() // the question and the escape come with the session
            }
        } catch {}
    }

    private func watchTranscript(_ bridge: Bridge, _ id: String) async {
        do {
            let sub = try await bridge.subscribe(AppAPI.Methods.transcriptWatch, AppAPI.TranscriptParams(session: id))
            transcript.start(sub.result.status)
            for try await e in sub.events {
                transcript.apply(e.name, e.data)
            }
            if ref.isRemote { reading = false }
        } catch BridgeError.closed {
            // The bridge went: the conversation stays as it was, and the
            // next bridge opens the stream again (follow).
        } catch {
            if !Task.isCancelled {
                transcriptFailure = error.localizedDescription
                if ref.isRemote { reading = false }
            }
        }
    }

    // MARK: acts, as the human (journaled "human (app)")

    enum ActError: LocalizedError {
        case noSession
        case notConnected

        var errorDescription: String? {
            switch self {
            case .noSession: "The session isn't running."
            case .notConnected: "Not connected to terminatr."
            }
        }
    }

    private func target() throws -> (Bridge, String) {
        guard let b = app?.bridge else { throw ActError.notConnected }
        guard let id = sessionID else { throw ActError.noSession }
        return (b, id)
    }

    /// Sends a prompt: queued (mode ""), steered into the running turn
    /// ("now") or interrupting it ("interrupt").
    /// files are refs upload answered: tm names each after the text.
    func prompt(_ text: String, mode: String = "", files: [String] = []) async throws -> AppAPI.PromptResult {
        let (b, id) = try target()
        return try await b.call(AppAPI.Methods.sessionPrompt, AppAPI.PromptParams(id: id, text: text, mode: mode, files: files))
    }

    /// Sends a file for a prompt to tm (session.upload), which stores it
    /// in the project's uploads/: the last answer has its ref.
    func upload(_ data: Data, name: String, progress: (Int) -> Void) async throws -> AppAPI.Upload {
        let (b, id) = try target()
        return try await ComposerFiles.upload(data, name: name, session: id,
                                              call: { try await b.call(AppAPI.Methods.sessionUpload, $0) }, progress: progress)
    }

    /// Answers question index of the open menu. guarded: the server
    /// refuses unless the menu still asks question, single-select and
    /// unanswered, and answer is one of its options.
    func answer(index: Int, answer: String, question: String, guarded: Bool) async throws -> AppAPI.AnswerResult {
        let (b, id) = try target()
        return try await b.call(AppAPI.Methods.sessionAnswer, AppAPI.AnswerParams(id: id, index: index, answer: answer, question: guarded ? question : ""))
    }

    /// Asks the server what option choice of the dialog on screen would
    /// answer, sending nothing (check), or sends it. Escape and session:
    /// the user confirmed a command outside the sandbox, an option that
    /// allows for the rest of the session.
    func approve(choice: Int, check: Bool, escape: Bool = false, session: Bool = false) async throws -> AppAPI.ApproveResult {
        let (b, id) = try target()
        return try await b.call(AppAPI.Methods.sessionApprove, AppAPI.ApproveParams(id: id, choice: choice, check: check, escape: escape, session: session))
    }

    /// Writes data to the session's terminal as if typed ("\u{1b}": Esc).
    func keys(_ data: String) async throws {
        let (b, id) = try target()
        _ = try await b.call(AppAPI.Methods.sessionKeys, AppAPI.KeysParams(id: id, data: data))
    }
}
