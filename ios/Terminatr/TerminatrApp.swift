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
/// internal/e2e TestPhoneDev's codes); -sidebar NO keeps an iPad's
/// sidebar closed as it starts; -open settings shows Settings; on Welcome -open pair (or "pair CODE") opens pairing. A terminatr://pair link (the
/// Camera app's scan of the code) opens the pairing screen.
@main
struct TerminatrApp: App {
    @UIApplicationDelegateAdaptor private var delegate: AppDelegate
    @State private var phone = Phone()
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
                .environment(phone)
                .themed()
                .onHinge()
                .task {
                    if UserDefaults.standard.bool(forKey: "demo"), phone.isEmpty {
                        await phone.startDemo()
                    }
                    if let s = UserDefaults.standard.string(forKey: "pair"), let c = try? PairingCode(s) {
                        do {
                            try await phone.pair(c, name: UIDevice.current.name)
                        } catch {
                            phone.failed(error)
                        }
                    }
                    phone.openPaired()
                }
                .onOpenURL { url in
                    if let c = try? PairingCode(url.absoluteString) { linked = LinkedCode(code: c) }
                }
                .sheet(item: $linked) { PairScreen(code: $0.code).environment(phone) }
        }
        .onChange(of: phase) { _, p in
            switch p {
            case .active: phone.resume()
            case .background: Grace.begin(phone)
            default: break
            }
        }
    }
}

/// The link's grace behind another app (LinkKeep.backgroundGrace): a
/// background task keeps the app running while the link stays up, and
/// when iOS ends it first, the link closes then.
@MainActor final class Grace {
    private var id = UIBackgroundTaskIdentifier.invalid

    static func begin(_ model: Phone) {
        let g = Grace()
        g.id = UIApplication.shared.beginBackgroundTask(withName: "link") {
            Task { @MainActor in
                await model.suspend()
                g.end()
            }
        }
        Task {
            await model.toBack()
            g.end()
        }
    }

    private func end() {
        guard id != .invalid else { return }
        UIApplication.shared.endBackgroundTask(id)
        id = .invalid
    }
}

/// A pairing code a link brought, for the sheet.
struct LinkedCode: Identifiable {
    let id = UUID()
    let code: PairingCode
}

extension Route {
    /// This route on the server key (keeps the one it names).
    func on(_ key: String) -> Route {
        if case .on = self { return self }
        return key.isEmpty ? self : .on(key, self)
    }

    /// The route without its server.
    var base: Route {
        if case .on(_, let r) = self { return r.base }
        return self
    }

    /// The server's key it names, if any.
    var server: String? {
        if case .on(let k, _) = self { return k }
        return nil
    }

    /// The project of this server whose screen this is, which the
    /// Search tab searches (T929); nil for a paired server's project,
    /// which this server's search doesn't reach.
    var searchSlug: String? {
        switch base {
        case .project(let s), .overview(let s), .board(let s), .memory(let s), .memoryFile(let s, _),
             .library(let s), .task(let s, _), .mods(let s), .mod(let s, _), .design(let s, _),
             .search(let s), .searchHit(let s, _):
            s
        case .session(let ref), .need(let ref): ref.isRemote ? nil : ref.project
        case .on, .peerProject, .peerPage, .peerDesign: nil
        }
    }
}

