import AppKit
import QuickLook
import SwiftUI
import TerminatrKit
import TerminatrUI

/// The right panel's Files view (the file browser design, in the
/// Inspector's calmer frame, T1016): the header at the main pane's height
/// names what's shown; Worktree, the session's folder as a tree with git's
/// marks, and Attached, what threads attached to their reports and the
/// coordinator keeps, its own first; beside a director only Attached, with
/// its own files and then its members' by project, and the Library's kinds;
/// a filter;
/// the picked file previewing under the tree; its actions in the footer.
/// Read only. Arrows move, ← and → fold, return opens, space is Quick Look.
struct FilesPanel: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @Environment(SectionFocusOwner.self) private var sections
    let model: FilesModel
    let place: FilesPlace
    /// A session window's own session model.
    var own: SessionModel?
    @State private var looking: URL?
    @FocusState private var treeFocused: Bool
    @FocusState private var filterFocused: Bool

    var body: some View {
        VStack(spacing: 0) {
            PanelHead { header }
                .accessibilityElement(children: .combine)
                .accessibilityAddTraits(.isHeader)
            if !place.isProgram { scopes }
            filterField
            if model.scope == .worktree, place.session != nil { toggles }
            if place.isProgram { programKinds }
            if let why = model.problem { note(why) }
            if model.scope == .attached, let why = model.libraryProblem { note(why, colour: theme.attention) }
            if model.scope == .attached, let why = model.libraryNote { note(why, colour: theme.attention) }
            list
                .frame(maxHeight: .infinity)
            if let f = model.picked {
                PickedFilePreview(model: model, file: f, open: { openInPane(f) })
                    .frame(maxHeight: .infinity)
            }
            if let why = model.actionProblem { note(why, colour: theme.attention) }
            if let f = model.picked { footer(f) }
        }
        .modifier(PanelGround())
        .quickLookPreview($looking)
        .task(id: place) {
            model.place = place
            await model.watch(app)
        }
        .task(id: model.filter) {
            guard model.scope == .worktree else { return }
            // As the user types: the last word's listing, not each letter's.
            try? await Task.sleep(for: .milliseconds(200))
            if !Task.isCancelled, model.place != nil, model.loaded { await model.refresh(app) }
        }
        .task(id: model.picked?.id) {
            if let f = model.picked { await model.load(f, app) }
        }
        .onChange(of: model.wantsFocus, initial: true) {
            if model.wantsFocus {
                model.wantsFocus = false
                treeFocused = true
            }
        }
        .onChange(of: sections.ask?.id) {
            if sections.ask?.section == .files { treeFocused = true }
        }
    }

    // MARK: the header

    @ViewBuilder
    private var header: some View {
        BarIcon(symbol: "doc")
            .foregroundStyle(theme.muted)
        Text("Files")
            .font(theme.sized(13, .medium))
            .fixedSize()
        if !place.label.isEmpty {
            Text(place.label)
                .font(theme.monoSized(13))
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .truncationMode(.middle)
        }
        if model.scope == .worktree, let r = model.repo { repoLabel(r) }
        Spacer(minLength: 0)
        if model.scope == .worktree, let s = model.summary, s.git {
            HStack(spacing: 6) {
                Text(verbatim: "\(s.files) changed")
                    .font(theme.sized(12.5))
                    .foregroundStyle(theme.muted)
                Text(verbatim: "+\(s.added)").foregroundStyle(theme.good)
                Text(verbatim: "−\(s.deleted)").foregroundStyle(theme.attention)
            }
            .font(theme.monoSized(12.5))
            .monospacedDigit()
            .lineLimit(1)
            .fixedSize()
        } else if model.scope == .attached, let n = model.library?.count {
            Text(n == 1 ? "1 file" : "\(n) files")
                .font(theme.sized(12.5))
                .foregroundStyle(theme.muted)
                .fixedSize()
        }
    }

    /// Beside a coordinator: which repo this is, and that it's the main
    /// checkout rather than the coordinator's own work; a picker when the
    /// project has more than one. Each repo's counts are its own.
    @ViewBuilder
    private func repoLabel(_ r: AppAPI.FilesRepo) -> some View {
        if model.repos.count > 1 {
            Menu {
                ForEach(model.repos, id: \.path) { o in
                    Button {
                        model.choose(repo: o, app)
                        treeFocused = true
                    } label: {
                        if o.path == r.path { Label(o.name, systemImage: "checkmark") } else { Text(o.name) }
                    }
                }
            } label: {
                HStack(spacing: 3) {
                    repoName(r)
                    Image(systemName: "chevron.down")
                        .font(.system(size: 9 * theme.scale, weight: .semibold))
                        .foregroundStyle(theme.muted)
                }
                .contentShape(Rectangle())
            }
            .menuStyle(.button)
            .buttonStyle(.plain)
            .menuIndicator(.hidden)
            .help("\(r.name): chooses which of the project's repos shows")
            .accessibilityLabel("Repo, \(r.name), main checkout")
            .layoutPriority(1)
        } else {
            repoName(r)
                .layoutPriority(1)
        }
        Text("main checkout")
            .font(theme.sized(12.5))
            .foregroundStyle(theme.muted)
            .lineLimit(1)
            .fixedSize()
    }

    private func repoName(_ r: AppAPI.FilesRepo) -> some View {
        Text(r.name)
            .font(theme.sized(13))
            .lineLimit(1)
            .truncationMode(.middle)
            .help(r.name)
    }

    // MARK: the tabs, the filter and the toggles

    private var scopes: some View {
        HStack(spacing: 4) {
            ForEach(FilesModel.Scope.allCases) { s in
                FilesChip(title: s.rawValue, on: model.scope == s) {
                    model.pick(scope: s, app)
                    treeFocused = true
                }
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .overlay(alignment: .bottom) { theme.lineSoft.frame(height: 1) }
    }

    private var filterField: some View {
        @Bindable var model = model
        return HStack(spacing: 7) {
            Image(systemName: "magnifyingglass")
                .font(.system(size: 12))
                .foregroundStyle(theme.faint)
                .accessibilityHidden(true)
            TextField(place.isProgram ? LibraryPage.filterPrompt(model.library ?? []) : "Filter by name", text: $model.filter)
                .textFieldStyle(.plain)
                .font(theme.sized(12.5))
                .focused($filterFocused)
                .onExitCommand { model.filter = "" }
                .onKeyPress(.downArrow) {
                    treeFocused = true
                    return .handled
                }
            if !model.filter.isEmpty {
                Button { model.filter = "" } label: {
                    Image(systemName: "xmark.circle.fill")
                        .font(.system(size: 11))
                        .foregroundStyle(theme.faint)
                }
                .buttonStyle(.plain)
                .help("Clears the filter")
                .accessibilityLabel("Clear the filter")
            }
        }
        .padding(.horizontal, 9)
        .frame(height: 28)
        .overlay { RoundedRectangle(cornerRadius: 6).strokeBorder(filterFocused ? theme.focus : theme.line, lineWidth: 1).allowsHitTesting(false) }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
    }

    /// The Library's kind chips, across the program's files.
    @ViewBuilder
    private var programKinds: some View {
        @Bindable var model = model
        let all = model.library ?? []
        let chips = LibraryPage.chips(all, query: model.filter, kind: model.kind)
        if !chips.isEmpty {
            LibraryChips(chips, kind: Binding { LibraryPage.picked(all, model.kind) } set: { model.kind = $0 })
                .padding(.horizontal, 12)
                .padding(.bottom, 6)
        }
    }

    private var toggles: some View {
        HStack(spacing: 4) {
            FilesChip(title: "Changed only", on: model.changedOnly) {
                model.setChangedOnly(!model.changedOnly, app)
                treeFocused = true
            }
                .disabled(model.summary?.git != true)
                .help(model.summary?.git == true ? "Only the files git marks" : "No repo here: no marks")
            FilesChip(title: "Show ignored", on: model.showIgnored) {
                model.setShowIgnored(!model.showIgnored, app)
                treeFocused = true
            }
                .help("Git-ignored files such as node_modules and build. They may hold secrets.")
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 12)
        .padding(.bottom, 6)
    }

    private func note(_ text: String, colour: Color? = nil) -> some View {
        Text(text)
            .font(theme.sized(12.5))
            .foregroundStyle(colour ?? theme.muted)
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 15)
            .padding(.vertical, 6)
    }

    // MARK: the list

    private var list: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0) {
                    if model.scope == .worktree { worktree } else { attached }
                }
                .padding(.horizontal, 6)
                .padding(.bottom, 8)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .scrollEdges()
            .focusable()
            .focusEffectDisabled()
            .focused($treeFocused)
            .onKeyPress(keys: [.upArrow, .downArrow, .leftArrow, .rightArrow, .return, .space]) { press in
                key(press.key, proxy)
            }
            .accessibilityLabel("Files")
        }
    }

    private func key(_ key: KeyEquivalent, _ proxy: ScrollViewProxy) -> KeyPress.Result {
        switch key {
        case .upArrow, .downArrow:
            guard let next = PickStep.next(model.order, from: model.cursor, up: key == .upArrow, down: key == .downArrow) else { return .handled }
            model.moveCursor(to: next)
            proxy.scrollTo(next)
        case .leftArrow, .rightArrow:
            model.fold(right: key == .rightArrow, app)
            if let c = model.cursor { proxy.scrollTo(c) }
        case .return:
            guard let c = model.cursor else { return .ignored }
            if model.scope == .worktree, model.rows.first(where: { $0.id == c })?.isFolder == true, !model.flat {
                model.toggle(folder: c, app)
            } else if let f = model.picked {
                openInPane(f)
            }
        case .space:
            guard let f = model.picked else { return .ignored }
            look(f)
        default:
            return .ignored
        }
        return .handled
    }

    @ViewBuilder
    private var worktree: some View {
        let rows = model.rows
        let unfolded = model.unfolded
        if rows.isEmpty, model.problem == nil, model.loaded {
            Text(model.searching ? "Nothing matches." : "Nothing here.")
                .font(theme.sized(12.5))
                .foregroundStyle(theme.muted)
                .padding(8)
        }
        ForEach(rows) { r in
            FileNode(row: r, flat: model.flat, unfolded: unfolded.contains(r.id), on: model.cursor == r.id) {
                click(r)
            } twice: {
                if !r.isFolder { openInPane(model.file(r.entry)) }
            }
            .contextMenu { rowMenu(r) }
            .id(r.id)
        }
        if model.cut {
            Text("More files than show here: the filter finds them.")
                .font(theme.sized(12))
                .foregroundStyle(theme.muted)
                .padding(8)
        }
    }

    private func click(_ r: FileRow) {
        treeFocused = true
        model.moveCursor(to: r.id)
        if r.isFolder, !model.flat { model.toggle(folder: r.id, app) }
    }

    @ViewBuilder
    private func rowMenu(_ r: FileRow) -> some View {
        if r.isFolder {
            pathActions(r.entry)
            mentionButton(.worktree(r.entry, ignored: false))
        } else {
            let f = model.file(r.entry)
            openActions(f)
            Divider()
            pathActions(r.entry)
            mentionButton(f.source)
        }
    }

    @ViewBuilder
    private var attached: some View {
        let sections = LibraryPage.sections(model.attached)
        if model.library?.isEmpty == true {
            Text(place.isProgram ? "No files: the director keeps its own (tm library add), threads attach theirs."
                 : place.thread == nil ? "No files: a thread's report attaches them (tm report --attach), the coordinator keeps its own (tm library add)."
                 : "No files: a thread's report attaches them (tm report --attach).")
                .font(theme.sized(12.5))
                .foregroundStyle(theme.muted)
                .padding(8)
        } else if sections.isEmpty, model.library != nil {
            Text("Nothing matches.")
                .font(theme.sized(12.5))
                .foregroundStyle(theme.muted)
                .padding(8)
        }
        ForEach(sections) { section in
            if !section.project.isEmpty {
                Text(section.title)
                    .font(theme.sized(13, .medium))
                    .lineLimit(1)
                    .truncationMode(.tail)
                    .padding(.horizontal, 8)
                    .padding(.top, 12)
                    .accessibilityAddTraits(.isHeader)
            }
            ForEach(section.groups) { g in
                if place.isProgram {
                    // A director's list has the Library's heading.
                    LibraryHeading(g)
                        .padding(.horizontal, 8)
                        .padding(.top, 9)
                        .padding(.bottom, 3)
                } else {
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Text(g.title)
                            .lineLimit(1)
                            .truncationMode(.tail)
                        Spacer(minLength: 0)
                        Text(g.detail)
                            .lineLimit(1)
                            .fixedSize()
                    }
                    .font(theme.sized(12))
                    .foregroundStyle(theme.muted)
                    .padding(.horizontal, 8)
                    .padding(.top, 9)
                    .padding(.bottom, 3)
                    .accessibilityElement(children: .combine)
                    .accessibilityAddTraits(.isHeader)
                }
                ForEach(g.files, id: \.self) { a in
                    let f = OpenedFile(.attached(a))
                    AttachedNode(file: a, on: model.cursor == f.id) {
                        treeFocused = true
                        model.moveCursor(to: f.id)
                    } twice: {
                        openInPane(f)
                    }
                    .contextMenu {
                        openActions(f)
                        Divider()
                        mentionButton(f.source)
                    }
                    .id(f.id)
                }
            }
        }
    }

    // MARK: the footer and the actions

    private func footer(_ f: OpenedFile) -> some View {
        HStack(spacing: 8) {
            Button("Quick Look") { look(f) }
                .help("Quick Look (space)")
            Button("Open") { openElsewhere(f) }
                .disabled(FileCaution.launches(f.name))
                .help(FileCaution.launches(f.name) ? "Opening it would run it: Save As or Reveal in Finder instead" : "Opens it in its app")
            mentionButton(f.source, short: true)
            if model.busy { ProgressView().controlSize(.small) }
            Spacer(minLength: 0)
            Menu {
                Button("Save As…") { saveAs(f) }
                if case .worktree(let e, _) = f.source {
                    Divider()
                    pathActions(e)
                }
            } label: {
                Image(systemName: "ellipsis")
                    .font(.system(size: 13 * theme.scale))
                    .foregroundStyle(theme.muted)
                    .frame(width: 22, height: 22)
                    .contentShape(Rectangle())
            }
            .menuStyle(.button)
            .buttonStyle(.plain)
            .menuIndicator(.hidden)
            .fixedSize()
            .help("Save As, Copy Path, Reveal in Finder")
            .accessibilityLabel("More actions")
        }
        .buttonStyle(FlatTextButtonStyle(points: 12.5))
        .modifier(PanelFootBar())
    }

    @ViewBuilder
    private func openActions(_ f: OpenedFile) -> some View {
        Button("Open in Pane") { openInPane(f) }
        Button("Quick Look") { look(f) }
        Button("Open") { openElsewhere(f) }
            .disabled(FileCaution.launches(f.name))
        Button("Save As…") { saveAs(f) }
    }

    /// Worktree files alone have a path here: a Library file's stays with
    /// the server. Everything the Mac's panel lists from a worktree is on
    /// this Mac: a paired server's lists Attached alone.
    @ViewBuilder
    private func pathActions(_ e: AppAPI.FileEntry) -> some View {
        if let path = model.absolute(e.path) {
            Button("Copy Path") {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(path, forType: .string)
            }
            Button("Reveal in Finder") {
                NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: path)])
            }
        }
    }

    @ViewBuilder
    private func mentionButton(_ s: FileSource, short: Bool = false) -> some View {
        if let ref = place.mentions, place.thread == nil || place.session != nil {
            Button(short ? "Mention" : "Mention in the Message") {
                mention(FileMention.text(s, root: model.summary?.root ?? "", relative: place.relativeMention), ref: ref)
            }
            .help("Puts it in the composer, to ask the agent about it")
        }
    }

    private func mention(_ text: String, ref: SessionRef) {
        model.pane.shown = nil
        Task {
            do {
                try await app.mention(text, in: ref, own: own)
            } catch {
                model.actionProblem = Dashboard.words(error)
            }
        }
    }

    private func openInPane(_ f: OpenedFile) {
        model.pane.open(f)
    }

    private func look(_ f: OpenedFile) {
        Task { if let url = await copy(f) { looking = url } }
    }

    private func openElsewhere(_ f: OpenedFile) {
        guard !FileCaution.launches(f.name) else { return }
        Task { if let url = await copy(f) { NSWorkspace.shared.open(url) } }
    }

    /// The file's bytes in a temporary folder of the app's own, as the
    /// Library's: Quick Look and Open read that copy, never the agent's path.
    private func copy(_ f: OpenedFile) async -> URL? {
        guard let d = await model.bytes(f, app) else { return nil }
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("terminatr-files/\(UUID().uuidString)")
        let url = dir.appendingPathComponent((f.name as NSString).lastPathComponent)
        do {
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            try d.write(to: url, options: .atomic)
            return url
        } catch {
            model.actionProblem = error.localizedDescription
            return nil
        }
    }

    private func saveAs(_ f: OpenedFile) {
        Task {
            guard let d = await model.bytes(f, app) else { return }
            let panel = NSSavePanel()
            panel.nameFieldStringValue = (f.name as NSString).lastPathComponent
            guard panel.runModal() == .OK, let url = panel.url else { return }
            do {
                try d.write(to: url, options: .atomic)
            } catch {
                NSAlert(error: error).runModal()
            }
        }
    }
}

