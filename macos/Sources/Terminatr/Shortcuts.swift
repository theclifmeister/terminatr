import SwiftUI

/// Help › Keyboard Shortcuts (⌘/, T748 milestone E): every key the
/// window has, by where it works.
struct ShortcutsSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(\.theme) private var theme

    static let groups: [(String, [(String, String)])] = [
        ("Window", [
            ("⌘N", "New Task: add a task to the shown project's board"),
            ("⇧⌘O", "Open the shown session in a new window"),
            ("⌘+  ⌘-  ⌘0", "Make text bigger, smaller, or its actual size"),
        ]),
        ("Go", [
            ("⌘K", "Command palette: go to a project, thread, task or need"),
            ("⌥⌘↓  ⌥⌘↑", "Next and previous thing that needs you"),
            ("⌘1 – ⌘9", "The active projects"),
            ("⌘[  ⌘]", "Back and forward"),
            ("⇧⌘H", "Show or hide the inactive projects"),
        ]),
        ("Sidebar", [
            ("↑  ↓", "Move through the rows"),
            ("a – z", "Select the row that begins with the letters typed"),
            ("←  →", "Fold or unfold a project; ← from a session goes up to its project"),
            ("⌥⌘F", "Filter the sidebar; Esc clears it"),
        ]),
        ("Session", [
            ("⌘↩", "The open card's first choice, with its confirmation"),
            ("⌘L", "The composer"),
            ("↩  ⌥↩  ⇧↩", "Send (queued), steer into the running turn, new line"),
            ("⌘F", "Find in the conversation; ⌘G and ⇧⌘G the next and previous match"),
            ("⇧⌘T", "Conversation or Terminal"),
            ("⌥⌘I", "Show or hide the inspector"),
        ]),
        ("Palette", [
            ("↑  ↓", "Move through the rows"),
            ("↩", "Show the row"),
            ("⌘↩", "Act on it: allow, answer, message or delegate, as the cards ask"),
            ("Esc", "Back, then close"),
        ]),
    ]

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Keyboard Shortcuts")
                .font(theme.title(20, .heavy))
            ForEach(Self.groups, id: \.0) { group, keys in
                VStack(alignment: .leading, spacing: 6) {
                    Text(group.uppercased())
                        .font(theme.mono(10.5, .medium))
                        .tracking(1)
                        .foregroundStyle(theme.muted)
                    Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 5) {
                        ForEach(keys, id: \.0) { key, what in
                            GridRow {
                                Text(key)
                                    .font(theme.mono(12.5, .medium))
                                    .foregroundStyle(theme.action)
                                    .frame(minWidth: 96, alignment: .leading)
                                Text(what)
                                    .font(theme.body(12.5))
                            }
                        }
                    }
                }
            }
            HStack {
                Spacer()
                Button("Done") { dismiss() }
                    .keyboardShortcut(.defaultAction)
                    .tint(theme.action)
            }
        }
        .padding(24)
        .frame(width: 560)
    }
}