/// Where a tap goes.
indirect enum Route: Hashable {
    /// A route on one of the servers (T852), by its pairing's key
    /// (PhoneModel.key): its screen reads that server. A route without
    /// one (a launch argument's) reads the first.
    case on(String, Route)
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
    /// A project's mods (T869), and one of them by its id: on or off
    /// for the project, and its settings for it.
    case mods(String)
    case mod(String, id: String)
    /// A project's design by its name (T864): its answers, open
    /// questions and build.
    case design(String, name: String)
    /// A project's history searched (T924), and one hit by its chunk's
    /// id: its text and its timeline.
    case search(String)
    case searchHit(String, id: String)
    /// A paired server's project's page (T886): its overview, board,
    /// memory (never shared), library or designs.
    case peerPage(alias: String, project: String, page: PeerPages.Page)
    /// A paired server's project's design by its name (T888), read-only.
    case peerDesign(alias: String, project: String, name: String)

    /// A launch argument's route: "project:SLUG", "session:SLUG" (its
    /// coordinator) or "session:SLUG/THREAD", "need:SLUG/THREAD" (its
    /// question or approval); "overview:SLUG",
    /// "board:SLUG", "memory:SLUG", "memory:SLUG/PATH", "library:SLUG",
    /// "mods:SLUG", "mods:SLUG/ID",
    /// "task:SLUG/REF", "design:SLUG/NAME", "search:SLUG",
    /// "search:SLUG/ID", or a paired server's
    /// "peer:ALIAS/PROJECT", "peer:ALIAS/PROJECT/PAGE" and
    /// "peer:ALIAS/PROJECT/designs/NAME".
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
        case "mods": return rest.isEmpty ? .mods(slug) : .mod(slug, id: rest)
        case "task": return rest.isEmpty ? nil : .task(slug, ref: rest)
        case "design": return rest.isEmpty ? nil : .design(slug, name: rest)
        case "search": return rest.isEmpty ? .search(slug) : .searchHit(slug, id: rest)
        // "peer:ALIAS/PROJECT", "peer:ALIAS/PROJECT/PAGE" (overview,
        // board, memory, library, designs).
        case "peer":
            let q = rest.split(separator: "/", maxSplits: 1).map(String.init)
            guard !q.isEmpty else { return nil }
            if q.count == 1 { return .peerProject(alias: slug, project: q[0]) }
            if q[1].hasPrefix("designs/"), q[1].count > "designs/".count {
                return .peerDesign(alias: slug, project: q[0], name: String(q[1].dropFirst("designs/".count)))
            }
            guard let page = PeerPages.Page.allCases.first(where: { $0.rawValue.lowercased() == q[1] }) else { return nil }
            return .peerPage(alias: slug, project: q[0], page: page)
        default: return nil
        }
    }

    /// The Projects tab's path to it, as taps would have it: a project's
    /// screens under the project, a memory file under Memory, a task
    /// under the board.
    var path: [Route] {
        switch self {
        case .on(let key, let r): r.path.map { $0.on(key) }
        case .project, .peerProject: [self]
        case .session(let ref), .need(let ref):
            [ref.isRemote ? .peerProject(alias: ref.server, project: ref.project) : .project(ref.project), self]
        case .overview(let s), .board(let s), .memory(let s), .library(let s), .design(let s, _), .mods(let s), .search(let s): [.project(s), self]
        case .searchHit(let s, _): [.project(s), .search(s), self]
        case .mod(let s, _): [.project(s), .mods(s), self]
        case .memoryFile(let s, _): [.project(s), .memory(s), self]
        case .task(let s, _): [.project(s), .board(s), self]
        case .peerPage(let alias, let project, _), .peerDesign(let alias, let project, _): [.peerProject(alias: alias, project: project), self]
        }
    }
}

enum RootTab: Hashable {
    case needs, projects
    /// Search (T929): the project in view's history (SearchScope).
    case search
    /// A row of the iPad's sidebar (T849): a session or an inactive
    /// project, opened in the Projects tab's columns.
    case place(Route)
}

struct Root: View {
    @Environment(Phone.self) private var phone
    @Environment(\.theme) private var theme
    @State private var tab = RootTab.needs
    @State private var needsPath: [Route] = []
    @State private var projectsPath: [Route] = []
    @State private var searchPath: [Route] = []
    /// The project the Search tab searches, picked as it's chosen.
    @State private var searchScope: SearchScope?
    @State private var settings = false
    @State private var gallery = false
    /// The window's size, for the iPad's sidebar (folding).
    @State private var size = CGSize.zero
    /// Whether the iPad's sidebar was opened at launch (opening).
    @State private var sidebarOpened = false

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
            // The screens that read one server read the first, unless
            // their route names another (RouteScreen); the sheets too.
            .environment(phone.primary)
    }

    @ViewBuilder private var content: some View {
        if phone.isEmpty {
            Welcome()
        } else if #available(iOS 18, *), Self.sidebar {
            sidebarTabs
                .onAppear(perform: openLaunched)
                .onChange(of: projectsPath) { if tab != .needs, tab != .search { tab = Self.place(projectsPath, in: phone.sidebar) } }
        } else if #available(iOS 18, *) {
            phoneTabs
                .onAppear(perform: openLaunched)
        } else {
            // .tabItem, not Tab: that one is iOS 18's. No Search tab:
            // the project screen and the Projects header keep their
            // Search buttons (Root.searchTab).
            TabView(selection: $tab) {
                needsStack
                    .tabItem { Label("Needs you", systemImage: "exclamationmark.bubble") }
                    .badge(phone.needs.count)
                    .tag(RootTab.needs)
                projectsStack
                    .tabItem { Label("Projects", systemImage: "folder") }
                    .tag(RootTab.projects)
            }
            .onAppear(perform: openLaunched)
        }
    }

    private var needsStack: some View {
        TabStack(path: $needsPath, key: "needs", empty: ("Pick a need", "exclamationmark.bubble")) {
            NeedsScreen()
        }
    }

    private var projectsStack: some View {
        TabStack(path: $projectsPath, key: "projects", empty: ("Pick a project", "folder")) {
            ProjectsScreen()
        }
    }

    /// The iPhone's tabs, the iPhone Duo's too (its rail on iOS 27.1):
    /// Needs you, Projects and Search.
    @available(iOS 18, *)
    private var phoneTabs: some View {
        TabView(selection: Binding(get: { tab }, set: choose)) {
            Tab("Needs you", systemImage: "exclamationmark.bubble", value: RootTab.needs) { needsStack }
                .badge(phone.needs.count)
            Tab("Projects", systemImage: "folder", value: RootTab.projects) { projectsStack }
            searchTab
        }
    }
}

