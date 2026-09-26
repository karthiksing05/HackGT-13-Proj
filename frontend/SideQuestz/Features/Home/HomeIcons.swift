import SwiftUI

/// The prototype's thin line icons (24-unit viewBox, round caps and joins), drawn as shapes so the
/// Event sheet matches it exactly: clock, pin, globe, walk, train, car and the × of the calendar bar.
struct HomeIcon: View {
    enum Glyph {
        case clock, pin, globe, walk, train, car, close
    }

    let glyph: Glyph
    var size: CGFloat = 18
    /// Stroke width in viewBox units, like the SVG's `stroke-width`.
    var strokeWidth: CGFloat = 1.8

    var body: some View {
        HomeIconShape(glyph: glyph)
            .stroke(style: StrokeStyle(lineWidth: strokeWidth * size / 24, lineCap: .round, lineJoin: .round))
            .frame(width: size, height: size)
            .accessibilityHidden(true)
    }
}

private struct HomeIconShape: Shape {
    let glyph: HomeIcon.Glyph

    func path(in rect: CGRect) -> Path {
        let s = min(rect.width, rect.height) / 24
        func p(_ x: CGFloat, _ y: CGFloat) -> CGPoint { CGPoint(x: rect.minX + x * s, y: rect.minY + y * s) }
        func circle(_ x: CGFloat, _ y: CGFloat, _ r: CGFloat) -> CGRect {
            CGRect(x: rect.minX + (x - r) * s, y: rect.minY + (y - r) * s, width: 2 * r * s, height: 2 * r * s)
        }
        func polyline(_ path: inout Path, _ points: [(CGFloat, CGFloat)]) {
            guard let first = points.first else { return }
            path.move(to: p(first.0, first.1))
            for point in points.dropFirst() { path.addLine(to: p(point.0, point.1)) }
        }

        var path = Path()
        switch glyph {
        case .clock:
            // circle r 8.5 + "M12 7.5V12l3 2"
            path.addEllipse(in: circle(12, 12, 8.5))
            polyline(&path, [(12, 7.5), (12, 12), (15, 14)])
        case .pin:
            // "M12 21s-6.5-6.2-6.5-11.2a6.5 6.5 0 0 1 13 0C18.5 14.8 12 21 12 21z" + circle (12, 9.8) r 2.3
            path.move(to: p(12, 21))
            path.addCurve(to: p(5.5, 9.8), control1: p(12, 21), control2: p(5.5, 14.8))
            path.addArc(center: p(12, 9.8), radius: 6.5 * s, startAngle: .degrees(180), endAngle: .degrees(360), clockwise: false)
            path.addCurve(to: p(12, 21), control1: p(18.5, 14.8), control2: p(12, 21))
            path.closeSubpath()
            path.addEllipse(in: circle(12, 9.8, 2.3))
        case .globe:
            // circle r 8.5 + "M3.5 12h17M12 3.5c2.5 2.6 3.5 5.4 3.5 8.5s-1 5.9-3.5 8.5c-2.5-2.6-3.5-5.4-3.5-8.5s1-5.9 3.5-8.5z"
            path.addEllipse(in: circle(12, 12, 8.5))
            polyline(&path, [(3.5, 12), (20.5, 12)])
            path.move(to: p(12, 3.5))
            path.addCurve(to: p(15.5, 12), control1: p(14.5, 6.1), control2: p(15.5, 8.9))
            path.addCurve(to: p(12, 20.5), control1: p(15.5, 15.1), control2: p(14.5, 17.9))
            path.addCurve(to: p(8.5, 12), control1: p(9.5, 17.9), control2: p(8.5, 15.1))
            path.addCurve(to: p(12, 3.5), control1: p(8.5, 8.9), control2: p(9.5, 6.1))
            path.closeSubpath()
        case .walk:
            // circle (13, 4.5) r 1.8 + three strokes
            path.addEllipse(in: circle(13, 4.5, 1.8))
            polyline(&path, [(10, 21), (12.2, 14.8), (10, 12), (11, 7.5), (14, 10), (17, 11)])
            polyline(&path, [(11, 7.5), (8, 9.5), (7, 13)])
            polyline(&path, [(12.2, 14.8), (15, 21)])
        case .train:
            // rect (6, 3.5, 12 × 13, rx 3) + "M6 11h12M9 16.5 7 21M15 16.5l2 4.5"
            path.addRoundedRect(in: CGRect(x: rect.minX + 6 * s, y: rect.minY + 3.5 * s, width: 12 * s, height: 13 * s),
                                cornerSize: CGSize(width: 3 * s, height: 3 * s))
            polyline(&path, [(6, 11), (18, 11)])
            polyline(&path, [(9, 16.5), (7, 21)])
            polyline(&path, [(15, 16.5), (17, 21)])
        case .car:
            // "M4 16.5v-4l2-5.5h12l2 5.5v4z" + wheels (8, 16.5) and (16, 16.5) r 2
            polyline(&path, [(4, 16.5), (4, 12.5), (6, 7), (18, 7), (20, 12.5), (20, 16.5)])
            path.closeSubpath()
            path.addEllipse(in: circle(8, 16.5, 2))
            path.addEllipse(in: circle(16, 16.5, 2))
        case .close:
            // "M6 6l12 12M18 6 6 18"
            polyline(&path, [(6, 6), (18, 18)])
            polyline(&path, [(18, 6), (6, 18)])
        }
        return path
    }
}
