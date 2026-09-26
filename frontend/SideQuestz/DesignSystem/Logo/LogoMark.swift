import SwiftUI

/// The SideQuestz mark, drawn in a 64×64 grid and scaled by `size / 64` (never an image).
///
/// The dotted line is the main road — the plan you already had. The S leaves it and comes back:
/// a side quest is a detour worth taking. The S path is authored top → bottom so `drawProgress`
/// draws it the way you write an S, from the top-right down to the pin.
struct LogoMark: View {
    enum Variant { case sage, dark, light }

    var size: CGFloat = 84
    var variant: Variant = .sage
    /// 0…1 — trims the S from the top.
    var drawProgress: CGFloat = 1
    var roadOpacity: Double = 1
    var pinOpacity: Double = 1
    var markerScale: CGFloat = 1
    /// nil = the standard 18/64 corner; 0 for the square app-icon export.
    var cornerRadius: CGFloat? = nil

    var body: some View {
        let s = size / 64
        let colors = palette
        ZStack {
            RoundedRectangle(cornerRadius: cornerRadius ?? 18 * s, style: .continuous)
                .fill(colors.tile)
            LogoRoadShape()
                .stroke(colors.road, style: StrokeStyle(lineWidth: 3 * s, lineCap: .round, dash: [0.1 * s, 7 * s]))
                .opacity(roadOpacity)
            LogoSRouteShape()
                .trim(from: 0, to: drawProgress)
                .stroke(colors.route, style: StrokeStyle(lineWidth: 5 * s, lineCap: .round, lineJoin: .round))
            Circle()
                .fill(colors.markerFill)
                .overlay(Circle().stroke(colors.markerStroke, lineWidth: 3 * s))
                .frame(width: 9 * s, height: 9 * s)
                .position(x: 17 * s, y: 46 * s)
                .opacity(pinOpacity)
            ZStack {
                LogoDiamondShape().fill(colors.markerFill)
                LogoDiamondShape().stroke(colors.markerStroke, style: StrokeStyle(lineWidth: 3 * s, lineJoin: .round))
            }
            .scaleEffect(markerScale, anchor: UnitPoint(x: 46.0 / 64, y: 14.0 / 64))
        }
        .frame(width: size, height: size)
        .accessibilityElement()
        .accessibilityLabel("SideQuestz logo")
    }

    private var palette: (tile: Color, road: Color, route: Color, markerFill: Color, markerStroke: Color) {
        switch variant {
        case .sage: (Theme.sage, Color(hex: 0xB9C7B8), Theme.ink, Theme.cream, Theme.ink)
        case .dark: (Theme.ink, Theme.text2, Theme.sage, Theme.ink, Theme.sage)
        case .light: (.white, Theme.lineStrong, Theme.ink, Theme.cream, Theme.ink)
        }
    }
}

/// Main road: vertical line from (32, 5) to (32, 59) — dotted by the stroke's dash pattern.
struct LogoRoadShape: Shape {
    func path(in rect: CGRect) -> Path {
        let s = rect.width / 64
        var p = Path()
        p.move(to: CGPoint(x: rect.minX + 32 * s, y: rect.minY + 5 * s))
        p.addLine(to: CGPoint(x: rect.minX + 32 * s, y: rect.minY + 59 * s))
        return p
    }
}

/// `M38 14 H29 C22.5 14 19 17.5 19 22 C19 26.5 22.5 30 29 30 H35 C41.5 30 45 33.5 45 38 C45 42.5 41.5 46 35 46 H17`
struct LogoSRouteShape: Shape {
    func path(in rect: CGRect) -> Path {
        let s = rect.width / 64
        func pt(_ x: CGFloat, _ y: CGFloat) -> CGPoint { CGPoint(x: rect.minX + x * s, y: rect.minY + y * s) }
        var p = Path()
        p.move(to: pt(38, 14))
        p.addLine(to: pt(29, 14))
        p.addCurve(to: pt(19, 22), control1: pt(22.5, 14), control2: pt(19, 17.5))
        p.addCurve(to: pt(29, 30), control1: pt(19, 26.5), control2: pt(22.5, 30))
        p.addLine(to: pt(35, 30))
        p.addCurve(to: pt(45, 38), control1: pt(41.5, 30), control2: pt(45, 33.5))
        p.addCurve(to: pt(35, 46), control1: pt(45, 42.5), control2: pt(41.5, 46))
        p.addLine(to: pt(17, 46))
        return p
    }
}

/// Quest marker: diamond (46,8) → (52,14) → (46,20) → (40,14).
struct LogoDiamondShape: Shape {
    func path(in rect: CGRect) -> Path {
        let s = rect.width / 64
        func pt(_ x: CGFloat, _ y: CGFloat) -> CGPoint { CGPoint(x: rect.minX + x * s, y: rect.minY + y * s) }
        var p = Path()
        p.move(to: pt(46, 8))
        p.addLine(to: pt(52, 14))
        p.addLine(to: pt(46, 20))
        p.addLine(to: pt(40, 14))
        p.closeSubpath()
        return p
    }
}

/// Loading indicator: a small mark with the S drawing in on a loop (static under Reduce Motion).
struct LogoLoadingView: View {
    var size: CGFloat = 44
    @State private var progress: CGFloat = 0
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        LogoMark(size: size, drawProgress: reduceMotion ? 1 : progress)
            .onAppear {
                guard !reduceMotion else { return }
                progress = 0
                withAnimation(.easeInOut(duration: 1.1).repeatForever(autoreverses: false)) { progress = 1 }
            }
            .accessibilityLabel("Loading")
    }
}