/// The Search tab (T929): the project search the project screen's
/// Search button opened (SearchScreen), as a tab of the app's own, role
/// .search: on an iPhone the tab bar's search circle, on an open iPhone
/// Duo in the rail, on an iPad the sidebar's first row. It is the
/// TabView's, never a screen's bar item, so no page or column change
/// leaves it behind (Columns builds each column's stack afresh, T928),
/// and it takes the project screen's button's place (searchTab), as
/// the Projects header's: one Search on every screen. Chosen, it
/// searches the project in view (the last screen of the tab left that
/// has one, this server's), else the first project of the first server
/// that searches, as the Projects header's did; the screen's title
/// picks another of that server's. Its hits and what they open stay in
/// its own stack while the project in view stays the same.
struct SearchScope: Hashable {
    /// The server's key (PhoneModel.key).
    let key: String
    let slug: String
}

extension Root {
    /// Whether the app has the Search tab (Tab's role is iOS 18's);
    /// without it the screens keep their Search buttons.
    static var searchTab: Bool {
        if #available(iOS 18, *) { true } else { false }
    }

    /// Whether any server searches (Compatibility.searches): else no tab.
    private var searches: Bool { phone.models.contains(where: \.searches) }

    @available(iOS 18, *)
    @TabContentBuilder<RootTab>
    private var searchTab: some TabContent<RootTab> {
        if searches {
            Tab(value: RootTab.search, role: .search) { searchStack }
        }
    }

    @ViewBuilder
    private var searchStack: some View {
        if let scope = searchScope, let m = phone.model(scope.key) {
            TabStack(path: $searchPath, key: "search", empty: ("Pick a hit", "magnifyingglass")) {
                SearchScreen(slug: scope.slug)
            }
            .environment(m)
            .id(scope)
        } else {
            ContentUnavailableView("Nothing to search", systemImage: "magnifyingglass")
                .backdrop()
        }
    }

    /// The path of the tab t shows (the sidebar's rows show Projects').
    private func path(_ t: RootTab) -> [Route] {
        switch t {
        case .needs: needsPath
        case .search: searchPath
        case .projects, .place: projectsPath
        }
    }

    /// What Search searches, chosen from tab: the project of its last
    /// screen that has one on a server that searches, else the first
    /// project of the first server that searches.
    private func scope(from t: RootTab) -> SearchScope? {
        for r in path(t).reversed() {
            guard let slug = r.searchSlug else { continue }
            let m = r.server.flatMap(phone.model) ?? phone.primary
            if m.searches { return SearchScope(key: m.key, slug: slug) }
        }
        for m in phone.models where m.searches {
            if let p = (m.activeProjects + m.inactiveProjects).first { return SearchScope(key: m.key, slug: p.slug) }
        }
        return nil
    }

    /// A tab chosen. Search searches the project in view as it's
    /// chosen; a new project starts its search afresh. A row picked in
    /// the sidebar opens its route in the columns.
    private func choose(_ t: RootTab) {
        if t == .search, tab != .search, let s = scope(from: tab), s != searchScope {
            searchScope = s
            searchPath = []
        }
        tab = t
        if case .place(let r) = t { projectsPath = r.path }
    }
}

