import AppKit
import SwiftUI
import TerminatrKit

extension Appearance {
    /// Sets the app's appearance from the stored choice: every window,
    /// the menu bar's dropdown and Settings follow it.
    @MainActor static func apply() {
        switch of(UserDefaults.standard.string(forKey: key)) {
        case .system: NSApp.appearance = nil
        case .light: NSApp.appearance = NSAppearance(named: .aqua)
        case .dark: NSApp.appearance = NSAppearance(named: .darkAqua)
        }
    }
}

extension TextSize {
    /// View › Make Text Bigger (1), Smaller (-1) or Actual Size (0).
    static func change(by: Int) {
        let d = UserDefaults.standard
        let now = of(d.object(forKey: key) as? Double)
        d.set(by == 0 ? standard : step(now, by: by), forKey: key)
    }
}

private struct DensityKey: EnvironmentKey {
    static let defaultValue = Density.regular
}

extension EnvironmentValues {
    /// The sidebar's density (Settings › Appearance).
    var density: Density {
        get { self[DensityKey.self] }
        set { self[DensityKey.self] = newValue }
    }
}

/// A window's content with the user's text size and density: Bloom
/// scaled, and the sidebar's rows as close as asked.
struct Themed: ViewModifier {
    @AppStorage(TextSize.key) private var scale = TextSize.standard
    @AppStorage(Density.key) private var density = Density.regular.rawValue

    func body(content: Content) -> some View {
        var theme = Theme.bloom
        theme.scale = TextSize.of(scale)
        let d = Density.of(density)
        return content
            .environment(\.theme, theme)
            .environment(\.density, d)
    }
}

extension View {
    func themed() -> some View {
        modifier(Themed())
    }
}

/// Keeps a window's frame across launches under name: the main window
/// opens at the size and place it was left (T748 milestone G). SwiftUI
/// writes its own autosave but sizes the window afresh at launch, so
/// the frame is put back once it did, then saved on every change.
struct WindowFrame: NSViewRepresentable {
    let name: String

    func makeNSView(context: Context) -> NSView {
        Tracker(key: "windowFrame." + name)
    }

    func updateNSView(_ nsView: NSView, context: Context) {}

    private final class Tracker: NSView {
        let key: String
        private var watching: [NSObjectProtocol] = []

        init(key: String) {
            self.key = key
            super.init(frame: .zero)
        }

        required init?(coder: NSCoder) { nil }

        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            guard let window, watching.isEmpty else { return }
            let saved = UserDefaults.standard.string(forKey: key)
            DispatchQueue.main.async { [weak self, weak window] in
                guard let self, let window else { return }
                if let saved { window.setFrame(from: saved) }
                let save: @Sendable (Notification) -> Void = { [key] n in
                    guard let w = n.object as? NSWindow else { return }
                    MainActor.assumeIsolated {
                        UserDefaults.standard.set(w.frameDescriptor, forKey: key)
                    }
                }
                for name in [NSWindow.didResizeNotification, NSWindow.didMoveNotification] {
                    self.watching.append(NotificationCenter.default.addObserver(forName: name, object: window, queue: .main, using: save))
                }
            }
        }

        override func viewWillMove(toWindow newWindow: NSWindow?) {
            super.viewWillMove(toWindow: newWindow)
            if newWindow == nil {
                watching.forEach(NotificationCenter.default.removeObserver)
                watching = []
            }
        }
    }
}

/// Saves the inspector's width under key as the user drags it, read
/// from its split view item's view: measuring the inspector from
/// SwiftUI (onGeometryChange) pins its column at one width.
struct InspectorWidth: NSViewRepresentable {
    let key: String

    func makeNSView(context: Context) -> NSView {
        Watcher(key: key)
    }

    func updateNSView(_ nsView: NSView, context: Context) {}

    private final class Watcher: NSView {
        let key: String
        private var watching: NSObjectProtocol?

        init(key: String) {
            self.key = key
            super.init(frame: .zero)
        }

        required init?(coder: NSCoder) { nil }

        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            if window == nil {
                watching.map(NotificationCenter.default.removeObserver)
                watching = nil
                return
            }
            DispatchQueue.main.async { [weak self] in
                self?.watch()
            }
        }

        /// The view of the split view item this one is in: the column.
        private func watch() {
            guard watching == nil else { return }
            var v: NSView? = superview
            while let s = v, !(s.superview is NSSplitView) { v = s.superview }
            guard let column = v else { return }
            column.postsFrameChangedNotifications = true
            watching = NotificationCenter.default.addObserver(forName: NSView.frameDidChangeNotification, object: column, queue: .main) { [key] n in
                guard let w = (n.object as? NSView)?.frame.width, w > 0 else { return }
                // A width outside its range is it sliding in or out.
                MainActor.assumeIsolated {
                    if (240...360).contains(w) { UserDefaults.standard.set(w.rounded(), forKey: key) }
                }
            }
        }
    }
}

/// File › New Task… (⌘N, T748 G1): a task added to the shown project's
/// board as tm task add adds it; the coordinator sees it there.
struct NewTaskSheet: View {
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @Environment(\.theme) private var theme
    let project: String
    @State private var form = NewTaskForm()
    @State private var busy = false
    @State private var error: String?
    /// What the last add said: the sheet stays for another.
    @State private var added: String?
    @FocusState private var titleFocused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            VStack(alignment: .leading, spacing: 4) {
                Text("New Task")
                    .font(theme.title(20, .heavy))
                Text("Adds a task to \(project)'s board. Its coordinator picks it up from there.")
                    .font(theme.body(12))
                    .foregroundStyle(theme.muted)
            }
            Form {
                TextField("Title", text: $form.title, prompt: Text("What needs doing, in one line"))
                    .focused($titleFocused)
                TextField("Notes", text: $form.notes, prompt: Text("Optional: details, links, acceptance"), axis: .vertical)
                    .lineLimit(4...8)
            }
            .formStyle(.grouped)
            .scrollDisabled(true)
            if let error {
                Text(error)
                    .font(theme.body(12))
                    .foregroundStyle(theme.attention)
            } else if let added {
                Label(added, systemImage: "checkmark.circle.fill")
                    .font(theme.body(12, .medium))
                    .foregroundStyle(theme.good)
            }
            HStack {
                Spacer()
                Button(added == nil ? "Cancel" : "Done") { dismiss() }
                    .keyboardShortcut(.cancelAction)
                Button(added == nil ? "Add Task" : "Add Another") { add() }
                    .keyboardShortcut(.defaultAction)
                    .disabled(!form.canAdd || busy)
            }
        }
        .padding(24)
        .frame(width: 480)
        .onAppear { titleFocused = true }
    }

    private func add() {
        busy = true
        error = nil
        Task {
            defer { busy = false }
            do {
                let r = try await app.addTask(form, project: project)
                added = NewTaskForm.done(r, project: project)
                form = NewTaskForm()
                titleFocused = true
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}
