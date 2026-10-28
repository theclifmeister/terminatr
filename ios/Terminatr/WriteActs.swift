import SwiftUI
import TerminatrKit
import TerminatrUI

/// The board's New Task (T821), as the Mac's File › New Task…: a task
/// added to the project's board as tm task add adds it, with the
/// templates of the project's mods when this tm offers them. It stays
/// open after an add, for another.
struct NewTaskSheet: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @Environment(\.theme) private var theme
    let project: String
    @State private var form = NewTaskForm()
    @State private var templates: [AppAPI.Template] = []
    @State private var busy = false
    @State private var error: String?
    @State private var added: String?
    @FocusState private var titleFocused: Bool

    private var template: AppAPI.Template? {
        templates.first { $0.ref == form.template }
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Title", text: $form.title, prompt: Text("What needs doing, in one line"))
                        .focused($titleFocused)
                        .submitLabel(.done)
                        .onSubmit(add)
                    TextField("Notes", text: $form.notes,
                              prompt: Text(template.map { $0.notes.isEmpty ? "Optional: details, links, acceptance" : "Empty: the \($0.name) template's notes" }
                                           ?? "Optional: details, links, acceptance"),
                              axis: .vertical)
                        .lineLimit(4...10)
                } footer: {
                    Text("Adds it to \(project)'s board. Its coordinator picks it up from there.")
                }
                if !templates.isEmpty {
                    Section {
                        Picker("Template", selection: $form.template) {
                            Text("No template").tag("")
                            ForEach(templates, id: \.ref) { t in
                                Text(NewTaskForm.label(t, among: templates)).tag(t.ref)
                            }
                        }
                        if let template {
                            LabeledContent("Adds", value: NewTaskForm.title(form.title, with: template))
                            if !template.steps.isEmpty {
                                LabeledContent("Steps", value: "\(template.steps.count)")
                            }
                        }
                    }
                }
                if let error {
                    Failure(text: error)
                } else if let added {
                    Label(added, systemImage: "checkmark.circle.fill")
                        .font(theme.body(.subheadline, .medium))
                        .foregroundStyle(theme.good)
                }
            }
            .navigationTitle("New Task")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(added == nil ? "Cancel" : "Done") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(added == nil ? "Add" : "Add Another", action: add)
                        .disabled(!form.canAdd || busy)
                }
            }
        }
        .onAppear { titleFocused = true }
        .task { templates = (try? await model.templates(project)) ?? [] }
    }

    private func add() {
        guard form.canAdd, !busy else { return }
        busy = true
        error = nil
        Task {
            defer { busy = false }
            do {
                let r = try await model.addTask(form, project: project)
                added = NewTaskForm.done(r, project: project)
                form = NewTaskForm(template: form.template)
                titleFocused = true
            } catch {
                self.error = NeedActs.words(error)
            }
        }
    }
}

/// Delegate… on a task that is open, on deck or blocked (T821), as the
/// Mac's TaskActs: it asks first, then only asks the project's
/// coordinator (task.delegate, the TUI's D), and says what came of it
/// (TaskAsk) while the task keeps its status.
struct DelegateButton: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let project: String
    let task: String
    let status: String
    @State private var asking = false
    @State private var ask: TaskAsk?

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if let ask, ask.stands(status) {
                Label(ask.text, systemImage: ask.symbol)
                    .font(theme.body(.subheadline))
                    .foregroundStyle(ask.tone == .attention ? theme.attention : theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if ask.map({ !$0.holdsButtons || !$0.stands(status) }) ?? true {
                Button {
                    asking = true
                } label: {
                    Label("Delegate…", systemImage: "person.badge.plus")
                        .font(theme.body(.body, .medium))
                }
                .buttonStyle(.bordered)
                .accessibilityLabel("Delegate \(task)…")
            }
        }
        .confirmationDialog("Delegate \(task)?", isPresented: $asking, titleVisibility: .visible) {
            Button("Ask to Delegate") { delegate() }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("\(project)'s coordinator is asked to delegate it: it starts a thread for it when it handles the ask.")
        }
    }

    private func delegate() {
        var a = TaskAsk(task: task, kind: .delegate, status: status)
        ask = a
        Task {
            do {
                a.phase = try await model.delegate(project: project, task: task) ? .asked : .waiting
            } catch {
                a.phase = .failed(NeedActs.words(error) ?? Dashboard.words(error, task: task))
            }
            ask = a
            AccessibilityNotification.Announcement(a.text).post()
        }
    }
}

/// Start Coordinator for an active project whose coordinator isn't
/// running (T821), as the Mac dashboard's: project.open, with the
/// project's agent and model.
struct StartCoordinatorButton: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let project: String
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Button {
                start()
            } label: {
                HStack {
                    Label("Start Coordinator", systemImage: "play.fill")
                    if busy { ProgressView().controlSize(.small) }
                }
                .font(theme.body(.body, .medium))
            }
            .disabled(busy)
            .accessibilityHint("Starts it with the project's agent and model, as tm project open does")
            if let error {
                Failure(text: error)
            }
        }
    }

    private func start() {
        busy = true
        error = nil
        Task {
            do {
                try await model.startCoordinator(project)
            } catch {
                self.error = NeedActs.words(error)
            }
            busy = false
        }
    }
}
