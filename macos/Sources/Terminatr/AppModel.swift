import Foundation
import Observation
import TerminatrKit

/// What the windows show: the bridge's state and every project, kept
/// current by projects.watch {detail}.
@MainActor
@Observable
final class AppModel {
    enum Phase: Equatable {
        case connecting
        case connected(AppAPI.Hello)
        case failed(String)
    }

    private(set) var phase = Phase.connecting
    private(set) var projects: [AppAPI.Project] = []

    @ObservationIgnored private var run: Task<Void, Never>?

    /// The settings key of the configured tm path (TMLocator.configured).
    static let tmPathKey = "tmPath"

    /// Starts the bridge unless it runs.
    func start() {
        if run == nil { connect() }
    }

    /// (Re)starts the bridge: with the configured tm, or the first one
    /// found. A bridge already running is ended first.
    func connect() {
        run?.cancel()
        run = Task { await watch(tmPath: UserDefaults.standard.string(forKey: Self.tmPathKey)) }
    }

    private func watch(tmPath: String?) async {
        phase = .connecting
        var bridge: Bridge?
        do {
            let tm = try TMLocator(configured: tmPath).locate()
            let b = try await Bridge.launch(tm: tm)
            bridge = b
            guard let hello = await b.hello else { throw BridgeError.closed("") }
            phase = .connected(hello)
            let sub = try await b.subscribe(AppAPI.Methods.projectsWatch, AppAPI.ProjectsWatchParams(detail: true))
            projects = sub.result.projects
            for try await e in sub.events {
                projects = e.data.projects
            }
            if !Task.isCancelled {
                phase = .failed("The server stopped sending the projects.")
            }
        } catch {
            if !Task.isCancelled {
                phase = .failed(error.localizedDescription)
            }
        }
        await bridge?.close()
    }
}