/// The panel's tab or toggle: words on the tab's fill while on.
private struct FilesChip: View {
    @Environment(\.theme) private var theme
    @Environment(\.isEnabled) private var enabled
    let title: String
    let on: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(title)
                .font(theme.sized(12.5))
                .foregroundStyle(on ? theme.ink : enabled ? theme.muted : theme.faint)
                .lineLimit(1)
                .padding(.horizontal, 10)
                .padding(.vertical, 3)
                .background(on ? theme.tab : .clear, in: RoundedRectangle(cornerRadius: 5))
                .contentShape(RoundedRectangle(cornerRadius: 5))
        }
        .buttonStyle(.plain)
        .fixedSize()
        .accessibilityAddTraits(on ? .isSelected : [])
    }
}

/// A row of the tree: 26 high, 5 round; its fold mark, icon, name and git's
/// mark at its end.
private struct FileNode: View {
    @Environment(\.theme) private var theme
    @State private var hovering = false
    let row: FileRow
    /// A filter's flat list: the whole path, no folding.
    let flat: Bool
    let unfolded: Bool
    let on: Bool
    let click: () -> Void
    let twice: () -> Void

    var body: some View {
        let e = row.entry
        let shape = RoundedRectangle(cornerRadius: 5)
        Button(action: click) {
            HStack(spacing: 6) {
                if row.isFolder, !flat {
                    Image(systemName: unfolded ? "chevron.down" : "chevron.right")
                        .font(.system(size: 9, weight: .semibold))
                        .foregroundStyle(theme.muted)
                        .frame(width: 13, height: 13)
                }
                Image(systemName: row.isFolder ? "folder" : "doc")
                    .font(.system(size: 12))
                    .foregroundStyle(theme.muted)
                    .frame(width: 13, height: 13)
                Text(name)
                    .font(theme.sized(13))
                    .foregroundStyle(dim ? theme.muted : theme.ink)
                    .lineLimit(1)
                    .truncationMode(flat ? .head : .middle)
                Spacer(minLength: 0)
                if let m = mark {
                    Text(m.word)
                        .font(theme.monoSized(11.5))
                        .foregroundStyle(m.colour)
                        .fixedSize()
                }
            }
            .padding(.leading, CGFloat(8 + row.depth * 14 + (row.isFolder || flat ? 0 : 19)))
            .padding(.trailing, 8)
            .frame(maxWidth: .infinity, minHeight: 26, alignment: .leading)
            .background(on ? theme.selection : hovering ? theme.selection.opacity(0.55) : .clear, in: shape)
            .contentShape(shape)
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .simultaneousGesture(TapGesture(count: 2).onEnded { twice() })
        .accessibilityLabel(spoken)
        .accessibilityAddTraits(on ? .isSelected : [])
        .help(e.linkOut ? "A link that doesn't open: it leads out of the worktree, or nowhere" : e.path)
    }

    private var name: String {
        let e = row.entry
        let base = flat ? e.path : e.name
        return e.linkOut ? base + " ↗" : e.link ? base + " →" : base
    }

    private var dim: Bool {
        let e = row.entry
        return e.ignored || e.status == "D" || e.linkOut
    }

    private var mark: (word: String, colour: Color)? {
        let e = row.entry
        switch e.status {
        case "M": return ("M", theme.approval)
        case "A": return ("A", theme.good)
        case "D": return ("D", theme.attention)
        default: break
        }
        if e.changed { return ("•", theme.approval) }
        return e.ignored ? ("ignored", theme.muted) : nil
    }

    private var spoken: String {
        let e = row.entry
        var parts = [row.isFolder ? "\(e.path), folder" : e.path]
        if row.isFolder, !flat { parts.append(unfolded ? "unfolded" : "folded") }
        switch e.status {
        case "M": parts.append("modified")
        case "A": parts.append("added")
        case "D": parts.append("deleted")
        default: if e.changed { parts.append("holds changes") }
        }
        if e.ignored { parts.append("git-ignored") }
        return parts.joined(separator: ", ")
    }
}

/// An attached file's row: its name, and its size at the end.
private struct AttachedNode: View {
    @Environment(\.theme) private var theme
    @State private var hovering = false
    let file: AppAPI.LibraryFile
    let on: Bool
    let click: () -> Void
    let twice: () -> Void

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: 5)
        Button(action: click) {
            HStack(spacing: 6) {
                Image(systemName: "doc")
                    .font(.system(size: 12))
                    .foregroundStyle(theme.muted)
                    .frame(width: 13, height: 13)
                Text(file.name)
                    .font(theme.sized(13))
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer(minLength: 0)
                Text(Attachment.size(file.size))
                    .font(theme.monoSized(11.5))
                    .foregroundStyle(theme.muted)
                    .fixedSize()
            }
            .padding(.horizontal, 8)
            .frame(maxWidth: .infinity, minHeight: 26, alignment: .leading)
            .background(on ? theme.selection : hovering ? theme.selection.opacity(0.55) : .clear, in: shape)
            .contentShape(shape)
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .simultaneousGesture(TapGesture(count: 2).onEnded { twice() })
        .accessibilityLabel("\(file.name), \(Attachment.size(file.size))")
        .accessibilityAddTraits(on ? .isSelected : [])
    }
}

