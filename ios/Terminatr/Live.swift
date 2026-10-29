import SwiftUI
import TerminatrKit
import TerminatrUI

extension View {
    /// The work running on the server above the tab bar (T849): from
    /// iOS 26.1 the tab bar's accessory, there only while something
    /// works; before, a strip inset at the foot of the tab's screens.
    /// open takes the session that has worked longest.
    func liveStrip(_ work: LiveWork?, open: @escaping (SessionRef) -> Void) -> some View {
        modifier(LiveStripModifier(work: work, open: open))
    }
}

private struct LiveStripModifier: ViewModifier {
    let work: LiveWork?
    let open: (SessionRef) -> Void

    func body(content: Content) -> some View {
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
    /// it.
    @available(iOS 26.1, *)
    private struct Accessory: View {
        @Environment(\.tabViewBottomAccessoryPlacement) private var placement
        let work: LiveWork
        let open: (SessionRef) -> Void

        var body: some View {
            LiveStrip(work, compact: placement == .inline) { open(work.ref) }
                .hoverEffect()
        }
    }
}
