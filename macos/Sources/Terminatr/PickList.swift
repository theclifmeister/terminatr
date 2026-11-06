import SwiftUI
import TerminatrKit
import TerminatrUI

/// A pane's list to pick from, beside what it shows (T1002): flat rows, 6
/// round, the picked one on the selection's fill, as the sidebar's. ↑ and
/// ↓ move the pick while it has the keys; each row is a button to Full
/// Keyboard Access and VoiceOver, which hears the picked one as selected.
struct PickList<ID: Hashable, Content: View>: View {
    @Binding var selection: ID?
    /// The rows' ids, top to bottom: what ↑ and ↓ walk.
    let order: [ID]
    var name: String
    @ViewBuilder var content: Content
    /// A pick takes the keys here, so ↑ and ↓ go on from it.
    @FocusState private var focused: Bool

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 2) {
                    content
                }
                .padding(8)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .scrollEdges()
            .focusable()
            .focusEffectDisabled()
            .focused($focused)
            .onChange(of: selection) { focused = true }
            .onMoveCommand { direction in
                guard let next = PickStep.next(order, from: selection, up: direction == .up, down: direction == .down) else { return }
                selection = next
                proxy.scrollTo(next)
            }
            .accessibilityLabel(name)
        }
    }
}

/// A PickList's row.
struct PickRow<ID: Hashable, Label: View>: View {
    @Environment(\.theme) private var theme
    let id: ID
    @Binding var selection: ID?
    /// A double click's act: Quick Look, in the Library.
    var open: (() -> Void)?
    /// A click's act besides the pick: a Library fold opens.
    var act: (() -> Void)?
    @ViewBuilder var label: Label

    var body: some View {
        let on = selection == id
        let shape = RoundedRectangle(cornerRadius: 6)
        Button {
            selection = id
            act?()
        } label: {
            label
                .font(theme.sized(Flat.text))
                .padding(.horizontal, 8)
                .padding(.vertical, 6)
                .frame(maxWidth: .infinity, minHeight: 32, alignment: .leading)
                .background(on ? theme.selection : .clear, in: shape)
                .contentShape(shape)
        }
        .buttonStyle(.plain)
        .simultaneousGesture(TapGesture(count: 2).onEnded { open?() })
        .accessibilityAddTraits(on ? .isSelected : [])
        .id(id)
    }
}

/// A list and what it shows, with a line and a handle between them.
struct PickSplit<List: View, Detail: View>: View {
    @Environment(\.theme) private var theme
    @Binding var width: Double
    let range: ClosedRange<Double>
    @ViewBuilder var list: List
    @ViewBuilder var detail: Detail

    var body: some View {
        HStack(spacing: 0) {
            list
                .frame(width: CGFloat(min(range.upperBound, max(range.lowerBound, width))))
            theme.line
                .frame(width: 1)
                .overlay { SplitHandle(width: 9, value: $width, sign: 1, range: range, name: "List width") }
            detail
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }
}
