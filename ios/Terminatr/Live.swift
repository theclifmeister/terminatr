import SwiftUI
import TerminatrKit
import TerminatrUI

extension View {
    /// The work running on the server above the tab bar (T849): from
    /// iOS 26.1 the tab bar's accessory, there only while something
    /// works; before, a strip inset at the foot of the tab's screens.
    /// open takes the session that has worked longest. A screen that has
    /// the bottom to itself (a session's, with its composer dock, T848)
    /// hides it: hidesLiveStrip.
    func liveStrip(_ work: LiveWork?, open: @escaping (SessionRef) -> Void) -> some View {
        modifier(LiveStripModifier(work: work, open: open))
    }

    /// While shown, the live strip hides (a session's screen at a phone's
    /// width, where the tab bar hides too).
    func hidesLiveStrip(_ hides: Bool = true) -> some View {
        preference(key: HidesLiveStrip.self, value: hides)
    }
}

/// Whether a screen shown asks the live strip to hide.
private struct HidesLiveStrip: PreferenceKey {
    static let defaultValue = false
    static func reduce(value: inout Bool, nextValue: () -> Bool) {
        value = value || nextValue()
    }
}

private struct LiveStripModifier: ViewModifier {
    let work: LiveWork?
    let open: (SessionRef) -> Void
    @State private var hidden = false

    func body(content: Content) -> some View {
        strip(content)
            .onPreferenceChange(HidesLiveStrip.self) { hidden = $0 }
    }

    @ViewBuilder private func strip(_ content: Content) -> some View {
        let work = hidden ? nil : work
        if #available(iOS 26.1, *) {
            content.tabViewBottomAccessory(isEnabled: work != nil) {
                if let work { Accessory(work: work, open: open) }
            }
        } else {
            content.safeAreaInset(edge: .bottom) {
                if let work {
                    LiveStrip(work) { open(work.ref) }
                        .hoverEffect()
                        .padding(.horizontal, Spacing.gutter)
                        .padding(.bottom, Spacing.tight)
                }
            }
        }
    }

    /// In the accessory: compact while the tab bar is minimised beside
    /// it, unframed on the accessory's own glass.
    @available(iOS 26.1, *)
    private struct Accessory: View {
        @Environment(\.tabViewBottomAccessoryPlacement) private var placement
        let work: LiveWork
        let open: (SessionRef) -> Void

        var body: some View {
            LiveStrip(work, compact: placement == .inline, framed: false) { open(work.ref) }
                .hoverEffect()
        }
    }
}
