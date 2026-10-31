import SwiftUI
import TerminatrKit

/// A project's overview (T794), as the TUI's A › overview: its goal,
/// repositories, coordinator, threads by agent and tasks. Read only.
public struct OverviewView: View {
    @Environment(\.theme) private var theme
    let overview: ProjectOverview

    public init(_ overview: ProjectOverview) {
        self.overview = overview
    }

    public var body: some View {
        Grid(alignment: .leadingFirstTextBaseline, horizontalSpacing: 16, verticalSpacing: 10) {
            row("Goal") {
                if overview.goal.isEmpty {
                    Text("None set: ask the coordinator to set one").foregroundStyle(theme.muted)
                } else {
                    Text(overview.goal).fixedSize(horizontal: false, vertical: true)
                }
            }
            row("Repositories") {
                if let n = overview.repoCount {
                    Text(n == 0 ? "None" : n == 1 ? "1, its path stays there" : "\(n), their paths stay there")
                        .foregroundStyle(n == 0 ? theme.muted : theme.ink)
                } else if overview.repos.isEmpty {
                    Text("None").foregroundStyle(theme.muted)
                } else {
                    VStack(alignment: .leading, spacing: 4) {
                        ForEach(overview.repos, id: \.path) { r in
                            Text(r.path).font(theme.mono(.subheadline)).textSelection(.enabled)
                            if !r.note.isEmpty {
                                Text(r.note).font(theme.body(.footnote)).foregroundStyle(theme.attention)
                            }
                        }
                    }
                }
            }
            row("Coordinator") {
                Text(overview.coordinator.isEmpty ? "Not running" : overview.coordinator)
                    .foregroundStyle(overview.coordinator.isEmpty ? theme.muted : theme.ink)
            }
            row("Threads") {
                if overview.agents.isEmpty {
                    Text("None open").foregroundStyle(theme.muted)
                } else {
                    VStack(alignment: .leading, spacing: 4) {
                        ForEach(overview.agents, id: \.agent) { a in
                            (Text(a.agent) + Text(" · " + a.threads.joined(separator: " ")).foregroundColor(theme.muted))
                                .fixedSize(horizontal: false, vertical: true)
                        }
                    }
                }
            }
            row("Tasks") { Text(overview.tasks) }
            if !overview.instructions.isEmpty {
                row("PROJECT.md") {
                    MarkdownView(text: overview.instructions)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
        }
    }

    private func row(_ label: String, @ViewBuilder _ value: () -> some View) -> some View {
        GridRow {
            Text(label)
                .font(theme.body(.subheadline, .medium))
                .foregroundStyle(theme.muted)
                .gridColumnAlignment(.trailing)
            value()
        }
    }
}

/// A memory file in the Memory page's list: its title and, for a note,
/// its path.
public struct MemoryFileRow: View {
    @Environment(\.theme) private var theme
    let file: AppAPI.MemoryFile

    public init(_ file: AppAPI.MemoryFile) {
        self.file = file
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(file.title).lines(1)
            Text(file.path)
                .font(theme.mono(.caption))
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .truncationMode(.middle)
        }
        .accessibilityElement(children: .combine)
    }
}

/// A library file in the Library page: its name, size and date.
public struct LibraryFileRow: View {
    @Environment(\.theme) private var theme
    let file: AppAPI.LibraryFile

    public init(_ file: AppAPI.LibraryFile) {
        self.file = file
    }

    public var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(file.name)
                .lineLimit(1)
                .truncationMode(.middle)
            Spacer(minLength: 8)
            Text(Attachment.size(file.size))
                .font(theme.mono(.caption))
                .foregroundStyle(theme.muted)
            Text(file.time, format: .dateTime.month(.abbreviated).day())
                .font(theme.body(.caption))
                .foregroundStyle(theme.muted)
        }
        .accessibilityElement(children: .combine)
    }
}
