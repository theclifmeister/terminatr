import SwiftUI
import TerminatrUI

/// Help › Keyboard Shortcuts (⌘/, T748 milestone E): every key the
/// window has, by where it works.
struct ShortcutsSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(\.theme) private var theme

    static let groups: [(String, [(String, String)])] = [
        ("Window", [
            ("⌘N", "New Task: add a task to the shown project's board"),
            ("⇧⌘O", "Open the shown session in a new window"),
            ("⌃⌘S", "Show or hide the sidebar"),
            ("⌥⌘E", "Show or hide the files"),
            ("⌥⌘I", "Show or hide the inspector"),
            ("⌘+  ⌘-  ⌘0", "Make text bigger, smaller, or its actual size"),
        ]),
        ("Go", [
            ("⌘K", "Command palette: go to a project, thread, task or need"),
            ("⇧⌘F", "Search the project's history: notes, tasks, threads, journal"),
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
            ("⌥⌘1 – ⌥⌘9", "The open question card's option by its number"),
            ("⌘L", "The composer"),
            ("↩  ⌥↩  ⇧↩", "Send (queued), steer into the running turn, new line"),
            ("⌘F", "Find in the conversation; ⌘G and ⇧⌘G the next and previous match"),
            ("⇧⌘T", "Conversation or Terminal"),
            ("⌥⌘R", "Repaint the terminal from its server"),
        ]),
        ("Files", [
            ("↑  ↓", "Move through the tree; the file previews under it"),
            ("←  →", "Fold or unfold a folder; ← from a file goes up to its folder"),
            ("↩", "Open the file in a tab of the main pane, or fold a folder"),
            ("Space", "Quick Look"),
        ]),
        ("Palette", [
            ("↑  ↓", "Move through the rows"),
            ("↩", "Show the row"),
            ("⌘↩", "Act on it: allow, answer, message or delegate, as the cards ask"),
            ("Esc", "Back, then close"),
        ]),
    ]

    var body: some View {
        FlatSheet(width: 560) {
            SheetTitle("Keyboard Shortcuts")
        } content: {
            ScrollView {
                SheetBody {
                    ForEach(Self.groups, id: \.0) { group, keys in
                        VStack(alignment: .leading, spacing: 6) {
                            PaneLabel(group)
                            Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 5) {
                                ForEach(keys, id: \.0) { key, what in
                                    GridRow {
                                        Text(key)
                                            .font(theme.monoSized(Flat.label, .medium))
                                            .foregroundStyle(theme.action)
                                            .frame(minWidth: 96, alignment: .leading)
                                        Text(what)
                                            .font(theme.sized(Flat.label))
                                    }
                                }
                            }
                        }
                    }
                }
            }
            .frame(maxHeight: 620)
            .fixedSize(horizontal: false, vertical: true)
        } footer: {
            SheetEscape { dismiss() }
            Button("Done") { dismiss() }
                .buttonStyle(.flatPrimary)
                .keyboardShortcut(.defaultAction)
        }
    }
}
