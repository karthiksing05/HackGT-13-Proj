import SwiftUI

/// Route endpoints drawn like the logo's markers: the start (A) is the logo's diamond quest marker,
/// the end (B) its ring pin. Cream fill with an ink outline, in the logo's proportions (the ring is
/// 4/5 the diamond's size). `onMap` adds a white halo and a soft shadow so they read on map tiles.
struct RouteMarker: View {
    enum Kind { case start, end }
    /// The logo's two markers.
    enum Figure { case diamond, ring }

    let figure: Figure
    /// The diamond's full size; the ring is 80% of it.
    var size: CGFloat = 22
    var onMap = false
    private var label: String?

    /// A route endpoint: the start is the diamond, the end the ring.
    init(kind: Kind, size: CGFloat = 22, onMap: Bool = false) {
        self.figure = kind == .start ? .diamond : .ring
        self.size = size
        self.onMap = onMap
        self.label = kind == .start ? "Start" : "End"
    }

    /// Just the figure, e.g. the diamond as a sidequest's icon.
    init(figure: Figure, size: CGFloat = 22, onMap: Bool = false) {
        self.figure = figure
        self.size = size
        self.onMap = onMap
    }

    var body: some View {
        let line = max(2, size * 0.16)
        ZStack {
            if onMap { halo }
            switch figure {
            case .ring:
                Circle()
                    .fill(Theme.cream)
                    .overlay(Circle().strokeBorder(Theme.ink, lineWidth: line))
                    .frame(width: size * 0.8, height: size * 0.8)
            case .diamond:
                RouteDiamondShape(inset: line / 2)
                    .fill(Theme.cream)
                    .overlay(RouteDiamondShape(inset: line / 2).stroke(Theme.ink, style: StrokeStyle(lineWidth: line, lineJoin: .round)))
                    .frame(width: size, height: size)
            }
        }
        .frame(width: size, height: size)
        .accessibilityElement()
        .accessibilityLabel(label ?? "")
        .accessibilityHidden(label == nil)
    }

    /// A white outline around the marker and a shadow, for map backgrounds.
    @ViewBuilder private var halo: some View {
        let pad = max(3, size * 0.12)
        Group {
            switch figure {
            case .ring:
                Circle().fill(.white).frame(width: size * 0.8 + pad * 2, height: size * 0.8 + pad * 2)
            case .diamond:
                RouteDiamondShape(inset: 0).fill(.white).frame(width: size + pad * 2.4, height: size + pad * 2.4)
            }
        }
        .shadow(color: .black.opacity(0.22), radius: 3, y: 1.5)
    }
}

/// The logo's quest-marker diamond in a square frame, inset so a centered stroke stays inside.
struct RouteDiamondShape: Shape {
    var inset: CGFloat = 0

    func path(in rect: CGRect) -> Path {
        let r = rect.insetBy(dx: inset, dy: inset)
        var p = Path()
        p.move(to: CGPoint(x: r.midX, y: r.minY))
        p.addLine(to: CGPoint(x: r.maxX, y: r.midY))
        p.addLine(to: CGPoint(x: r.midX, y: r.maxY))
        p.addLine(to: CGPoint(x: r.minX, y: r.midY))
        p.closeSubpath()
        return p
    }
}
