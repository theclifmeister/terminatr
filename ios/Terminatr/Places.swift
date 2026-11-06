import SwiftUI
import TerminatrUI

/// Where the user is on each screen, kept beside the screens (T825):
/// half-typed text, folds, a sheet left open, the conversation's scroll
/// place, the row a list last opened. Opening or closing an iPhone Duo
/// (a resize between compact and regular width), or an iPad's window
/// crossing to regular, lays the screens out afresh in the stack or the
/// columns (TabStack), and their @State goes with the old layout; what
/// they keep here (View.kept) comes back. Not observed: a screen reads
/// it when it appears. One for every server (Phone), forgotten with the
/// last of them and on the demo's way in or out.
@MainActor
final class Places {
    private var values: [String: Any] = [:]

    func value<T>(_ key: String) -> T? {
        values[key] as? T
    }

    /// Keeps value under key; nil forgets it.
    func keep(_ key: String, _ value: Any?) {
        values[key] = value
    }

    func forget() {
        values = [:]
    }

    /// The route a list opened last, by the list's key (Route.listKey).
    func chosen(_ list: String) -> Route? {
        value("chosen " + list)
    }

    /// Notes for each list on path the route it opened, for its
    /// selection and its scroll place when it shows again.
    func choose(_ path: [Route], root: String) {
        for (i, r) in path.enumerated() {
            keep("chosen " + (i == 0 ? root : path[i - 1].listKey), r)
        }
    }
}

extension Route {
    /// This route's screen as a list, for Places.chosen. Written out,
    /// never "\(self)": that walks the enum and its SessionRef by
    /// reflection on each view update that asks for it (T956).
    var listKey: String {
        switch self {
        case .on(let key, let r): "on \(key) " + r.listKey
        case .project(let s): "project \(s)"
        case .peerProject(let alias, let project): "peerProject \(alias)/\(project)"
        case .program(let s): "program \(s)"
        case .session(let ref): "session \(ref.description)"
        case .need(let ref): "need \(ref.description)"
        case .overview(let s): "overview \(s)"
        case .board(let s): "board \(s)"
        case .memory(let s): "memory \(s)"
        case .memoryFile(let s, let path): "memoryFile \(s)/\(path)"
        case .library(let s): "library \(s)"
        case .task(let s, let ref): "task \(s)/\(ref)"
        case .mods(let s): "mods \(s)"
        case .mod(let s, let id): "mod \(s)/\(id)"
        case .designs(let s): "designs \(s)"
        case .design(let s, let name): "design \(s)/\(name)"
        case .search(let s): "search \(s)"
        case .searchHit(let s, let id): "searchHit \(s)/\(id)"
        case .peerPage(let alias, let project, let page): "peerPage \(alias)/\(project)/\(page.rawValue)"
        case .peerDesign(let alias, let project, let name): "peerDesign \(alias)/\(project)/\(name)"
        }
    }
}

/// How a screen's links open their routes. In the stack (compact width)
/// none is set and a link is a NavigationLink; in the columns each
/// column sets its own: the list's opens beside it, the detail's opens
/// the next pair of columns (Columns).
struct Navigate {
    /// The route open beside the list: its row shows selected.
    let open: Route?
    let go: (Route) -> Void

    /// Whether route's screen is the one open beside the list: a need's
    /// conversation is its session's, so a project's row for that session
    /// shows selected beside it too.
    func shows(_ route: Route) -> Bool {
        open?.sessionForm == route.sessionForm
    }
}

extension Route {
    /// The route with a need's conversation as its session's.
    fileprivate var sessionForm: Route {
        switch self {
        case .on(let key, let r): .on(key, r.sessionForm)
        case .need(let ref): .session(ref)
        default: self
        }
    }

    /// The screen above this one, on the path a tap would have taken
    /// (Route.path): a session's project or program.
    var parent: Route? { path.dropLast().last }
}

/// Puts a session's project or program in the list column beside it,
/// when the columns' path doesn't hold it (Columns.showParent).
struct ShowParent {
    let go: () -> Void
}

extension EnvironmentValues {
    @Entry var navigate: Navigate?
    @Entry var showParent: ShowParent?
}

extension Optional where Wrapped == Navigate {
    /// Whether route, on model's server, is the one open beside the list.
    @MainActor func opens(_ route: Route, on model: PhoneModel) -> Bool {
        self?.shows(route.on(model.key)) ?? false
    }
}

/// A plain link, not a list row (a card, a button in a page): the same
/// choice, without the chevron; lit under an iPad's pointer.
struct PlainRouteLink<Label: View>: View {
    @Environment(\.navigate) private var navigate
    @Environment(PhoneModel.self) private var model
    let value: Route
    @ViewBuilder let label: Label

    init(value route: Route, @ViewBuilder label: () -> Label) {
        value = route
        self.label = label()
    }

    private var route: Route { value.on(model.key) }

    var body: some View {
        if let navigate {
            Button { navigate.go(route) } label: { label }
                .hoverEffect()
                .accessibilityAddTraits(navigate.shows(route) ? .isSelected : [])
                .id(route)
        } else {
            NavigationLink(value: route) { label }
                .hoverEffect()
                .id(route)
        }
    }
}

extension View {
    /// A screen's @State value, kept in Places under key as it changes
    /// and put back when the screen appears laid out afresh.
    func kept<T: Equatable>(_ value: Binding<T>, _ key: String) -> some View {
        modifier(Kept(value: value, key: key))
    }

    /// A list that opens routes scrolls back to the one it opened last
    /// when it shows again, in the stack or a column (its RouteLinks
    /// carry their route as their id).
    func keepsPlace(_ list: String) -> some View {
        modifier(KeepsPlace(list: list))
    }

    /// The same for route's screen, on the server it is of.
    func keepsPlace(route: Route) -> some View {
        modifier(KeepsPlace(list: route.listKey, route: route))
    }
}

private struct Kept<T: Equatable>: ViewModifier {
    @Environment(PhoneModel.self) private var model
    @Binding var value: T
    let key: String

    func body(content: Content) -> some View {
        content
            .onAppear {
                if let v: T = model.places.value(key), v != value { value = v }
            }
            .onChange(of: value) { model.places.keep(key, value) }
    }
}

private struct KeepsPlace: ViewModifier {
    @Environment(PhoneModel.self) private var model
    let list: String
    var route: Route?

    func body(content: Content) -> some View {
        ScrollViewReader { proxy in
            content.onAppear {
                if let r = model.places.chosen(route.map { $0.on(model.key).listKey } ?? list) { proxy.scrollTo(r) }
            }
        }
    }
}
