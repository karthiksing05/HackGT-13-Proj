import SwiftUI

/// The prototype's star (24-unit viewBox path), used for ratings everywhere.
struct StarShape: Shape {
    func path(in rect: CGRect) -> Path {
        let points: [(CGFloat, CGFloat)] = [(12, 3.5), (14.6, 8.9), (20.5, 9.7), (16.2, 13.8), (17.2, 19.6),
                                            (12, 16.9), (6.8, 19.6), (7.8, 13.8), (3.5, 9.7), (9.4, 8.9)]
        let s = min(rect.width, rect.height) / 24
        var path = Path()
        for (i, p) in points.enumerated() {
            let point = CGPoint(x: rect.minX + p.0 * s, y: rect.minY + p.1 * s)
            if i == 0 { path.move(to: point) } else { path.addLine(to: point) }
        }
        path.closeSubpath()
        return path
    }
}

/// One star: filled sage when on, outline otherwise.
struct StarGlyph: View {
    var filled: Bool
    var size: CGFloat
    var emptyStroke: Color = Theme.mutedStar
    var lineWidth: CGFloat = 1.8

    var body: some View {
        ZStack {
            if filled { StarShape().fill(Theme.sage) }
            StarShape().stroke(filled ? Theme.sage : emptyStroke, style: StrokeStyle(lineWidth: lineWidth * size / 24, lineJoin: .round))
        }
        .frame(width: size, height: size)
    }
}

/// Read-only row of 5 stars (Past rows, Account history: 14pt).
struct StarRow: View {
    var rating: Int
    var size: CGFloat = 14
    var spacing: CGFloat = 1
    var emptyStroke: Color = Theme.mutedStar

    var body: some View {
        HStack(spacing: spacing) {
            ForEach(1...5, id: \.self) { StarGlyph(filled: $0 <= rating, size: size, emptyStroke: emptyStroke) }
        }
        .accessibilityElement()
        .accessibilityLabel("\(rating) of 5 stars")
    }
}

/// Tappable stars (Rate sheet: 40pt in 52pt targets; Event sheet: 26pt in 36×44 targets).
struct StarPicker: View {
    @Binding var rating: Int
    var size: CGFloat = 40
    var target = CGSize(width: 52, height: 52)
    var spacing: CGFloat = 6
    var emptyStroke: Color = Theme.mutedStar
    var lineWidth: CGFloat = 1.5

    var body: some View {
        HStack(spacing: spacing) {
            ForEach(1...5, id: \.self) { n in
                Button {
                    rating = n
                } label: {
                    StarGlyph(filled: n <= rating, size: size, emptyStroke: emptyStroke, lineWidth: lineWidth)
                        .sqBounce(when: n <= rating, scale: 1.2)
                        .frame(width: target.width, height: target.height)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("\(n) star\(n > 1 ? "s" : ""), \(Rating.starWords[n])")
                .accessibilityAddTraits(n == rating ? .isSelected : [])
            }
        }
        .animation(Motion.quick, value: rating)
        .sensoryFeedback(.selection, trigger: rating)
    }
}

/// The checkmark path from the prototype ("M5 12.5 10 17.5 19 7") in a square frame.
struct CheckShape: Shape {
    func path(in rect: CGRect) -> Path {
        let s = min(rect.width, rect.height) / 24
        var p = Path()
        p.move(to: CGPoint(x: rect.minX + 5 * s, y: rect.minY + 12.5 * s))
        p.addLine(to: CGPoint(x: rect.minX + 10 * s, y: rect.minY + 17.5 * s))
        p.addLine(to: CGPoint(x: rect.minX + 19 * s, y: rect.minY + 7 * s))
        return p
    }
}

/// The checkmark stroke from the prototype.
/// `lineWidth` is the SVG stroke-width in the 24-unit viewBox (it scales with the frame, like the SVG).
struct CheckGlyph: View {
    var lineWidth: CGFloat = 2.6

    var body: some View {
        GeometryReader { proxy in
            let s = min(proxy.size.width, proxy.size.height) / 24
            CheckShape()
                .stroke(style: StrokeStyle(lineWidth: lineWidth * s, lineCap: .round, lineJoin: .round))
        }
        .accessibilityHidden(true)
    }
}

/// The prototype's outline calendar (Create › When, Setup › Connect): a 17×15 rounded rect, a
/// header rule and two rings. `lineWidth` is in 24-unit viewBox units, like the SVG's.
struct CalendarGlyph: View {
    var size: CGFloat = 20
    var lineWidth: CGFloat = 1.8

    var body: some View {
        Canvas { context, canvas in
            let s = canvas.width / 24
            var path = Path(roundedRect: CGRect(x: 3.5 * s, y: 5 * s, width: 17 * s, height: 15 * s), cornerRadius: 2.5 * s)
            path.move(to: CGPoint(x: 3.5 * s, y: 10 * s)); path.addLine(to: CGPoint(x: 20.5 * s, y: 10 * s))
            path.move(to: CGPoint(x: 8 * s, y: 3 * s)); path.addLine(to: CGPoint(x: 8 * s, y: 7 * s))
            path.move(to: CGPoint(x: 16 * s, y: 3 * s)); path.addLine(to: CGPoint(x: 16 * s, y: 7 * s))
            context.stroke(path, with: .foreground, style: StrokeStyle(lineWidth: lineWidth * s, lineCap: .round, lineJoin: .round))
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}

/// The drag handle: two horizontal lines, 14pt wide, 6pt apart, 2.4pt stroke, round caps.
struct DragHandleGlyph: View {
    var width: CGFloat = 14
    var gap: CGFloat = 6
    var lineWidth: CGFloat = 2.4

    var body: some View {
        Path { p in
            p.move(to: CGPoint(x: 0, y: 0)); p.addLine(to: CGPoint(x: width, y: 0))
            p.move(to: CGPoint(x: 0, y: gap)); p.addLine(to: CGPoint(x: width, y: gap))
        }
        .stroke(style: StrokeStyle(lineWidth: lineWidth, lineCap: .round))
        .frame(width: width, height: gap)
        .accessibilityHidden(true)
    }
}

/// A tinted info box with an icon (Setup age note, calendar note, When summary).
struct InfoBox<Content: View>: View {
    var systemImage: String?
    var fill: Color = Theme.sageTint
    var iconColor: Color = Theme.sageInk
    var radius: CGFloat = 14
    var padding = EdgeInsets(top: 12, leading: 14, bottom: 12, trailing: 14)
    @ViewBuilder var content: Content

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            if let systemImage {
                Image(systemName: systemImage)
                    .font(.system(size: 15, weight: .medium))
                    .foregroundStyle(iconColor)
                    .frame(width: 18)
                    .padding(.top, 1)
            }
            content
            Spacer(minLength: 0)
        }
        .padding(padding)
        .background(fill, in: RoundedRectangle(cornerRadius: radius, style: .continuous))
    }
}

/// 56pt rounded-16 tile with a tinted background and a sageInk icon (Forgot password steps).
struct IconTile: View {
    let systemImage: String
    var size: CGFloat = 56
    var radius: CGFloat = 16
    var fill: Color = Theme.sageTint
    var foreground: Color = Theme.sageInk
    var iconSize: CGFloat = 26

    var body: some View {
        Image(systemName: systemImage)
            .font(.system(size: iconSize, weight: .regular))
            .foregroundStyle(foreground)
            .frame(width: size, height: size)
            .background(fill, in: RoundedRectangle(cornerRadius: radius, style: .continuous))
            .accessibilityHidden(true)
    }
}
