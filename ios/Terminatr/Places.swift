import SwiftUI
import TerminatrUI

/// Where the user is on each screen, kept beside the screens (T825):
/// half-typed text, folds, a sheet left open, the conversation's scroll
/// place, the row a list last opened. Opening or closing an iPhone Duo
/// (a resize between compact and regular width), or an iPad's window
/// crossing to regular, lays the screens out afresh in the stack or the
/// columns (TabStack), and their @State goes with the old layout; what
/// they keep here (View.kept) comes back. Not observed: a screen reads
/// it when it appears. Forgotten with the server (PhoneModel
/// disconnect).
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
    /// This route's screen as a list, for Places.chosen.
    var listKey: String { "\(self)" }
}

/// How a screen's links open their routes. In the stack (compact width)
/// none is set and a link is a NavigationLink; in the columns each
/// column sets its own: the list's opens beside it, the detail's opens
/// the next pair of columns (Columns).
struct Navigate {
    /// The route open beside the list: its row shows selected.
    let open: Route?
    let go: (Route) -> Void
}

extension EnvironmentValues {
    @Entry var navigate: Navigate?
}

/// A link to a route: a NavigationLink in the stack, a button that
/// opens it in the columns, its row marked while its route is open
/// beside it, and lit under an iPad's pointer (T830), as a
/// NavigationLink's row is. List rows keep the chevron either way.
struct RouteLink<Label: View>: View {
    @Environment(\.navigate) private var navigate
    @Environment(\.theme) private var theme
    let route: Route
    @ViewBuilder let label: Label

    init(value route: Route, @ViewBuilder label: () -> Label) {
        self.route = route
        self.label = label()
    }

    var body: some View {
        if let navigate {
            let on = navigate.open == route
            Button {
                navigate.go(route)
            } label: {
                HStack {
                    label
                    Spacer(minLength: 0)
                    Image(systemName: "chevron.right")
                        .font(theme.body(.footnote, .semibold))
                        .foregroundStyle(theme.muted)
                        .accessibilityHidden(true)
                }
                .contentShape([.interaction, .hoverEffect], Rectangle())
            }
            .buttonStyle(.plain)
            .hoverEffect(.highlight)
            .listRowBackground(on ? theme.action.opacity(0.16) : nil)
            .accessibilityAddTraits(on ? .isSelected : [])
            .id(route)
        } else {
            NavigationLink(value: route) { label }
                .id(route)
        }
    }
}

/// A plain link, not a list row (a card, a button in a page): the same
/// choice, without the chevron; lit under an iPad's pointer.
struct PlainRouteLink<Label: View>: View {
    @Environment(\.navigate) private var navigate
    let route: Route
    @ViewBuilder let label: Label

    init(value route: Route, @ViewBuilder label: () -> Label) {
        self.route = route
        self.label = label()
    }

    var body: some View {
        if let navigate {
            Button { navigate.go(route) } label: { label }
                .hoverEffect()
                .accessibilityAddTraits(navigate.open == route ? .isSelected : [])
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

    func body(content: Content) -> some View {
        ScrollViewReader { proxy in
            content.onAppear {
                if let r = model.places.chosen(list) { proxy.scrollTo(r) }
            }
        }
    }
}