/// The picked file under the tree: its name, Open in pane and its text.
private struct PickedFilePreview: View {
    @Environment(\.theme) private var theme
    let model: FilesModel
    let file: OpenedFile
    let open: () -> Void

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 8) {
                Text(file.path)
                    .font(theme.monoSized(12.5))
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer(minLength: 0)
                Button("Open in pane", action: open)
                    .buttonStyle(.plain)
                    .font(theme.sized(12))
                    .foregroundStyle(theme.muted)
                    .padding(.horizontal, 7)
                    .padding(.vertical, 2)
                    .background(theme.tab, in: RoundedRectangle(cornerRadius: 5))
                    .fixedSize()
                    .help("Adds it as a tab in the main pane (return)")
            }
            .padding(.leading, 13)
            .padding(.trailing, 8)
            .frame(height: 32)
            .padding(.bottom, 1)
            .overlay(alignment: .bottom) { theme.lineSoft.frame(height: 1) }
            FileBody(model: model, file: file, points: 12)
        }
        .overlay(alignment: .top) { theme.line.frame(height: 1) }
    }
}

/// A file's text with what's to say of it: a warning, a cut, that it was
/// deleted. What has no text says so, with what can show it.
struct FileBody: View {
    @Environment(\.theme) private var theme
    let model: FilesModel
    let file: OpenedFile
    let points: CGFloat

