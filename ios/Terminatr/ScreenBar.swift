import SwiftUI
import TerminatrUI
import UIKit

/// A screen's own bar (T925, T1001), in place of the navigation bar and
/// its glass buttons: Back, when there is somewhere to go back to, and the
/// screen's trailing buttons, pinned over its top on the screen's solid
/// colour, laid out by the screen's gutter as its content is. The
/// navigation bar keeps its own margins whatever the screen's: 16 pt on an
/// iPhone, 10 pt in an iPad's column, and in a column on an open iPhone Duo
/// the window's inset on the inner edge, which the content ignores
/// (InnerEdge), so a trailing button there sat 21 pt short of the content's
/// edge; and on an open iPhone Duo its items go to the rail. A page's bar
/// has its title and a line under it (the design page's `.phbar`); a screen
/// that opens with its own big title (ProjectHeader) has neither. The
/// screen keeps the navigation title too, for VoiceOver and the screen it
/// opens.
struct ScreenBar<Trailing: View>: ViewModifier {
    @Environment(\.theme) private var theme
    @Environment(\.back) private var back
    @Environment(\.isPresented) private var pushed
    @Environment(\.dismiss) private var dismiss
    @Environment(\.ground) private var ground
    let title: String?
    /// Whether it has no trailing buttons: with no Back either, no bar.
    var bare = false
    @ViewBuilder let trailing: () -> Trailing

    /// Back to the list before this one in the columns (Columns sets it),
    /// else out of the stack it was pushed on; at the root of a column, none.
    private var go: (() -> Void)? {
        back?.action ?? (pushed ? { dismiss() } : nil)
    }

    func body(content: Content) -> some View {
        content
            .safeAreaInset(edge: .top, spacing: 0) {
                if title != nil || go != nil || !bare { row }
            }
            .toolbar(.hidden, for: .navigationBar)
            .swipesBack()
    }

    private var row: some View {
        HStack(spacing: Spacing.item) {
            if let go {
                Button("Back", systemImage: "chevron.left", action: go)
                    .buttonStyle(.circle)
                    .accessibilityIdentifier("BackButton")
            }
            if let title {
                Text(title)
                    .font(theme.sized(14.5, .medium))
                    .foregroundStyle(theme.ink)
                    .lines(1)
                    .accessibilityAddTraits(.isHeader)
            }
            Spacer(minLength: 0)
            trailing()
                .buttonStyle(.circle)
        }
        .padding(.horizontal, Spacing.gutter)
        .padding(.vertical, Spacing.tight)
        .frame(minHeight: 30 * theme.scale + 2 * Spacing.tight)
        .background((title != nil || ground == .chrome ? theme.chrome : theme.gutter).ignoresSafeArea(edges: .top))
        .overlay(alignment: .bottom) {
            if title != nil { theme.line.frame(height: 1) }
        }
    }
}

extension View {
    /// The screen's own bar (ScreenBar): Back and trailing's buttons.
    func screenBar<Trailing: View>(bare: Bool = false, @ViewBuilder trailing: @escaping () -> Trailing) -> some View {
        modifier(ScreenBar(title: nil, bare: bare, trailing: trailing))
    }

    /// A page's own bar: Back, its title and trailing's buttons.
    func pageBar<Trailing: View>(_ title: String, @ViewBuilder trailing: @escaping () -> Trailing) -> some View {
        navigationTitle(title)
            .modifier(ScreenBar(title: title, trailing: trailing))
    }

    func pageBar(_ title: String) -> some View {
        pageBar(title) { EmptyView() }
    }
}

/// A swipe in from the leading edge goes back on a screen that hides the
/// navigation bar, as it does with one: UIKit turns that swipe off with the
/// bar, by its own gesture's delegate.
private struct SwipeBack: UIViewControllerRepresentable {
    func makeUIViewController(context: Context) -> Hook { Hook() }
    func updateUIViewController(_ hook: Hook, context: Context) {}

    final class Hook: UIViewController, UIGestureRecognizerDelegate {
        override func viewDidLoad() {
            super.viewDidLoad()
            view.isUserInteractionEnabled = false
        }

        override func viewDidAppear(_ animated: Bool) {
            super.viewDidAppear(animated)
            guard let pop = navigationController?.interactivePopGestureRecognizer else { return }
            pop.isEnabled = true
            pop.delegate = self
        }

        func gestureRecognizerShouldBegin(_ g: UIGestureRecognizer) -> Bool {
            (navigationController?.viewControllers.count ?? 0) > 1
        }
    }
}

extension View {
    func swipesBack() -> some View {
        background(SwipeBack().frame(width: 0, height: 0).accessibilityHidden(true))
    }
}

/// Goes back to the list before this one in the columns (Columns).
struct GoBack {
    let action: () -> Void
}

/// Slides the folded list out over a session (Columns.folded).
struct ShowList {
    let shown: Bool
    let toggle: () -> Void
}

extension EnvironmentValues {
    /// The columns' Back, nil outside them or with no list before.
    @Entry var back: GoBack?
    /// The list button of a session that folds the list away.
    @Entry var showList: ShowList?
    /// The colour behind a screen's panels (backdrop): the window's, or
    /// behind the panes in the columns' detail, as the design page's.
    @Entry var ground: ScreenGround = .chrome
}

enum ScreenGround {
    case chrome, gutter
}
