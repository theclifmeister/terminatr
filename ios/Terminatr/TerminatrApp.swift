import CoreText
import SwiftUI
import TerminatrKit
import TerminatrLink
import TerminatrUI

/// terminatr for iPhone (T788, stage 1): a server's Needs you, projects,
/// threads and conversations, read only, over the device link to the
/// server it paired with (tm device pair), or the built-in demo. The
/// link is up while the app is in front. Launched with -demo YES it
/// opens the demo at once, and -open ROUTE (Route.parse) on that
/// screen: the Simulator's screenshots. A terminatr://pair link (the
/// Camera app's scan of the code) opens the pairing screen.
@main
struct TerminatrApp: App {
    @State private var model = PhoneModel()
    @State private var linked: LinkedCode?
    @Environment(\.scenePhase) private var phase

    init() {
        // The Mac app's fonts (Bricolage Grotesque, DM Mono; OFL), which
        // Bloom's titles and mono are set in.
        for f in Bundle.main.urls(forResourcesWithExtension: "ttf", subdirectory: nil) ?? [] {
            CTFontManagerRegisterFontsForURL(f as CFURL, .process, nil)
        }
    }

    var body: some Scene {
        WindowGroup {
            Root()
                .environment(model)
                .environment(\.theme, Theme.bloom)
                .tint(Theme.bloom.action)
                .task {
                    if UserDefaults.standard.bool(forKey: "demo"), model.source == .none {
                        await model.startDemo()
                    }
                    model.openPaired()
                }
                .onOpenURL { url in
                    if let c = try? PairingCode(url.absoluteString) { linked = LinkedCode(code: c) }
                }
                .sheet(item: $linked) { PairScreen(code: $0.code).environment(model) }
        }
        .onChange(of: phase) { _, p in
            switch p {
            case .active: model.resume()
            case .background: Task { await model.suspend() }
            default: break
            }
        }
    }
}

/// A pairing code a link brought, for the sheet.
struct LinkedCode: Identifiable {
    let id = UUID()
    let code: PairingCode
}

/// Where a tap goes.
enum Route: Hashable {
    case project(String)
    case peerProject(alias: String, project: String)
    case session(SessionRef)

    /// A launch argument's route: "project:SLUG",
    /// "session:SLUG" (its coordinator) or "session:SLUG/THREAD".
    static func parse(_ s: String) -> Route? {
        let parts = s.split(separator: ":", maxSplits: 1).map(String.init)
        guard parts.count == 2 else { return nil }
        switch parts[0] {
        case "project": return .project(parts[1])
        case "session":
            let p = parts[1].split(separator: "/", maxSplits: 1).map(String.init)
            return .session(SessionRef(project: p[0], thread: p.count > 1 ? p[1] : ""))
        default: return nil
        }
    }
}

enum RootTab: Hashable {
    case needs, projects
}

struct Root: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    @State private var tab = RootTab.needs
    @State private var needsPath: [Route] = []
    @State private var projectsPath: [Route] = []

    var body: some View {
        if model.source == .none {
            Welcome()
        } else {
            // .tabItem, not Tab: that one is iOS 18's.
            TabView(selection: $tab) {
                NavigationStack(path: $needsPath) {
                    NeedsScreen().destinations()
                }
                .tabItem { Label("Needs you", systemImage: "exclamationmark.bubble") }
                .badge(model.needs.count)
                .tag(RootTab.needs)
                NavigationStack(path: $projectsPath) {
                    ProjectsScreen().destinations()
                }
                .tabItem { Label("Projects", systemImage: "folder") }
                .tag(RootTab.projects)
            }
            .onAppear(perform: openLaunched)
        }
    }
}

extension Root {
    /// -open: "projects", or a route opened from the Projects tab (a
    /// project's session under its project, as a tap would have it).
    private func openLaunched() {
        guard let open = UserDefaults.standard.string(forKey: "open") else { return }
        if open == "projects" {
            tab = .projects
        } else if let r = Route.parse(open) {
            tab = .projects
            if case .session(let ref) = r { projectsPath = [.project(ref.project), r] } else { projectsPath = [r] }
        }
    }
}

extension View {
    /// The screens a Route opens.
    func destinations() -> some View {
        navigationDestination(for: Route.self) { r in
            switch r {
            case .project(let slug): ProjectScreen(slug: slug)
            case .peerProject(let alias, let project): PeerProjectScreen(alias: alias, project: project)
            case .session(let ref): ConversationScreen(ref: ref)
            }
        }
    }

    /// A screen's backdrop: the theme's base, under its lists.
    func backdrop() -> some View {
        modifier(Backdrop())
    }
}

private struct Backdrop: ViewModifier {
    @Environment(\.theme) private var theme

    func body(content: Content) -> some View {
        content
            .scrollContentBackground(.hidden)
            .background {
                LinearGradient(colors: [theme.mesh[0], theme.base, theme.mesh[3]], startPoint: .topLeading, endPoint: .bottomTrailing)
                    .ignoresSafeArea()
            }
    }
}