/// The iPad's sidebar (T849): Needs you and Projects, then each
/// project's coordinator and threads in the TUI's order (Phone.sidebar, by server),
/// each paired server's shared ones, and the inactive projects. A row
/// opens its session in the Projects tab's columns, beside its
/// project; the sidebar follows the columns as they move on. An iPhone
/// and the Duo keep the tab bar (and the Duo's rail).
extension Root {
    static var sidebar: Bool { UIDevice.current.userInterfaceIdiom == .pad }

    @available(iOS 18, *)
    private var sidebarTabs: some View {
        TabView(selection: Binding(get: { tab }, set: choose)) {
            Tab("Needs you", systemImage: "exclamationmark.bubble", value: RootTab.needs) { opening(needsStack) }
                .badge(phone.needs.count)
            Tab("Projects", systemImage: "folder", value: RootTab.projects) { opening(projectsStack) }
            searchTab
            ForEach(phone.sidebar) { s in
                TabSection(s.title) {
                    ForEach(s.rows) { r in
                        Tab(value: RootTab.place(r.route)) {
                            opening(projectsStack)
                        } label: {
                            Label {
                                Text(r.title)
                            } icon: {
                                Image(systemName: r.tone.symbol)
                                    .foregroundStyle(theme.colour(r.tone))
                            }
                        }
                        .badge(r.badge.map { Text($0) })
                        .defaultVisibility(.hidden, for: .tabBar)
                    }
                }
                .defaultVisibility(.hidden, for: .tabBar)
            }
        }
        .tabViewStyle(.sidebarAdaptable)
        // Given its models: laid out again on a rotation, its cell's
        // host has no Phone from the scene, and reading it crashed (T895).
        .tabViewSidebarHeader { SidebarHead().environment(phone).environment(phone.primary) }
        .onGeometryChange(for: CGSize.self) { $0.size } action: { size = $0 }
        .onChange(of: folding) { if $1 != nil { Self.setSidebar(hidden: true) } }
    }

    /// A tab's content, which opens the sidebar as the app starts (T858):
    /// as the first tab appears, its UITabBarController is there to ask.
    /// From Root's onAppear it sometimes wasn't yet (1 portrait launch
    /// in 10 to 20), and the sidebar stayed closed, its rows in the top
    /// tab bar. -sidebar NO leaves it closed (the App Store's portrait
    /// screenshots, T911: open, it lies over the list).
    @available(iOS 18, *)
    private func opening(_ content: some View) -> some View {
        content.background(TabBarHook { tabs in
            guard !sidebarOpened else { return }
            sidebarOpened = true
            let launched = UserDefaults.standard.object(forKey: "sidebar") == nil || UserDefaults.standard.bool(forKey: "sidebar")
            if folding == nil, launched { tabs.sidebar.isHidden = false }
        })
    }

    /// The coordinator or thread the columns show across the whole
    /// width, the list folded away (Columns, PhoneColumns.foldsList),
    /// else nil. There the sidebar folds away too (T862): in portrait
    /// it lies over the content, and covered the session's composer
    /// dock. It folds as such a session opens or the window turns to
    /// fit one; its button opens it again, over the session.
    private var folding: Route? {
        guard size != .zero else { return nil }
        let last = path(tab).last
        switch last?.base {
        case .session, .need: return PhoneColumns.foldsList(size) ? last : nil
        default: return nil
        }
    }

    /// Opens or folds the sidebar: SwiftUI names no way to (in portrait
    /// the TabView starts with it closed behind its button). Opened once
    /// as the app starts (opening); the user opens and closes it as ever.
    @available(iOS 18, *)
    private static func setSidebar(hidden: Bool) {
        func tabs(_ vc: UIViewController?) -> UITabBarController? {
            guard let vc else { return nil }
            if let t = vc as? UITabBarController { return t }
            return vc.children.lazy.compactMap(tabs).first
        }
        for case let scene as UIWindowScene in UIApplication.shared.connectedScenes {
            for w in scene.windows { tabs(w.rootViewController)?.sidebar.isHidden = hidden }
        }
    }

    /// The sidebar's row for what the columns show: the last route on
    /// path that has a row, else Projects.
    static func place(_ path: [Route], in sidebar: [SidebarGroup]) -> RootTab {
        let rows = Set(sidebar.flatMap { $0.rows.map(\.route) })
        return path.last(where: rows.contains).map(RootTab.place) ?? .projects
    }
}

extension Root {
    /// Opens route from the Projects tab, on the path a tap would have
    /// taken (Route.path).
    private func open(on route: Route) {
        tab = .projects
        projectsPath = route.path
    }

