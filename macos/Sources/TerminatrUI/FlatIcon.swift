import SwiftUI

/// The flat look's line icons (T1008): the design page's own, drawn on its
/// 24 pt grid, where an SF Symbol of the same side is heavier and larger.
public enum FlatGlyph: Sendable {
    case gear, info, board, mark, books, mod, tool

    fileprivate func path() -> Path {
        var p = Path()
        func line(_ points: (CGFloat, CGFloat)...) {
            p.addLines(points.map { CGPoint(x: $0.0, y: $0.1) })
        }
        func shape(_ points: (CGFloat, CGFloat)...) {
            p.addLines(points.map { CGPoint(x: $0.0, y: $0.1) })
            p.closeSubpath()
        }
        func circle(_ x: CGFloat, _ y: CGFloat, _ r: CGFloat) {
            p.addEllipse(in: CGRect(x: x - r, y: y - r, width: 2 * r, height: 2 * r))
        }
        func box(_ x: CGFloat, _ y: CGFloat, _ w: CGFloat, _ h: CGFloat, _ r: CGFloat) {
            p.addRoundedRect(in: CGRect(x: x, y: y, width: w, height: h), cornerSize: CGSize(width: r, height: r))
        }
        func corner(_ x1: CGFloat, _ y1: CGFloat, _ x2: CGFloat, _ y2: CGFloat, _ r: CGFloat) {
            p.addArc(tangent1End: CGPoint(x: x1, y: y1), tangent2End: CGPoint(x: x2, y: y2), radius: r)
        }
        switch self {
        case .gear:
            circle(12, 12, 3)
            circle(12, 12, 7.5)
            line((12, 2.5), (12, 4.5))
            line((12, 19.5), (12, 21.5))
            line((2.5, 12), (4.5, 12))
            line((19.5, 12), (21.5, 12))
            line((5.3, 5.3), (6.7, 6.7))
            line((17.3, 17.3), (18.7, 18.7))
            line((5.3, 18.7), (6.7, 17.3))
            line((17.3, 6.7), (18.7, 5.3))
        case .info:
            circle(12, 12, 8.5)
            line((12, 11), (12, 16.5))
            line((12, 7.8), (12, 7.9))
        case .board:
            box(3, 5, 18, 14, 2)
            line((7, 9.5), (17, 9.5))
            line((7, 12.5), (17, 12.5))
            line((7, 15.5), (13, 15.5))
        case .mark:
            shape((7, 3), (17, 3), (17, 21), (12, 17), (7, 21))
        case .books:
            box(4, 5, 4, 15, 1)
            box(9.5, 8, 4, 12, 1)
            shape((15.5, 6.5), (18.9, 5.7), (21.2, 19.3), (17.8, 20))
        case .mod:
            p.move(to: CGPoint(x: 5, y: 6))
            p.addLine(to: CGPoint(x: 9.5, y: 6))
            p.addArc(center: CGPoint(x: 12, y: 6), radius: 2.5, startAngle: .degrees(180), endAngle: .degrees(360), clockwise: false)
            p.addLine(to: CGPoint(x: 18, y: 6))
            corner(19, 6, 19, 7, 1)
            p.addLine(to: CGPoint(x: 19, y: 10.5))
            p.addArc(center: CGPoint(x: 19, y: 13), radius: 2.5, startAngle: .degrees(-90), endAngle: .degrees(90), clockwise: false)
            p.addLine(to: CGPoint(x: 19, y: 19))
            corner(19, 20, 18, 20, 1)
            p.addLine(to: CGPoint(x: 5, y: 20))
            corner(4, 20, 4, 19, 1)
            p.addLine(to: CGPoint(x: 4, y: 7))
            corner(4, 6, 5, 6, 1)
            p.closeSubpath()
        case .tool:
            line((4, 7), (9, 12), (4, 17))
        }
        return p
    }
}

private struct GlyphShape: Shape {
    let glyph: FlatGlyph

    func path(in rect: CGRect) -> Path {
        let k = rect.width / 24
        return glyph.path().applying(CGAffineTransform(scaleX: k, y: k).concatenating(CGAffineTransform(translationX: rect.minX, y: rect.minY)))
    }
}

/// A FlatGlyph in the foreground colour, side points square at 100%, its
/// line 1.5 on the 24 pt grid, as the design page's `svg`.
public struct FlatIcon: View {
    @Environment(\.theme) private var theme
    let glyph: FlatGlyph
    let side: CGFloat

    public init(_ glyph: FlatGlyph, side: CGFloat = 15) {
        self.glyph = glyph
        self.side = side
    }

    public var body: some View {
        let s = side * theme.scale
        GlyphShape(glyph: glyph)
            .stroke(style: StrokeStyle(lineWidth: 1.5 * s / 24, lineCap: .round, lineJoin: .round))
            .frame(width: s, height: s)
            .accessibilityHidden(true)
    }
}
