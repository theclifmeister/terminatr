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
        FlatSheet(width: 480) {
            SheetTitle("New Project", detail: "Its name becomes a lower-case slug. Its coordinator starts once you open it.")
        } content: {
            SheetBody {
                SheetField("Name") {
                    TextField("Name", text: $form.name, prompt: Text("demo-app"))
                        .sheetField()
                        .focused($nameFocused)
                }
                SheetField("Repo") {
                    HStack(spacing: 8) {
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
                if let error {
                    Text(error)
                        .font(theme.sized(Flat.label))
                        .foregroundStyle(theme.attention)
                } else if let made {
                    Label(made, systemImage: "checkmark.circle.fill")
                        .font(theme.sized(Flat.label, .medium))
                        .foregroundStyle(theme.good)
                }
            }
        } footer: {
            Button(made == nil ? "Cancel" : "Done") { dismiss() }
                .keyboardShortcut(.cancelAction)
            if made == nil {
                Button("Create Project") { make() }
                    .buttonStyle(.flatPrimary)
                    .keyboardShortcut(.defaultAction)
                    .disabled(!form.canMake || busy)
            }
        }
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
        FlatSheet(width: 480) {
            SheetTitle("Write to a Linked Project", detail: "Sent as you for \(project).")
        } content: {
            SheetBody {
                SheetField("To") {
                    let peer = links.first { $0.peer == form.peer }
                    SheetPicker(label: "To", shown: (peer?.shown.isEmpty == false ? peer?.shown : peer?.peer) ?? form.peer, selection: $form.peer) {
                        ForEach(links, id: \.peer) { l in
                            Text(l.shown.isEmpty ? l.peer : l.shown).tag(l.peer)
                        }
                    }
                }
                SheetField("Message") {
                    TextField("Message", text: $form.text, prompt: Text("Your message"), axis: .vertical)
                        .lineLimit(4...10)
                        .sheetField()
                        .focused($textFocused)
                }
                if let error {
                    Text(error)
                        .font(theme.sized(Flat.label))
                        .foregroundStyle(theme.attention)
                }
            }
        } footer: {
            Button("Cancel") { dismiss() }
                .keyboardShortcut(.cancelAction)
            Button("Send") { send() }
                .buttonStyle(.flatPrimary)
                .keyboardShortcut(.defaultAction)
                .disabled(!form.canSend || busy)
        }
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
