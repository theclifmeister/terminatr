import SwiftUI
import TerminatrKit

// The inspector's parts: the Mac's right panel and the phone's Inspector
// tab (T1001).

/// A state in words beside its dot.
public struct StateLine: View {
    @Environment(\.theme) private var theme
    let word: String
    let colour: Color
    /// The role whose state it is (T1003), named first.
    let role: SessionRole?

    public init(word: String, colour: Color, role: SessionRole? = nil) {
        self.word = word
        self.colour = colour
        self.role = role
    }

    public var body: some View {
        HStack(spacing: 6) {
            if let role {
                RoleLabel(role, points: 12.5)
                    .padding(.trailing, 2)
            }
            Circle()
                .fill(colour)
                .frame(width: 7, height: 7)
                .accessibilityHidden(true)
            Text(word.prefix(1).uppercased() + word.dropFirst())
                .font(theme.sized(12.5))
                .foregroundStyle(theme.muted)
        }
        .accessibilityElement(children: .combine)
    }
}

/// A group of the panel under its label.
public struct InspectorSection<Content: View>: View {
    @Environment(\.theme) private var theme
    let title: String
    let content: Content

    public init(_ title: String, @ViewBuilder content: () -> Content) {
        self.title = title
        self.content = content()
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title)
                .font(theme.sized(12))
                .foregroundStyle(theme.muted)
                .accessibilityAddTraits(.isHeader)
            VStack(alignment: .leading, spacing: 5) {
                content
            }
        }
    }
}

/// Facts in their two columns: the label's 62 and the value.
public struct FactList<Content: View>: View {
    let content: Content

    public init(@ViewBuilder content: () -> Content) {
        self.content = content()
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            content
        }
    }
}

public struct FactRow: View {
    @Environment(\.theme) private var theme
    let label: String
    let value: String
    let mono: Bool

    public init(_ label: String, _ value: String, mono: Bool = false) {
        self.label = label
        self.value = value
        self.mono = mono
    }

    public var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Text(label)
                .foregroundStyle(theme.muted)
                .frame(minWidth: 62 * theme.scale, alignment: .leading)
            Text(value)
                .font(mono ? theme.monoSized(12) : theme.sized(13))
                .lineLimit(1)
                .truncationMode(.middle)
                .textSelection(.enabled)
                .help(value)
        }
        .font(theme.sized(13))
        .accessibilityElement(children: .combine)
    }
}

/// A step or a todo: done, current or to do.
public struct CheckRow: View {
    @Environment(\.theme) private var theme
    let text: String
    let done: Bool
    let current: Bool

    public init(text: String, done: Bool, current: Bool) {
        self.text = text
        self.done = done
        self.current = current
    }

    public var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 7) {
            Image(systemName: done ? "checkmark.circle" : current ? "circle.inset.filled" : "circle")
                .font(.system(size: 12 * theme.scale))
                .foregroundStyle(done ? theme.good : current ? theme.ink : theme.faint)
                .frame(width: 15 * theme.scale)
            Text(text)
                .font(theme.sized(13))
                .foregroundStyle(done ? theme.muted : theme.ink)
                .fixedSize(horizontal: false, vertical: true)
        }
        .accessibilityElement(children: .combine)
        .accessibilityValue(done ? "done" : current ? "current" : "")
    }
}