    var body: some View {
        let s = model.shown[file.id]
        VStack(spacing: 0) {
            switch s?.body {
            case .text(let text, let size, let shown, _):
                if let c = file.caution { line(c, icon: "exclamationmark.triangle", colour: theme.attention) }
                if s?.deleted == true { line("Deleted from the worktree: this is what it held.", icon: "trash", colour: theme.attention) }
                if let cut = FileText.cutNote(size: size, shown: shown) { line(cut, icon: "scissors", colour: theme.muted) }
                change(s?.diff)
                if s?.diff?.tooBig == true { line("Too big to mark its changes.", icon: "scissors", colour: theme.muted) }
                if s?.diff?.truncated == true { line("More changes than are marked here.", icon: "scissors", colour: theme.muted) }
                if let marks = s?.marks {
                    CodeView(id: file.id, text: marks.text, marks: marks, points: points)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else if text.isEmpty {
                    empty("It's empty.")
                } else {
                    CodeView(id: file.id, text: text, marks: nil, points: points)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            case .binary(let size, _):
                if case .drawn(let picture) = s?.picture {
                    if let c = file.caution { line(c, icon: "exclamationmark.triangle", colour: theme.attention) }
                    if s?.deleted == true { line("Deleted from the worktree: this is what it held.", icon: "trash", colour: theme.attention) }
                    change(s?.diff)
                    FileImageView(name: file.name, picture: picture, size: size)
                } else {
                    change(s?.diff)
                    if case .notDrawn(let why) = s?.picture {
                        empty(FileImage.message(name: file.name, size: size, why: why, hasPath: hasPath))
                    } else {
                        empty(FileImage.message(name: file.name, size: size))
                    }
                }
            case .gone:
                empty("\(file.name) is no longer there.")
            case .unavailable(let why):
                empty(why)
            case nil:
                ProgressView()
                    .controlSize(.small)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
    }

    /// Whether Copy Path and Reveal in Finder are in its menu (pathActions).
    private var hasPath: Bool {
        if case .worktree(let e, _) = file.source { return model.absolute(e.path) != nil }
        return false
    }

    /// What changed in it since the base: nothing shows of a file with no
    /// change.
    @ViewBuilder private func change(_ diff: AppAPI.FilesDiff?) -> some View {
        if let diff, let w = FileChange.words(diff) {
            HStack(spacing: 6) {
                if let lead = w.lead {
                    Text(lead)
                        .foregroundStyle(theme.ink)
                        .truncationMode(.middle)
                }
                if let n = w.added {
                    Text("+\(n)").foregroundStyle(theme.good)
                }
                if let n = w.deleted {
                    Text("−\(n)").foregroundStyle(theme.attention)
                }
                if let since = w.since {
                    Text(since)
                        .foregroundStyle(theme.muted)
                        .truncationMode(.middle)
                }
                Spacer(minLength: 0)
            }
            .font(theme.sized(12))
            .lineLimit(1)
            .padding(.horizontal, 13)
            .padding(.vertical, 4)
            .overlay(alignment: .bottom) { theme.lineSoft.frame(height: 1) }
            .accessibilityElement(children: .combine)
        }
    }

    private func line(_ text: String, icon: String, colour: Color) -> some View {
        Label(text, systemImage: icon)
            .font(theme.sized(12))
            .foregroundStyle(colour)
            .lineLimit(2)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 13)
            .padding(.vertical, 4)
            .overlay(alignment: .bottom) { theme.lineSoft.frame(height: 1) }
            .accessibilityElement(children: .combine)
    }

    private func empty(_ text: String) -> some View {
        Text(text)
            .font(theme.sized(13))
            .foregroundStyle(theme.muted)
            .fixedSize(horizontal: false, vertical: true)
            .padding(.horizontal, 15)
            .padding(.vertical, 14)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }
}

/// An image file, fitted to what room there is and never past its actual
/// size; its size under it. Nothing to zoom: Space's Quick Look does that.
private struct FileImageView: View {
    @Environment(\.theme) private var theme
    let name: String
    let picture: FileImage.Drawn
    let size: Int

    var body: some View {
        VStack(spacing: 0) {
            Image(decorative: picture.image, scale: 1)
                .resizable()
                .interpolation(.high)
                .aspectRatio(contentMode: .fit)
                .background { if !picture.opaque { Checks() } }
                .frame(maxWidth: picture.pointWidth, maxHeight: picture.pointHeight)
                .accessibilityLabel(name)
                .accessibilityValue("\(picture.width) by \(picture.height) pixels")
                .accessibilityAddTraits(.isImage)
                .padding(12)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            Text(words)
                .font(theme.sized(12))
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .truncationMode(.middle)
                .padding(.horizontal, 13)
                .padding(.bottom, 8)
        }
    }

    private var words: String {
        var out = "\(picture.width) × \(picture.height) · \(Attachment.size(size))"
        if picture.frames > 1 { out += " · one of \(picture.frames) frames" }
        return out
    }

    /// What's behind an image with clear parts: mid greys, so that neither
    /// black nor white ink is lost on either theme.
    private struct Checks: View {
        var body: some View {
            Canvas { c, size in
                let side: CGFloat = 8
                c.fill(Path(CGRect(origin: .zero, size: size)), with: .color(white: 0.62))
                var dark = Path()
                for row in 0...Int(size.height / side) {
                    for col in 0...Int(size.width / side) where (row + col) % 2 == 0 {
                        dark.addRect(CGRect(x: CGFloat(col) * side, y: CGFloat(row) * side, width: side, height: side))
                    }
                }
                c.fill(dark, with: .color(white: 0.5))
            }
            .accessibilityHidden(true)
        }
    }
}

/// A file tab's place in the main pane: read at full width, refreshed on its
/// own while it shows.
struct FilePane: View {
    @Environment(AppModel.self) private var app
    let model: FilesModel
    let file: OpenedFile

    var body: some View {
        FileBody(model: model, file: file, points: 13)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .task(id: file.id) {
                await model.load(file, app)
                while !Task.isCancelled {
                    try? await Task.sleep(for: .seconds(2))
                    if Task.isCancelled { return }
                    await model.reread(file, app)
                }
            }
    }
}
