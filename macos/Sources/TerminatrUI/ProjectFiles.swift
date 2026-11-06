import SwiftUI
import TerminatrKit

/// A project's overview (T794), as the TUI's A › overview: its goal,
/// repositories, coordinator, threads by agent and tasks. Read only.
public struct OverviewView: View {
    @Environment(\.theme) private var theme
    let overview: ProjectOverview
    /// Whether it says the goal: the Mac's Overview has it at its head.
    let showsGoal: Bool

    public init(_ overview: ProjectOverview, showsGoal: Bool = true) {
        self.overview = overview
        self.showsGoal = showsGoal
    }

    public var body: some View {
        Grid(alignment: .leadingFirstTextBaseline, horizontalSpacing: 16, verticalSpacing: 10) {
            if showsGoal {
                row("Goal") {
                    if overview.goal.isEmpty {
                        Text("None set: ask the coordinator to set one").foregroundStyle(theme.muted)
                    } else {
                        Text(overview.goal).fixedSize(horizontal: false, vertical: true)
                    }
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

/// The Library page's kinds (T1054): a chip with its count for all and
/// for each kind that has files. When they don't fit they wrap on the Mac,
/// where a mouse couldn't scroll them, and scroll sideways on the iPhone.
public struct LibraryChips: View {
    @Environment(\.theme) private var theme
    let chips: [LibraryPage.Chip]
    @Binding var kind: LibraryPage.Kind?

    public init(_ chips: [LibraryPage.Chip], kind: Binding<LibraryPage.Kind?>) {
        self.chips = chips
        _kind = kind
    }

    public var body: some View {
        #if os(macOS)
        FlowRow(spacing: 5) { row }
        #else
        ScrollView(.horizontal) {
            HStack(spacing: 5) { row }
        }
        .scrollIndicators(.hidden)
        #endif
    }

    private var row: some View {
        ForEach(chips) { c in
            let on = c.kind == kind
            Button {
                kind = c.kind
            } label: {
                HStack(spacing: 4) {
                    Text(c.word)
                    Text(String(c.count)).opacity(on ? 0.72 : 1).foregroundStyle(on ? theme.pane : theme.muted)
                }
                .font(theme.sized(Flat.label))
                .foregroundStyle(on ? theme.pane : theme.ink)
                .padding(.horizontal, 9)
                .padding(.vertical, 3)
                .background(on ? theme.ink : .clear, in: RoundedRectangle(cornerRadius: 5))
                .overlay {
                    RoundedRectangle(cornerRadius: 5)
                        .strokeBorder(on ? theme.ink : theme.line, lineWidth: theme.contrast ? 1.5 : 1)
                }
                .contentShape(RoundedRectangle(cornerRadius: 5))
            }
            .buttonStyle(.plain)
            .accessibilityLabel("\(c.word), \(c.count)")
            .accessibilityAddTraits(on ? .isSelected : [])
        }
    }
}

/// A Library group's heading (T1055): its title and task's status, then
/// where its files came from, their day, kinds and size.
public struct LibraryHeading: View {
    @Environment(\.theme) private var theme
    let group: LibraryPage.Group

    public init(_ group: LibraryPage.Group) {
        self.group = group
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 1) {
            HStack(alignment: .firstTextBaseline, spacing: 7) {
                Text(group.title)
                    .font(theme.sized(Flat.text, .medium))
                    .foregroundStyle(theme.ink)
                    .lineLimit(1)
                if !group.status.isEmpty {
                    let tone = tone(group.status)
                    Text(group.status)
                        .font(theme.mono(.caption2))
                        .foregroundStyle(tone)
                        .lineLimit(1)
                        .fixedSize()
                        .padding(.horizontal, 4)
                        .overlay {
                            RoundedRectangle(cornerRadius: 3).strokeBorder(tone, lineWidth: theme.contrast ? 1.5 : 1)
                        }
                }
            }
            Text(group.facts())
                .font(theme.sized(Flat.label))
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .truncationMode(.middle)
        }
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }

    private func tone(_ status: String) -> Color {
        switch status {
        case "done": theme.good
        case "review": theme.approval
        case "started": theme.action
        case "blocked": theme.attention
        default: theme.muted
        }
    }
}

/// A thumbnail in a Library group's strip (T1059): the picture, or its
/// ground until it comes, with the file's name under it. A landscape picture
/// fills the frame from its top, where a screenshot's title is; a tall one
/// fits inside it, whole.
public struct LibraryTile: View {
    @Environment(\.theme) private var theme
    let file: AppAPI.LibraryFile
    let image: FileImage.Drawn?

    public init(_ file: AppAPI.LibraryFile, image: FileImage.Drawn?) {
        self.file = file
        self.image = image
    }

    public var body: some View {
        let shape = RoundedRectangle(cornerRadius: 5)
        VStack(alignment: .leading, spacing: 3) {
            Color.clear
                .aspectRatio(300.0 / 190.0, contentMode: .fit)
                .background(theme.selection)
                .overlay(alignment: .top) {
                    if let image {
                        Image(decorative: image.image, scale: 1)
                            .resizable()
                            .interpolation(.high)
                            .aspectRatio(contentMode: image.height > image.width ? .fit : .fill)
                    }
                }
                .clipShape(shape)
                .overlay { shape.strokeBorder(theme.line, lineWidth: theme.contrast ? 1.5 : 1) }
            Text(file.name)
                .font(theme.sized(Flat.label))
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .truncationMode(.middle)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(file.name), \(Attachment.size(file.size))")
        .accessibilityAddTraits(.isImage)
    }
}

/// The +N at the end of a Library group's strip, beside the tiles.
public struct LibraryStripFold: View {
    @Environment(\.theme) private var theme
    let more: Int
    /// Its width beside the tiles.
    public static let width = 62.0

    public init(more: Int) {
        self.more = more
    }

    public var body: some View {
        Text("+\(more)")
            .font(theme.sized(Flat.text, .semibold))
            .foregroundStyle(theme.action)
            .frame(maxWidth: .infinity)
            .padding(.vertical, 10)
            .accessibilityLabel("\(more) more")
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
