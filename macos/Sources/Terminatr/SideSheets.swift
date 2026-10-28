import SwiftUI
import TerminatrKit
import TerminatrUI

/// File › New Project… (⇧⌘N, app API 22), as the TUI's +: its name,
/// then, if it has one, its repo's folder; tm makes the folder and the
/// project shows once made.
struct NewProjectSheet: View {
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @Environment(\.theme) private var theme
    @State private var form = NewProjectForm()
    @State private var busy = false
    @State private var error: String?
    /// What the make said when worth a word (no agent): the sheet stays
    /// to say it.
    @State private var made: String?
    @State private var picking = false
    @FocusState private var nameFocused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            VStack(alignment: .leading, spacing: 4) {
                Text("New Project")
                    .font(theme.title(.title, .heavy))
                Text("Its name becomes a lower-case slug. Its coordinator starts once you open it.")
                    .font(theme.body(.subheadline))
                    .foregroundStyle(theme.muted)
            }
            Form {
                TextField("Name", text: $form.name, prompt: Text("demo-app"))
                    .focused($nameFocused)
                LabeledContent("Repo") {
                    HStack {
                        Text(form.repo.isEmpty ? "None" : (form.repo as NSString).abbreviatingWithTildeInPath)
                            .foregroundStyle(form.repo.isEmpty ? theme.muted : theme.ink)
                            .lineLimit(1)
                            .truncationMode(.middle)
                        Spacer()
                        if !form.repo.isEmpty {
                            Button("Clear") { form.repo = "" }
                        }
                        Button("Choose…") { picking = true }
                    }
                }
            }
            .formStyle(.grouped)
            .scrollDisabled(true)
            if let error {
                Text(error)
                    .font(theme.body(.subheadline))
                    .foregroundStyle(theme.attention)
            } else if let made {
                Label(made, systemImage: "checkmark.circle.fill")
                    .font(theme.body(.subheadline, .medium))
                    .foregroundStyle(theme.good)
            }
            HStack {
                Spacer()
                Button(made == nil ? "Cancel" : "Done") { dismiss() }
                    .keyboardShortcut(.cancelAction)
                if made == nil {
                    Button("Create Project") { make() }
                        .keyboardShortcut(.defaultAction)
                        .disabled(!form.canMake || busy)
                }
            }
        }
        .padding(24)
        .frame(width: 480)
        .onAppear { nameFocused = true }
        .fileImporter(isPresented: $picking, allowedContentTypes: [.folder]) { result in
            if case .success(let url) = result { form.repo = url.path }
        }
    }

    private func make() {
        busy = true
        error = nil
        Task {
            defer { busy = false }
            do {
                let r = try await app.makeProject(form)
                if r.noAgent {
                    made = NewProjectForm.done(r)
                } else {
                    dismiss()
                }
            } catch {
                self.error = Dashboard.words(error)
            }
        }
    }
}

/// Write to Linked Project… (app API 22), as the TUI's M: a message the
/// user writes as the project to one of its links that send.
struct LinkMessageSheet: View {
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @Environment(\.theme) private var theme
    let project: String
    @State var form: LinkMessageForm
    @State private var busy = false
    @State private var error: String?
    @FocusState private var textFocused: Bool

    var body: some View {
        let links = app.project(project).map { LinkMessageForm.writable($0) } ?? []
        VStack(alignment: .leading, spacing: 14) {
            VStack(alignment: .leading, spacing: 4) {
                Text("Write to a Linked Project")
                    .font(theme.title(.title, .heavy))
                Text("Sent as you for \(project).")
                    .font(theme.body(.subheadline))
                    .foregroundStyle(theme.muted)
            }
            Form {
                Picker("To", selection: $form.peer) {
                    ForEach(links, id: \.peer) { l in
                        Text(l.shown.isEmpty ? l.peer : l.shown).tag(l.peer)
                    }
                }
                TextField("Message", text: $form.text, prompt: Text("Your message"), axis: .vertical)
                    .lineLimit(4...10)
                    .focused($textFocused)
            }
            .formStyle(.grouped)
            .scrollDisabled(true)
            if let error {
                Text(error)
                    .font(theme.body(.subheadline))
                    .foregroundStyle(theme.attention)
            }
            HStack {
                Spacer()
                Button("Cancel") { dismiss() }
                    .keyboardShortcut(.cancelAction)
                Button("Send") { send() }
                    .keyboardShortcut(.defaultAction)
                    .disabled(!form.canSend || busy)
            }
        }
        .padding(24)
        .frame(width: 480)
        .onAppear { textFocused = true }
    }

    private func send() {
        busy = true
        error = nil
        Task {
            defer { busy = false }
            do {
                let words = try await app.sendLinkMessage(form, project: project)
                app.askSaid = ("Message sent", words.prefix(1).uppercased() + words.dropFirst() + ".")
                dismiss()
            } catch {
                self.error = Dashboard.words(error)
            }
        }
    }
}
