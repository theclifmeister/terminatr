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
        // MetricKit's crash and hang reports of earlier runs, for the
        // paired server only (T957, PhoneModel's link).
        CrashReporter.shared.start()
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
        case .session(let ref), .need(let ref): ref.isRemote || ref.director ? nil : ref.project
        case .on, .peerProject, .program, .peerPage, .peerDesign: nil
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
    /// A program (B19): its rolled-up Needs you, then its members.
    case program(String)
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
    /// coordinator) or "session:SLUG/THREAD", "director:SLUG" (a
    /// program's director), "need:SLUG/THREAD" (its
    /// question or approval); "overview:SLUG",
    /// "board:SLUG", "memory:SLUG", "memory:SLUG/PATH", "library:SLUG",
    /// "mods:SLUG", "mods:SLUG/ID",
    /// "program:SLUG", "task:SLUG/REF", "design:SLUG/NAME", "search:SLUG",
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
        case "program": return .program(parts[1])
        case "director": return .session(.director(parts[1]))
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
        case .project, .peerProject, .program: [self]
        case .session(let ref), .need(let ref):
            [ref.isRemote ? .peerProject(alias: ref.server, project: ref.project) : ref.director ? .program(ref.project) : .project(ref.project), self]
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
    @Environment(\.haptics) private var haptics
    @Environment(\.horizontalSizeClass) private var width
    @State private var tab = RootTab.needs
    @State private var needsPath: [Route] = []
    @State private var projectsPath: [Route] = []
    @State private var searchPath: [Route] = []
    /// The project the Search tab searches, picked as it's chosen.
    @State private var searchScope: SearchScope?
    @State private var settings = false
    @State private var gallery = false
    /// The window's size without the keyboard: where the bar or rail goes.
    @State private var size = CGSize.zero
    /// Whether the iPad's sidebar is open; opened as the app starts.
    @State private var sidebarShown = false
    /// Raised by a second tap of a tab at its root.
    @State private var toTop = 0

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
        } else {
            shell
                .onAppear {
                    openLaunched()
                    openSidebarAtLaunch()
                }
                .onChange(of: projectsPath) {
                    if Self.sidebar, tab != .needs, tab != .search { tab = Self.place(projectsPath, in: phone.sidebar) }
                }
                .onChange(of: folding) { if $1 != nil { sidebarShown = false } }
                .onChange(of: searches) { if !$1, tab == .search { tab = .needs } }
        }
    }

    private var place: NavChrome.Place {
        NavChrome.place(pad: Self.sidebar, regular: width == .regular, size: size)
    }

    /// The bar or rail gives way to a session at a phone's width: its
    /// composer takes the bottom, as with the system's tab bar (T1005).
    /// From the path, as the page's screens are behind a hosting controller.
    private var barHidden: Bool {
        guard width != .regular else { return false }
        switch path(tab).last?.base {
        case .session, .need: return true
        default: return false
        }
    }

    private var pages: [TabPage] {
        searches ? [.needs, .projects, .search] : [.needs, .projects]
    }

    /// The places and the pages, always this one tree: an `if` around the
    /// host would build it afresh, and every tab's stack with it, when a
    /// Duo opens or turns.
    private var shell: some View {
        let count = phone.needs.count
        let sidebarScale = min(theme.scale, Self.sidebarMost)
        let lies = NavChrome.sidebarOverlays(size, sidebar: NavChrome.sidebarWidth(scale: sidebarScale))
        let overlaid = place == .sidebar && sidebarShown && lies
        return ZStack(alignment: .leading) {
            HStack(spacing: 0) {
                if place == .sidebar {
                    if sidebarShown, !lies {
                        sidebar(count).transition(.move(edge: .leading))
                    } else {
                        FlatRail(pages: pages, selected: page(tab), count: count, openSidebar: { showSidebar(true) }, choose: { choose($0.tab) })
                            .navScale()
                    }
                    theme.line.frame(width: 1).ignoresSafeArea()
                }
                VStack(spacing: 0) {
                    TabHost(showing: page(tab), toTop: toTop, page: pageView)
                        .ignoresSafeArea()
                    if place == .bar, !barHidden {
                        FlatBar(pages: pages, selected: page(tab), count: count) { choose($0.tab) }
                            .navScale()
                    }
                }
                if place == .rail, !barHidden {
                    theme.line.frame(width: 1).ignoresSafeArea()
                    FlatRail(pages: pages, selected: page(tab), count: count) { choose($0.tab) }
                        .navScale()
                }
            }
            .accessibilityHidden(overlaid)
            if overlaid {
                Color.black.opacity(0.2)
                    .ignoresSafeArea()
                    .onTapGesture { showSidebar(false) }
                    .accessibilityHidden(true)
                    .transition(.opacity)
                HStack(spacing: 0) {
                    sidebar(count)
                    theme.line.frame(width: 1).ignoresSafeArea()
                }
                .transition(.move(edge: .leading))
                .gesture(DragGesture(minimumDistance: 20).onEnded { if $0.translation.width < -60 { showSidebar(false) } })
                .accessibilityAddTraits(.isModal)
                .accessibilityAction(.escape) { showSidebar(false) }
            }
        }
        .background(theme.chrome.ignoresSafeArea())
        .onGeometryChange(for: CGSize.self) { $0.size } action: { size = $0 }
        // Under the keyboard, as the system's bar: only the page avoids it.
        .ignoresSafeArea(.keyboard)
    }

    private func sidebar(_ count: Int) -> some View {
        FlatSidebar(pages: pages, selected: tab, count: count, hide: { showSidebar(false) }, choose: choose)
            .navScale(most: Self.sidebarMost)
    }

    /// The sidebar's limit on Dynamic Type: past it, its width would crowd
    /// out the content.
    private static let sidebarMost: CGFloat = 1.6

    private func showSidebar(_ shown: Bool) {
        withAnimation(.snappy) { sidebarShown = shown }
    }

    /// The tab's page. A hosting controller is the edge of SwiftUI's
    /// environment, so what Root and Themed set goes across here.
    private func pageView(_ p: TabPage) -> AnyView {
        AnyView(
            pageContent(p)
                .environment(\.showSettings, ShowSettings { settings = true })
                .environment(\.theme, theme)
                .environment(\.haptics, haptics)
                .tint(theme.action)
                .environment(phone.primary)
                .environment(phone)
        )
    }

    @ViewBuilder private func pageContent(_ p: TabPage) -> some View {
        switch p {
        case .needs: needsStack
        case .projects: projectsStack
        case .search: searchStack
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
}

/// The Search tab (T929): the project search the project screen's
/// Search button opened (SearchScreen), as a place of the app's own: in
/// the bar, the rail and the sidebar (T1005). It is Root's, never a
/// screen's bar item, so no page or column change leaves it behind
/// (Columns builds each column's stack afresh, T928), and it takes the
/// place of the buttons the project screen and the Projects header had:
/// one Search on every screen. Chosen, it
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
    /// Whether any server searches (Compatibility.searches): else no tab.
    private var searches: Bool { phone.models.contains(where: \.searches) }

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

    /// The page tab t shows.
    private func page(_ t: RootTab) -> TabPage {
        switch t {
        case .needs: .needs
        case .search: .search
        case .projects, .place: .projects
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
    /// the sidebar opens its route in the columns. The tab showing chosen
    /// again goes back to its root, and at its root to its top.
    private func choose(_ t: RootTab) {
        if case .place(let r) = t {
            tab = t
            projectsPath = r.path
            return
        }
        if page(t) == page(tab) {
            switch page(t) {
            case _ where path(t).isEmpty: toTop += 1
            case .needs: needsPath = []
            case .projects: projectsPath = []
            case .search: searchPath = []
            }
        } else if t == .search, let s = scope(from: tab), s != searchScope {
            searchScope = s
            searchPath = []
        }
        tab = t
    }
}

/// The iPad's sidebar (T849, FlatSidebar): a row opens its session in the
/// Projects tab's columns, beside its project, and follows the columns as
/// they move on. An iPhone and the Duo keep the bar and the rail.
extension Root {
    static var sidebar: Bool { UIDevice.current.userInterfaceIdiom == .pad }

    /// Opens the sidebar as the app starts, unless a session has the whole
    /// width. -sidebar NO leaves it closed (the App Store's portrait
    /// screenshots, T911: open, it lies over the list).
    private func openSidebarAtLaunch() {
        let d = UserDefaults.standard
        sidebarShown = folding == nil && (d.object(forKey: "sidebar") == nil || d.bool(forKey: "sidebar"))
    }

    /// The coordinator or thread the columns show across the whole
    /// width, the list folded away (Columns, PhoneColumns.foldsList),
    /// else nil. There the sidebar folds away too (T862): in portrait
    /// it lies over the content, and covered the session's composer
    /// dock. It folds as such a session opens or the window turns to
    /// fit one; the rail's button opens it again, over the session.
    private var folding: Route? {
        guard size != .zero else { return nil }
        let last = path(tab).last
        switch last?.base {
        case .session, .need: return PhoneColumns.foldsList(size) ? last : nil
        default: return nil
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
    /// Search tab, or a route
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
            if case .search(let slug) = r {
                openSearch(slug)
            } else if case .searchHit(let slug, _) = r {
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
        case .program(let slug): ProgramScreen(slug: slug)
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

/// A screen's solid ground: the window's colour, or behind the panes in
/// the columns' detail (T999, T1001).
private struct BackdropModifier: ViewModifier {
    @Environment(\.theme) private var theme
    @Environment(\.ground) private var ground

    func body(content: Content) -> some View {
        content
            .scrollContentBackground(.hidden)
            .background { (ground == .gutter ? theme.gutter : theme.chrome).ignoresSafeArea() }
    }
}
