import SwiftUI
import TerminatrKit
import TerminatrUI

/// "History ›", the way into the History view from the Pulse and the Done
/// heading (design board-library, B6).
struct HistoryLink: View {
    @Environment(\.theme) private var theme
    let open: () -> Void

    var body: some View {
        Button("History ›", action: open)
            .buttonStyle(.link)
            .foregroundStyle(theme.action)
            .font(theme.sized(Flat.chip, .medium))
            .help("Shows all the work done, with the releases and designs along it")
    }
}

/// The project's History in the Board's place (design board-library, step 9):
/// the facts, the curve of tasks done with what each day added and each repo's
/// releases under it along one axis, and the designs as Chapters. Nothing says
/// what a project hasn't: no releases, no ticks; no designs, no Chapters.
struct HistoryPane: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let page: HistoryPage
    let project: String
    let back: () -> Void
    /// The repo whose ticks show, where there are too many for a row each.
    @State private var repo = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(spacing: 5) {
                Button("Board", action: back)
                    .buttonStyle(.link)
                    .foregroundStyle(theme.action)
                Text("›").foregroundStyle(theme.muted)
                Text("History").foregroundStyle(theme.ink)
                Text(page.curve.range).foregroundStyle(theme.muted).padding(.leading, 6)
            }
            .font(theme.sized(Flat.label))
            factsRow
            if page.filtersRepos {
                Picker("Releases of", selection: $repo) {
                    Text("Any repo").tag("")
                    ForEach(page.curve.ticks, id: \.repo) { Text($0.repo).tag($0.repo) }
                }
                .pickerStyle(.menu)
                .fixedSize()
                .font(theme.sized(Flat.label))
            }
            HistoryChart(page: page, rows: page.tickRows(repo: repo))
            if !page.chapters.isEmpty { chapters }
        }
    }

    private var factsRow: some View {
        FactsFlow { facts }
    }

    @ViewBuilder
    private var facts: some View {
        ForEach(page.facts, id: \.word) { f in
            VStack(alignment: .leading, spacing: 0) {
                Text(f.value)
                    .font(theme.sized(17, .semibold))
                    .monospacedDigit()
                    .foregroundStyle(theme.ink)
                Text(f.word)
                    .font(theme.sized(Flat.chip))
                    .foregroundStyle(theme.muted)
            }
            .fixedSize()
            .accessibilityElement(children: .combine)
        }
    }

    private var chapters: some View {
        VStack(alignment: .leading, spacing: 7) {
            HStack(spacing: 6) {
                PaneLabel("Chapters · \(page.chapters.count)")
                Text(page.chapterWords)
                    .font(theme.sized(Flat.chip))
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
            }
            HairlineList {
                ForEach(page.chapters, id: \.name) { c in
                    Button { app.showDesign(project, c.name) } label: { chapter(c) }
                        .buttonStyle(.plain)
                        .help("Shows \(c.title) on the Designs tab")
                }
            }
        }
    }

    /// On one line where the whole title fits, else its numbers under it.
    private func chapter(_ c: AppAPI.HistoryChapter) -> some View {
        ListRow {
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 10) {
                    chapterTitle(c)
                    Spacer(minLength: 8)
                    chapterNumbers(c)
                }
                VStack(alignment: .leading, spacing: 4) {
                    chapterTitle(c)
                    HStack(spacing: 10) {
                        Spacer(minLength: 0)
                        chapterNumbers(c)
                    }
                }
            }
            .contentShape(Rectangle())
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(c.title), \(c.done) of \(c.total) tasks done\(c.label.isEmpty ? "" : ", \(c.label)")")
    }

    private func chapterTitle(_ c: AppAPI.HistoryChapter) -> some View {
        Text(c.title)
            .font(theme.sized(Flat.label))
            .foregroundStyle(theme.ink)
            .lineLimit(1)
            .truncationMode(.tail)
    }

    @ViewBuilder
    private func chapterNumbers(_ c: AppAPI.HistoryChapter) -> some View {
        if !c.label.isEmpty {
            Text(c.label)
                .font(theme.mono(.caption))
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .fixedSize()
        }
        ChapterBar(value: HistoryPage.progress(c))
        Text(verbatim: "\(c.done)/\(c.total)")
            .font(theme.mono(.caption))
            .foregroundStyle(theme.muted)
            .monospacedDigit()
            .frame(minWidth: 34, alignment: .trailing)
    }
}

