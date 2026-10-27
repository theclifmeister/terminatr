import AppKit
import SwiftUI
import TerminatrKit

/// A link need's buttons (T778), as the TUI's Messages tab and Network
/// popup answer it: Send or Drop… a held message, Accept or Decline… an
/// offered file, Accept… a link request with this side's direction (its
/// mirror first) or Decline… it, Share… a project for a share request
/// or Decline… it. What the TUI asks first asks first here, in its
/// words; the outcome row takes the buttons' place while it runs and
/// once it went.
struct LinkActs: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let need: Need
    let act: LinkAct
    @State private var declining = false
    @State private var sharing: String?

    var body: some View {
        HStack(spacing: 6) {
            if app.linkOutcomes[need.id]?.holdsButtons == true {
                EmptyView()
            } else {
                yes
                Button(act.no) { declining = true }
                    .help(act.declineMessage)
                    .accessibilityLabel("\(act.no) \(need.what)")
            }
        }
        .confirmationDialog(act.declineTitle, isPresented: $declining) {
            Button(act.declineButton, role: .destructive) { app.answerLink(need, yes: false) }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text(act.declineMessage)
        }
        .confirmationDialog("Share \(sharing ?? "")?", isPresented: Binding(get: { sharing != nil }, set: { if !$0 { sharing = nil } }),
                            presenting: sharing) { slug in
            Button("Share") { app.answerLink(need, yes: true, share: slug) }
            Button("Cancel", role: .cancel) {}
        } message: { slug in
            Text(act.shareMessage(slug))
        }
    }

    @ViewBuilder
    private var yes: some View {
        switch act.kind {
        case .held, .file:
            Button(act.yes) { app.answerLink(need, yes: true) }
                .buttonStyle(.borderedProminent)
                .tint(theme.action)
                .help(act.kind == .held ? "Sends \(act.id) to \(act.other)" : "Takes \(act.file) from \(act.other)")
                .accessibilityLabel("\(act.yes) \(need.what)")
        case .request:
            Menu(act.yes) {
                Section("Link \(act.project) with \(act.other) · this side") {
                    ForEach(act.directions, id: \.self) { d in
                        Button(d.words.prefix(1).uppercased() + d.words.dropFirst()) { app.answerLink(need, yes: true, direction: d) }
                    }
                }
            }
            .fixedSize()
            .help("Links \(act.project) with \(act.other): pick this side's direction")
            .accessibilityLabel("Accept \(need.what)…")
        case .share:
            let free = LinkAct.shareable(app.projects)
            Menu(act.yes) {
                if free.isEmpty {
                    Text("Every active project is shared already")
                }
                Section("Share one of yours with every paired server") {
                    ForEach(free, id: \.self) { slug in
                        Button(slug) { sharing = slug }
                    }
                }
            }
            .fixedSize()
            .help("Shares one of your projects with every paired server, for \(act.other)'s request")
            .accessibilityLabel("Share a project for \(act.other)…")
        }
    }
}

/// What came of a link need's act, as a row across its card (T778):
/// the act running, done in the server's words, or refused. VoiceOver
/// hears it as one element (AppModel.answerLink announces it).
struct LinkOutcomeRow: View {
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let outcome: LinkOutcome
    /// Whether it stands alone, its need gone: it names the need.
    var settled = false

    var body: some View {
        let colour = colour
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: outcome.symbol)
                .font(theme.body(.subheadline, .semibold))
                .foregroundStyle(colour)
            VStack(alignment: .leading, spacing: 1) {
                if settled {
                    Text(outcome.title)
                        .font(theme.body(.footnote, .medium))
                        .foregroundStyle(theme.muted)
                        .lineLimit(1)
                }
                Text(outcome.text)
                    .font(theme.body(.subheadline))
                    .foregroundStyle(isFailed ? theme.attention : theme.ink)
                    .fixedSize(horizontal: false, vertical: true)
                    .textSelection(.enabled)
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 7)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(colour.opacity(0.12), in: RoundedRectangle(cornerRadius: 8))
        .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(colour.opacity(theme.contrast ? 1 : 0.3), lineWidth: 1))
        .accessibilityElement(children: .combine)
        .transition(reduceMotion ? .identity : .opacity.combined(with: .move(edge: .top)))
    }

    private var isFailed: Bool {
        if case .failed = outcome.phase { return true }
        return false
    }

    private var colour: Color {
        switch outcome.phase {
        case .acting: theme.muted
        case .done: theme.good
        case .failed: theme.attention
        }
    }
}

extension AppModel {
    /// Answers need's link act (link.act, or peer.share-answer for a
    /// share request) as the human, yes or no, with a link request's
    /// direction or a share's project, and keeps what came of it in
    /// linkOutcomes for the outcome rows; VoiceOver hears it. A done
    /// one's row stays a moment after its need went.
    func answerLink(_ need: Need, yes: Bool, direction: LinkAct.Direction? = nil, share: String = "") {
        guard let act = need.link else { return }
        var outcome = LinkOutcome(act: act, title: need.title)
        fade { linkOutcomes[need.id] = outcome }
        Task {
            do {
                let b = try connected()
                if act.kind == .share {
                    let r = try await b.call(AppAPI.Methods.peerShareAnswer,
                                             AppAPI.ShareAnswerParams(id: act.id, accept: yes, project: share))
                    outcome.phase = .done(r.state == "accepted"
                        ? "\(r.project) is shared with every paired server, for \(act.other)'s request"
                        : "declined \(act.other)'s request; nothing was shared, \(act.other) hears of it")
                } else {
                    outcome.phase = .done(try await b.call(AppAPI.Methods.linkAct, act.params(yes: yes, direction: direction)).words)
                }
            } catch {
                outcome.phase = .failed(Dashboard.words(error))
            }
            fade { linkOutcomes[need.id] = outcome }
            AccessibilityNotification.Announcement(outcome.text).post()
            guard case .done = outcome.phase else { return }
            try? await Task.sleep(for: .seconds(Self.settledFor))
            if linkOutcomes[need.id] == outcome { fade { linkOutcomes[need.id] = nil } }
        }
    }

    /// How long a done act's row outlives its need, in seconds.
    static let settledFor = 8

    /// The done acts of project ("" for share requests, nil for all)
    /// whose needs went: their rows, for a moment.
    func settledLinks(_ project: String?) -> [(id: String, outcome: LinkOutcome)] {
        linkOutcomes.filter { id, o in
            guard case .done = o.phase, project == nil || o.act.project == project else { return false }
            return !needs.contains { $0.id == id }
        }
        .map { (id: $0.key, outcome: $0.value) }
        .sorted { $0.id < $1.id }
    }

    /// A change to the outcome rows, faded in; at once under Reduce
    /// Motion.
    private func fade(_ change: () -> Void) {
        withAnimation(NSWorkspace.shared.accessibilityDisplayShouldReduceMotion ? nil : .easeOut(duration: 0.2), change)
    }
}
