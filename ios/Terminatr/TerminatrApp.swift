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
/// screen: the Simulator's screenshots; with -pair CODE it pairs with
/// that code at once, as this phone (the Simulator has no camera:
/// internal/e2e TestPhoneDev's codes); -open settings shows Settings; on Welcome -open pair (or "pair CODE") opens pairing. A terminatr://pair link (the
/// Camera app's scan of the code) opens the pairing screen.
@main
struct TerminatrApp: App {
    @UIApplicationDelegateAdaptor private var delegate: AppDelegate
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
                .themed()
                .onHinge()
                .task {
                    if UserDefaults.standard.bool(forKey: "demo"), model.source == .none {
                        await model.startDemo()
                    }
                    if let s = UserDefaults.standard.string(forKey: "pair"), let c = try? PairingCode(s) {
                        do {
                            try await model.pair(c, name: UIDevice.current.name)
                        } catch {
                            model.failed(error)
                        }
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
    /// A session's question or approval from Needs you (T826): its
    /// conversation with the need's card above it, scrolled to where it
    /// was asked.
    case need(SessionRef)
    /// A project's pages (T798): its overview, board, memory, a memory
    /// file by its path, library, and a task by its ref.
    case overview(String)
    case board(String)
    case memory(String)
    case memoryFile(String, path: String)
    case library(String)
    case task(String, ref: String)

    /// A launch argument's route: "project:SLUG", "session:SLUG" (its
    /// coordinator) or "session:SLUG/THREAD", "need:SLUG/THREAD" (its
    /// question or approval); "overview:SLUG",
    /// "board:SLUG", "memory:SLUG", "memory:SLUG/PATH", "library:SLUG"
    /// or "task:SLUG/REF".
    static func parse(_ s: String) -> Route? {
        let parts = s.split(separator: ":", maxSplits: 1).map(String.init)
        guard parts.count == 2 else { return nil }
        let p = parts[1].split(separator: "/", maxSplits: 1).map(String.init)
        let slug = p[0], rest = p.count > 1 ? p[1] : ""
        switch parts[0] {
        case "project": return .project(parts[1])
        case "session": return .session(SessionRef(project: slug, thread: rest))
        case "need": return .need(SessionRef(project: slug, thread: rest))
        case "overview": return .overview(slug)
        case "board": return .board(slug)
        case "memory": return rest.isEmpty ? .memory(slug) : .memoryFile(slug, path: rest)
        case "library": return .library(slug)
        case "task": return rest.isEmpty ? nil : .task(slug, ref: rest)
        default: return nil
        }
    }

    /// The Projects tab's path to it, as taps would have it: a project's
    /// screens under the project, a memory file under Memory, a task
    /// under the board.
    var path: [Route] {
        switch self {
        case .project, .peerProject: [self]
        case .session(let ref), .need(let ref): [.project(ref.project), self]
        case .overview(let s), .board(let s), .memory(let s), .library(let s): [.project(s), self]
        case .memoryFile(let s, _): [.project(s), .memory(s), self]
        case .task(let s, _): [.project(s), .board(s), self]
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
    @State private var settings = false
    @State private var gallery = false

    var body: some View {
        content
            .environment(\.showSettings, ShowSettings { settings = true })
            .sheet(isPresented: $settings) { SettingsScreen() }
            .fullScreenCover(isPresented: $gallery) {
                ComponentGallery(lower: UserDefaults.standard.bool(forKey: "gallery-lower"))
                    .overlay(alignment: .topLeading) {
                        Button("Done") { gallery = false }
                            .buttonStyle(.ghost)
                            .padding(.horizontal, 16)
                    }
            }
    }

    @ViewBuilder private var content: some View {
        if model.source == .none {
            Welcome()
        } else {
            // .tabItem, not Tab: that one is iOS 18's.
            TabView(selection: $tab) {
                TabStack(path: $needsPath, key: "needs", empty: ("Pick a need", "exclamationmark.bubble")) {
                    NeedsScreen()
                }
                .tabItem { Label("Needs you", systemImage: "exclamationmark.bubble") }
                .badge(model.needs.count)
                .tag(RootTab.needs)
                TabStack(path: $projectsPath, key: "projects", empty: ("Pick a project", "folder")) {
                    ProjectsScreen()
                }
                .tabItem { Label("Projects", systemImage: "folder") }
                .tag(RootTab.projects)
            }
            .onAppear(perform: openLaunched)
        }
    }
}

extension Root {
    /// -open: "settings", "gallery" (the redesign's components, T845;
    /// with -gallery-lower YES at its states and buttons),
    /// "projects", "needs ROUTE" (a need's route
    /// opened from Needs you), or a route opened from the Projects tab, on
    /// the path a tap would have taken (Route.path).
    private func openLaunched() {
        guard let open = UserDefaults.standard.string(forKey: "open") else { return }
        if open.hasPrefix("needs "), let r = Route.parse(String(open.dropFirst(6))) {
            needsPath = [r]
        } else if open == "settings" {
            settings = true
        } else if open == "gallery" {
            gallery = true
        } else if open == "projects" {
            tab = .projects
        } else if let r = Route.parse(open) {
            tab = .projects
            projectsPath = r.path
        }
    }
}

extension View {
    /// The screens a Route opens.
    func destinations() -> some View {
        navigationDestination(for: Route.self) { RouteScreen(route: $0) }
    }

    /// A screen's backdrop: the theme's base, under its lists.
    func backdrop() -> some View {
        modifier(BackdropModifier())
    }
}

/// The screen a Route opens.
struct RouteScreen: View {
    let route: Route

    var body: some View {
        switch route {
        case .project(let slug): ProjectScreen(slug: slug)
        case .peerProject(let alias, let project): PeerProjectScreen(alias: alias, project: project)
        case .session(let ref): SessionScreen(ref: ref)
        case .need(let ref): NeedConversation(ref: ref)
        case .overview(let slug): OverviewScreen(slug: slug)
        case .board(let slug): BoardScreen(slug: slug)
        case .memory(let slug): MemoryScreen(slug: slug)
        case .memoryFile(let slug, let path): MemoryFileScreen(slug: slug, path: path)
        case .library(let slug): LibraryScreen(slug: slug)
        case .task(let slug, let ref): TaskScreen(slug: slug, ref: ref)
        }
    }
}

private struct BackdropModifier: ViewModifier {
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