/// The facts in a row, wrapping onto more where there are many repos' counts.
private struct FactsFlow: Layout {
    private let across: CGFloat = 22
    private let down: CGFloat = 8

    private func placed(_ views: Subviews, width: CGFloat) -> (spots: [CGPoint], size: CGSize) {
        var spots: [CGPoint] = []
        var at = CGPoint.zero
        var rowHigh: CGFloat = 0, wide: CGFloat = 0
        for v in views {
            let size = v.sizeThatFits(.unspecified)
            if at.x > 0 && at.x + size.width > width {
                at = CGPoint(x: 0, y: at.y + rowHigh + down)
                rowHigh = 0
            }
            spots.append(at)
            wide = max(wide, at.x + size.width)
            rowHigh = max(rowHigh, size.height)
            at.x += size.width + across
        }
        return (spots, CGSize(width: wide, height: at.y + rowHigh))
    }

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        placed(subviews, width: proposal.width ?? .infinity).size
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let spots = placed(subviews, width: bounds.width).spots
        for (v, at) in zip(subviews, spots) {
            v.place(at: CGPoint(x: bounds.minX + at.x, y: bounds.minY + at.y), proposal: .unspecified)
        }
    }
}

/// A chapter's progress: a thin bar.
private struct ChapterBar: View {
    @Environment(\.theme) private var theme
    let value: Double

    var body: some View {
        GeometryReader { box in
            ZStack(alignment: .leading) {
                Capsule().fill(theme.lineSoft)
                Capsule().fill(theme.action).frame(width: value > 0 ? max(box.size.width * value, 4) : 0)
            }
        }
        .frame(width: 80, height: 5)
        .accessibilityHidden(true)
    }
}

/// The curve and what is under it, along one time axis. Hovering a point says
/// what it holds; VoiceOver reads the same words, point by point.
private struct HistoryChart: View {
    @Environment(\.theme) private var theme
    let page: HistoryPage
    let rows: [HistoryPage.TickRow]
    @State private var hovered: Int?

    private static let flagRoom: CGFloat = 40
    private static let curveHigh: CGFloat = 190
    private static let barsHigh: CGFloat = 54
    private static let tickRow: CGFloat = 14
    private static let axisHigh: CGFloat = 16
    private static let gap: CGFloat = 12

    private var points: [AppAPI.CurvePoint] { page.curve.points }

    /// The room on the left: the curve's numbers, or a repo's name.
    private var gutter: CGFloat {
        let widest = rows.map { CGFloat($0.repo.count) * 6.2 }.max() ?? 0
        return min(max(36, widest + 8), 110)
    }

