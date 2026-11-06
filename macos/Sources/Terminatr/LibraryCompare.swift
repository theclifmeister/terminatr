import AppKit
import SwiftUI
import TerminatrKit
import TerminatrUI

/// A stack's before and after side by side (T1057), before on the left, each
/// read through tm as Quick Look's copy is. Read only; esc closes it.
struct LibraryCompare: View {
    @Environment(\.theme) private var theme
    @Environment(\.dismiss) private var dismiss
    let row: LibraryPage.Row
    let load: (AppAPI.LibraryFile) async -> Data?
    /// What each file came to, by name; none yet while it reads.
    @State private var read: [String: NSImage?] = [:]

    var body: some View {
        FlatSheet {
            SheetTitle(row.title)
                .lineLimit(1)
                .truncationMode(.middle)
        } content: {
            HStack(alignment: .top, spacing: 12) {
                ForEach(row.sideBySide, id: \.word) { side in
                    VStack(alignment: .leading, spacing: 5) {
                        HStack(spacing: 7) {
                            Text(side.word.capitalized).font(theme.sized(Flat.label, .medium))
                            Text("\(side.file.name) · \(Attachment.size(side.file.size))")
                                .font(theme.sized(Flat.label))
                                .foregroundStyle(theme.muted)
                                .lineLimit(1)
                                .truncationMode(.middle)
                        }
                        picture(side.file)
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
                }
            }
            .padding(12)
            .frame(minWidth: 720, idealWidth: 1040, maxWidth: .infinity, minHeight: 420, idealHeight: 640, maxHeight: .infinity)
        } footer: {
            Button("Done") { dismiss() }
                .keyboardShortcut(.cancelAction)
        }
        .task {
            for side in row.sideBySide {
                let image = await load(side.file).flatMap(NSImage.init(data:))
                read[side.file.name] = .some(image)
            }
        }
    }

    @ViewBuilder
    private func picture(_ f: AppAPI.LibraryFile) -> some View {
        Group {
            switch read[f.name] {
            case .none:
                ProgressView().controlSize(.small)
            case .some(.none):
                Label("This image can't be drawn", systemImage: "eye.slash")
                    .foregroundStyle(theme.muted)
            case .some(.some(let image)):
                Image(nsImage: image)
                    .resizable()
                    .aspectRatio(contentMode: .fit)
                    .accessibilityLabel(f.name)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}
