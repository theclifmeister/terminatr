import SwiftUI
import TerminatrKit

/// A task's priority as a small tag beside its id (T927): must in the
/// attention colour, should in the action colour, could muted; nothing
/// for none.
public struct PriorityTag: View {
    @Environment(\.theme) private var theme
    let priority: String

    public init(_ priority: String) { self.priority = priority }

    public var body: some View {
        if !priority.isEmpty {
            Text(priority)
                .font(theme.mono(.caption2, .medium))
                .foregroundStyle(colour)
                .padding(.horizontal, 6)
                .padding(.vertical, 1)
                .overlay(Capsule().strokeBorder(colour.opacity(theme.contrast ? 1 : 0.5), lineWidth: 1))
                .accessibilityLabel("priority \(priority)")
        }
    }

    private var colour: Color {
        switch priority {
        case "must": theme.attention
        case "should": theme.action
        default: theme.muted
        }
    }
}

/// The Priority menu of a task's view (task.priority, the TUI's P):
/// must, should, could or none, the task's own checked. set runs the
/// pick; busy greys it while a pick is on its way.
public struct PriorityMenu: View {
    let task: String
    let current: String
    let busy: Bool
    let set: (String) -> Void

    public init(task: String, current: String, busy: Bool = false, set: @escaping (String) -> Void) {
        self.task = task
        self.current = current
        self.busy = busy
        self.set = set
    }

    public var body: some View {
        Menu {
            ForEach(Priority.choices, id: \.self) { p in
                Button {
                    set(p)
                } label: {
                    if p == current {
                        Label(Priority.word(p), systemImage: "checkmark")
                    } else {
                        Text(Priority.word(p))
                    }
                }
            }
        } label: {
            Label(current.isEmpty ? "Priority" : "Priority: \(current)", systemImage: "flag")
        }
        .disabled(busy)
        .help("Sets \(task)'s priority: must, should, could or none; the board lists each group by it")
        .accessibilityLabel("Priority of \(task): \(Priority.word(current))")
    }
}