    private var height: CGFloat {
        Self.flagRoom + Self.curveHigh + Self.gap + Self.barsHigh + 6 + CGFloat(rows.count) * Self.tickRow + Self.axisHigh + 4
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            GeometryReader { box in
                let plot = max(box.size.width - gutter - 8, 1)
                ZStack(alignment: .topLeading) {
                    Canvas { context, _ in draw(&context, plot: plot) }
                    flagLabels(plot: plot)
                    hoverLine(plot: plot)
                }
                .contentShape(Rectangle())
                .onContinuousHover { phase in
                    switch phase {
                    case .active(let at):
                        let n = points.count
                        let step = n > 1 ? run(plot) / CGFloat(n - 1) : plot
                        hovered = n > 1 ? min(max(Int(((at.x - gutter - Self.inset) / step).rounded()), 0), n - 1) : 0
                    case .ended:
                        hovered = nil
                    }
                }
            }
            .frame(height: height)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("Tasks done, \(points.last?.total ?? 0) in all over \(page.curve.span), by \(page.curve.unit)")
            .accessibilityChildren {
                ForEach(points, id: \.from) { Text($0.tip) }
            }
            HStack(spacing: 8) {
                if let hovered, points.indices.contains(hovered) {
                    Text(points[hovered].tip).foregroundStyle(theme.ink)
                }
                Spacer(minLength: 0)
                Text(rows.isEmpty ? "bars: done per \(page.curve.unit)" : "bars: done per \(page.curve.unit) · ticks: releases")
                    .foregroundStyle(theme.muted)
            }
            .font(theme.sized(Flat.chip))
            .lineLimit(1)
        }
    }

    /// Clear of the numbers on the left: half the widest bar.
    private static let inset: CGFloat = 10

    /// How wide the points run, inside the plot.
    private func run(_ plot: CGFloat) -> CGFloat { max(plot - 2 * Self.inset, 1) }

    private func x(_ i: Int, _ plot: CGFloat) -> CGFloat {
        let start = gutter + Self.inset
        return points.count > 1 ? start + CGFloat(i) * run(plot) / CGFloat(points.count - 1) : start + run(plot) / 2
    }

    private func curveY(_ total: Int) -> CGFloat {
        let top = max(points.last?.total ?? 1, 1)
        return Self.flagRoom + Self.curveHigh * (1 - CGFloat(total) / CGFloat(top))
    }

    private func draw(_ context: inout GraphicsContext, plot: CGFloat) {
        let bottom = curveY(0)
        // The grid and its numbers.
        for value in page.gridLines {
            let y = curveY(value)
            context.stroke(Path { $0.move(to: CGPoint(x: gutter, y: y)); $0.addLine(to: CGPoint(x: gutter + plot, y: y)) }, with: .color(theme.lineSoft), lineWidth: 1)
            context.draw(Text(verbatim: "\(value)").font(theme.sized(10.5)).foregroundStyle(theme.muted), at: CGPoint(x: gutter - 6, y: y), anchor: .trailing)
        }
        // The area and the line over it.
        if points.count > 1 {
            var line = Path()
            for (i, p) in points.enumerated() {
                let at = CGPoint(x: x(i, plot), y: curveY(p.total))
                if i == 0 { line.move(to: at) } else { line.addLine(to: at) }
            }
            var area = line
            area.addLine(to: CGPoint(x: x(points.count - 1, plot), y: bottom))
            area.addLine(to: CGPoint(x: x(0, plot), y: bottom))
            area.closeSubpath()
            context.fill(area, with: .color(theme.action.opacity(0.11)))
            context.stroke(line, with: .color(theme.action), style: StrokeStyle(lineWidth: 2, lineJoin: .round))
        }
        // The end, and how many in all.
        if let last = points.last {
            let end = CGPoint(x: x(points.count - 1, plot), y: curveY(last.total))
            context.fill(Path(ellipseIn: CGRect(x: end.x - 4, y: end.y - 4, width: 8, height: 8)), with: .color(theme.pane))
            context.fill(Path(ellipseIn: CGRect(x: end.x - 3, y: end.y - 3, width: 6, height: 6)), with: .color(theme.action))
            // Under the line where the words run, so it crosses neither.
            let words = 72 + CGFloat("\(last.total)".count) * 7
            let from = end.x - words
            var under = points.indices.filter { x($0, plot) >= from }.map { curveY(points[$0].total) }.max() ?? end.y
            // The words' left edge can fall between two points, on the line joining them.
            if let right = points.indices.first(where: { x($0, plot) >= from }), right > 0 {
                let a = x(right - 1, plot), b = x(right, plot)
                let share = (from - a) / max(b - a, 1)
                under = max(under, curveY(points[right - 1].total) + share * (curveY(points[right].total) - curveY(points[right - 1].total)))
            }
            context.draw(Text(verbatim: "\(last.total) done").font(theme.sized(12.5, .semibold)).foregroundStyle(theme.ink), at: CGPoint(x: end.x, y: min(under + 14, bottom - 8)), anchor: .trailing)
        }
        // Flags on the curve.
        for f in page.flags(width: Double(run(plot)), height: Double(Self.curveHigh)) {
            let px = x(f.point, plot), py = curveY(points[f.point].total)
            let topY = py - CGFloat(HistoryPage.rise(high: f.high))
            context.stroke(Path { $0.move(to: CGPoint(x: px, y: py)); $0.addLine(to: CGPoint(x: px, y: topY)) }, with: .color(theme.muted.opacity(0.7)), lineWidth: 1)
            let dot = CGRect(x: px - 3, y: py - 3, width: 6, height: 6)
            context.fill(Path(ellipseIn: dot), with: .color(theme.pane))
            context.stroke(Path(ellipseIn: dot), with: .color(theme.action), lineWidth: 2)
        }
        // The bars.
        let barsTop = bottom + Self.gap
        let barsBottom = barsTop + Self.barsHigh
        let top = max(page.topDone, 1)
        let step = points.count > 1 ? run(plot) / CGFloat(points.count - 1) : plot
        let barWide = max(min(step * 0.72, 18), 1)
        context.draw(Text(verbatim: "\(page.topDone)").font(theme.sized(10.5)).foregroundStyle(theme.muted), at: CGPoint(x: gutter - 6, y: barsTop + 5), anchor: .trailing)
        context.draw(Text("0").font(theme.sized(10.5)).foregroundStyle(theme.muted), at: CGPoint(x: gutter - 6, y: barsBottom - 5), anchor: .trailing)
        for (i, p) in points.enumerated() where p.done > 0 {
            let high = max(Self.barsHigh * CGFloat(p.done) / CGFloat(top), 2)
            let rect = CGRect(x: x(i, plot) - barWide / 2, y: barsBottom - high, width: barWide, height: high)
            let on = hovered == i || (hovered == nil && i == points.count - 1)
            context.fill(Path(roundedRect: rect, cornerRadius: min(2, barWide / 2)), with: .color(theme.action.opacity(on ? 1 : 0.62)))
        }
        // A tick row for each repo.
        var y = barsBottom + 6
        for row in rows {
            if !row.repo.isEmpty {
                context.draw(Text(row.repo).font(theme.sized(10.5)).foregroundStyle(theme.muted), at: CGPoint(x: gutter - 6, y: y + 5), anchor: .trailing)
            }
            for i in row.points where points.indices.contains(i) {
                context.fill(Path(CGRect(x: x(i, plot) - 0.75, y: y, width: 1.5, height: 10)), with: .color(theme.muted))
            }
            y += Self.tickRow
        }
        // The axis.
        for i in page.axis(width: Double(run(plot))) {
            let at = x(i, plot)
            let anchor: UnitPoint = at - gutter < 26 ? .leading : gutter + plot - at < 26 ? .trailing : .center
            context.draw(Text(points[i].label).font(theme.sized(10.5)).foregroundStyle(theme.muted), at: CGPoint(x: at, y: y + 8), anchor: anchor)
        }
    }

    /// The flags' names: text a person can read and hover for the whole title.
    private func flagLabels(plot: CGFloat) -> some View {
        ForEach(page.flags(width: Double(run(plot)), height: Double(Self.curveHigh)), id: \.name) { f in
            let px = x(f.point, plot), py = curveY(points[f.point].total)
            let wide = CGFloat(f.name.count) * 6.2 + 8
            let leftward = px - gutter - Self.inset >= wide
            Text(f.name)
                .font(theme.sized(11, .medium))
                .foregroundStyle(theme.ink)
                .lineLimit(1)
                .fixedSize()
                .frame(width: wide, alignment: leftward ? .trailing : .leading)
                .help(f.title)
                .offset(x: leftward ? px - wide - 4 : px + 4, y: py - CGFloat(HistoryPage.rise(high: f.high)) - 8)
        }
    }

    @ViewBuilder
    private func hoverLine(plot: CGFloat) -> some View {
        if let hovered, points.indices.contains(hovered) {
            Rectangle()
                .fill(theme.muted.opacity(0.5))
                .frame(width: 1, height: height - Self.flagRoom)
                .offset(x: x(hovered, plot), y: Self.flagRoom)
                .allowsHitTesting(false)
        }
    }
}
