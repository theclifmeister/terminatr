import SwiftUI

/// A few choices side by side (T850, the phone's Settings › Appearance),
/// the picked one filled in ink with the pane's colour for its words, as a
/// picked PulseChip. Each is at least 44 pt tall;
/// when the words don't fit in a row (accessibility sizes) they stack.
/// Picking is a selection tap.
public struct Segments<ID: Hashable>: View {
    @Environment(\.theme) private var theme
    let items: [(id: ID, label: String)]
    @Binding var selection: ID

    public init(_ items: [(id: ID, label: String)], selection: Binding<ID>) {
        self.items = items
        _selection = selection
    }

    public var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 0) { buttons }
            VStack(spacing: 0) { buttons }
        }
        .padding(4)
        .surface(in: RoundedRectangle(cornerRadius: 8, style: .continuous))
        .feedback(.selection, trigger: selection)
    }

    private var buttons: some View {
        ForEach(items, id: \.id) { item in
            let on = item.id == selection
            Button { selection = item.id } label: {
                Text(item.label)
                    .font(theme.sized(Flat.label))
                    .foregroundStyle(on ? theme.pane : theme.ink)
                    .lineLimit(1)
                    .fixedSize()
                    .padding(.horizontal, 14)
                    .frame(maxWidth: .infinity, minHeight: tapTarget)
                    .background { if on { RoundedRectangle(cornerRadius: Radius.row).fill(theme.ink) } }
                    .contentShape(Rectangle())
            }
            .buttonStyle(Pressable())
            .accessibilityAddTraits(on ? .isSelected : [])
        }
    }
}
