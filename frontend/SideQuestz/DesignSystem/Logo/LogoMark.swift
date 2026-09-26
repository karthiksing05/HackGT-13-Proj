import SwiftUI

/// The SideQuests mark, drawn in a 64×64 grid and scaled by `size / 64` (never an image).
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
    /// 0…1 — erases the S from the top (the loader's second half).
    var drawStart: CGFloat = 0
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
                .trim(from: drawStart, to: drawProgress)
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
        .accessibilityLabel("SideQuests logo")
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

/// Loading indicator: a small mark whose S draws from the diamond to the circle, holds a beat, then
/// erases the same way (its tail following the head to the circle), and loops without a jump.
/// Static under Reduce Motion.
struct LogoLoadingView: View {
    var size: CGFloat = 44
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    /// Seconds for one draw + erase.
    static let cycle = 2.0

    var body: some View {
        TimelineView(.animation(minimumInterval: 1.0 / 60, paused: reduceMotion)) { timeline in
            let trim = reduceMotion ? (start: 0, end: 1) : Self.trim(at: timeline.date.timeIntervalSinceReferenceDate)
            LogoMark(size: size, drawProgress: trim.end, drawStart: trim.start)
        }
        .accessibilityElement()
        .accessibilityLabel("Loading")
    }

    /// Where the S is trimmed `time` seconds in: drawing for 45% of a cycle, a 5% hold, erasing for
    /// 45%, a 5% hold with nothing drawn. Each move eases in and out, so the loop never jumps.
    static func trim(at time: TimeInterval) -> (start: CGFloat, end: CGFloat) {
        let phase = time.truncatingRemainder(dividingBy: cycle) / cycle
        switch phase {
        case ..<0.45: return (0, ease(phase / 0.45))
        case ..<0.5: return (0, 1)
        case ..<0.95: return (ease((phase - 0.5) / 0.45), 1)
        default: return (1, 1)
        }
    }

    /// Cubic ease in-out.
    private static func ease(_ x: Double) -> CGFloat {
        let t = min(max(x, 0), 1)
        return CGFloat(t < 0.5 ? 4 * t * t * t : 1 - pow(-2 * t + 2, 3) / 2)
    }
}