    /// -open: "settings", "gallery" (the redesign's components, T845;
    /// with -gallery-lower YES at its states and buttons),
    /// "projects", "needs ROUTE" (a need's route
    /// opened from Needs you), "search:SLUG" or "search:SLUG/ID" in the
    /// Search tab (on iOS 17 from the project, as below), or a route
    /// opened from the Projects tab, on the path a tap would have taken
    /// (Route.path).
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
            if Self.searchTab, case .search(let slug) = r {
                openSearch(slug)
            } else if Self.searchTab, case .searchHit(let slug, _) = r {
                openSearch(slug)
                searchPath = [r]
            } else {
                self.open(on: r)
            }
        }
    }

    /// The Search tab on the first server's project slug (-open search:SLUG).
    private func openSearch(_ slug: String) {
        searchScope = SearchScope(key: phone.primary.key, slug: slug)
        searchPath = []
        tab = .search
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

/// The screen a Route opens, reading the server it names (T852).
struct RouteScreen: View {
    @Environment(Phone.self) private var phone
    let route: Route

    var body: some View {
        if case .on(let key, let r) = route {
            if let m = phone.model(key) {
                BaseRouteScreen(route: r).environment(m)
            } else {
                ContentUnavailableView("Not paired with this server now", systemImage: "iphone.slash")
                    .backdrop()
            }
        } else {
            BaseRouteScreen(route: route)
        }
    }
}

/// The screen a route without its server opens, on the environment's.
private struct BaseRouteScreen: View {
    let route: Route

    var body: some View {
        switch route {
        case .on(_, let r): BaseRouteScreen(route: r)
        case .project(let slug): ProjectScreen(slug: slug)
        case .peerProject(let alias, let project): PeerProjectScreen(alias: alias, project: project)
        case .session(let ref): SessionScreen(ref: ref)
        case .need(let ref): NeedConversation(ref: ref)
        case .overview(let slug): OverviewScreen(slug: slug)
        case .board(let slug): BoardScreen(slug: slug)
        case .memory(let slug): MemoryScreen(slug: slug)
        case .memoryFile(let slug, let path): MemoryFileScreen(slug: slug, path: path)
        case .library(let slug): LibraryScreen(slug: slug)
        case .mods(let slug): ModsScreen(slug: slug)
        case .mod(let slug, let id): ModScreen(slug: slug, id: id)
        case .task(let slug, let ref): TaskScreen(slug: slug, ref: ref)
        case .design(let slug, let name): DesignScreen(slug: slug, name: name)
        case .search(let slug): SearchScreen(slug: slug)
        case .searchHit(let slug, let id): SearchHitScreen(slug: slug, id: id)
        case .peerPage(let alias, let project, let page): PeerPageScreen(alias: alias, project: project, page: page)
        case .peerDesign(let alias, let project, let name): DesignScreen(slug: project, name: name, peer: alias)
        }
    }
}

/// The mesh's colours as a gradient; with Reduce Transparency the base
/// alone, as Backdrop leaves it.
private struct BackdropModifier: ViewModifier {
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceTransparency) private var reduceTransparency

    func body(content: Content) -> some View {
        content
            .scrollContentBackground(.hidden)
            .background {
                Group {
                    if reduceTransparency {
                        theme.base
                    } else {
                        LinearGradient(colors: [theme.mesh[0], theme.base, theme.mesh[3]], startPoint: .topLeading, endPoint: .bottomTrailing)
                    }
                }
                .ignoresSafeArea()
            }
    }
}

/// Hands found the UITabBarController a TabView shows its tabs in, each
/// time the tab holding it appears: from inside a tab the controller is
/// always reachable, where a walk of the window's controllers can run
/// before SwiftUI has put it there (T858).
struct TabBarHook: UIViewControllerRepresentable {
    let found: (UITabBarController) -> Void

    func makeUIViewController(context: Context) -> Hook { Hook(found: found) }
    func updateUIViewController(_ hook: Hook, context: Context) { hook.found = found }

    final class Hook: UIViewController {
        var found: (UITabBarController) -> Void

        init(found: @escaping (UITabBarController) -> Void) {
            self.found = found
            super.init(nibName: nil, bundle: nil)
        }

        @available(*, unavailable)
        required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

        override func viewDidLoad() {
            super.viewDidLoad()
            view.isUserInteractionEnabled = false
        }

        override func viewWillAppear(_ animated: Bool) {
            super.viewWillAppear(animated)
            if let tabs = tabBarController { found(tabs) }
        }
    }
}
