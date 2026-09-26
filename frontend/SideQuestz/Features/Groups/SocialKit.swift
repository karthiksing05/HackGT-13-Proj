import SwiftUI
import UIKit

// Small helpers shared by the Forum and Groups features (prefixed `Social…` to stay out of other
// features' way).

// MARK: - CSS-style text

private struct SocialTextModifier: ViewModifier {
    let size: CGFloat
    let weight: Font.Weight
    let lineHeight: CGFloat

    func body(content: Content) -> some View {
        // The prototype sets `line-height: 1.35` on everything; SF's natural line is ≈1.19×, so we
        // add the difference as line spacing plus half of it above and below (a CSS line box).
        let natural = UIFont.systemFont(ofSize: size).lineHeight
        let extra = max(0, lineHeight * size - natural)
        content
            .sqFont(size, weight)
            .lineSpacing(extra)
            .padding(.vertical, extra / 2)
    }
}

extension View {
    /// SF Pro at `size`/`weight` with the prototype's CSS line box (`line-height` × size, default 1.35).
    func socialText(_ size: CGFloat, _ weight: Font.Weight = .regular, lineHeight: CGFloat = 1.35) -> some View {
        modifier(SocialTextModifier(size: size, weight: weight, lineHeight: lineHeight))
    }
}

// MARK: - Labels

/// Small non-interactive chip: `padding: 3px 8px; border-radius: 8px; 12px Semibold`
/// (Forum "Open plan" / "Free now", Groups chips, Splits share chips).
struct SocialTag: View {
    let text: String
    var fill: Color = Theme.cream
    var foreground: Color = Theme.text2

    var body: some View {
        Text(text)
            .socialText(12, .semibold)
            .foregroundStyle(foreground)
            .lineLimit(1)
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .background(fill, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
    }
}

// MARK: - Chip styles

extension ChipStyle {
    // The prototype's chips add a 1pt CSS border outside their padding; `SQChip` strokes its border
    // inside, so these variants carry 1pt more padding to land on the prototype's widths.

    /// Forum All / Open plans / Free now.
    static let socialDark: ChipStyle = {
        var style = ChipStyle.dark
        style.horizontalPadding += 1
        return style
    }()

    /// Four-column grid chips (Forum When / Distance / Cost). The column sets the width; a small inset
    /// keeps long labels like "Weekend" at full size (CSS lets them run into the padding).
    static let socialGrid: ChipStyle = {
        var style = ChipStyle.grid
        style.horizontalPadding = 4
        return style
    }()

    /// Grid chips that wrap instead of stretching (Forum interests, Add expense › Paid by).
    static let socialCompactGrid: ChipStyle = {
        var style = ChipStyle.grid
        style.fullWidth = false
        style.horizontalPadding += 1
        return style
    }()

    /// Soft toggles with a check (Add expense › Split between).
    static let socialSoft: ChipStyle = {
        var style = ChipStyle.soft
        style.horizontalPadding += 1
        return style
    }()
}

// MARK: - Glyphs

/// Line icons drawn from the prototype's 24-unit SVG paths, so strokes and proportions match.
enum SocialGlyphKind {
    case pin, chevronDown, chevronLeft, sliders, lock, arrowUp, check
}

private struct SocialGlyphShape: Shape {
    let kind: SocialGlyphKind

    func path(in rect: CGRect) -> Path {
        let s = min(rect.width, rect.height) / 24
        func p(_ x: CGFloat, _ y: CGFloat) -> CGPoint { CGPoint(x: rect.minX + x * s, y: rect.minY + y * s) }
        var path = Path()
        switch kind {
        case .pin:
            // M12 21s-6.5-6.2-6.5-11.2a6.5 6.5 0 0 1 13 0C18.5 14.8 12 21 12 21z + circle r2.3
            path.move(to: p(12, 21))
            path.addCurve(to: p(5.5, 9.8), control1: p(12, 21), control2: p(5.5, 14.8))
            path.addRelativeArc(center: p(12, 9.8), radius: 6.5 * s, startAngle: .degrees(180), delta: .degrees(180))
            path.addCurve(to: p(12, 21), control1: p(18.5, 14.8), control2: p(12, 21))
            path.closeSubpath()
            path.addEllipse(in: CGRect(x: rect.minX + 9.7 * s, y: rect.minY + 7.5 * s, width: 4.6 * s, height: 4.6 * s))
        case .chevronDown:
            path.move(to: p(6, 9)); path.addLine(to: p(12, 15)); path.addLine(to: p(18, 9))
        case .chevronLeft:
            path.move(to: p(15, 5)); path.addLine(to: p(8, 12)); path.addLine(to: p(15, 19))
        case .sliders:
            // M4 7h10M18 7h2M4 17h4M12 17h8 + circles (16,7) and (10,17), r2
            path.move(to: p(4, 7)); path.addLine(to: p(14, 7))
            path.move(to: p(18, 7)); path.addLine(to: p(20, 7))
            path.move(to: p(4, 17)); path.addLine(to: p(8, 17))
            path.move(to: p(12, 17)); path.addLine(to: p(20, 17))
            path.addEllipse(in: CGRect(x: rect.minX + 14 * s, y: rect.minY + 5 * s, width: 4 * s, height: 4 * s))
            path.addEllipse(in: CGRect(x: rect.minX + 8 * s, y: rect.minY + 15 * s, width: 4 * s, height: 4 * s))
        case .lock:
            path.addRoundedRect(in: CGRect(x: rect.minX + 5 * s, y: rect.minY + 10.5 * s, width: 14 * s, height: 10 * s),
                                cornerSize: CGSize(width: 2 * s, height: 2 * s))
            path.move(to: p(8.5, 10.5)); path.addLine(to: p(8.5, 7.5))
            path.addRelativeArc(center: p(12, 7.5), radius: 3.5 * s, startAngle: .degrees(180), delta: .degrees(180))
            path.addLine(to: p(15.5, 10.5))
        case .arrowUp:
            path.move(to: p(12, 19)); path.addLine(to: p(12, 5))
            path.move(to: p(5, 12)); path.addLine(to: p(12, 5)); path.addLine(to: p(19, 12))
        case .check:
            path.move(to: p(5, 12.5)); path.addLine(to: p(10, 17.5)); path.addLine(to: p(19, 7))
        }
        return path
    }
}

/// A prototype icon at `size` points with the SVG's `stroke-width` (in 24-unit viewBox terms).
struct SocialGlyph: View {
    let kind: SocialGlyphKind
    var size: CGFloat = 16
    /// The SVG `stroke-width` (scaled by size / 24).
    var lineWidth: CGFloat = 2

    var body: some View {
        SocialGlyphShape(kind: kind)
            .stroke(style: StrokeStyle(lineWidth: lineWidth * size / 24, lineCap: .round, lineJoin: .round))
            .frame(width: size, height: size)
            .accessibilityHidden(true)
    }
}

// MARK: - Motion

enum SocialMotion {
    /// For items that were on screen from the start: nothing on insert, a fade when removed.
    /// (A scale transition left on a view shifts its text by a pixel even at rest, so resting
    /// screens keep only this one.)
    static let settled = AnyTransition.asymmetric(insertion: .identity, removal: .opacity)

    /// The kit's transition, or a plain fade with Reduce Motion (what `.sqTransition` does), for
    /// call sites that pick between it and `settled`.
    static func transition(_ kind: SQTransition, reduceMotion: Bool) -> AnyTransition {
        reduceMotion ? .opacity : kind.transition
    }
}

extension View {
    /// List items fade and rise in as they appear: one after another on the load that replaces the
    /// skeleton (`staggered`), right away when they show up later (a filter change, a refresh).
    /// Removed items fade out. The item's `index` only matters when it first appears.
    func socialArrival(_ index: Int, staggered: Bool) -> some View {
        sqAppear(staggered ? index : 0)
            .transition(SocialMotion.settled)
    }
}

/// `.sqAppear(index)` for items of a first load only, decided once when the item is first built:
/// later items keep their own insertion transition (chat bubbles, uploaded tiles), and an item
/// rebuilt later (a lazy grid scrolling) doesn't replay the stagger.
struct SocialFirstArrival: ViewModifier {
    let index: Int
    @State private var staggered: Bool

    init(index: Int, staggered: Bool) {
        self.index = index
        _staggered = State(initialValue: staggered)
    }

    func body(content: Content) -> some View {
        if staggered {
            content.sqAppear(index)
        } else {
            content
        }
    }
}

/// A button label that keeps its size while the button's request runs: the label fades out and
/// `LoadingDots` in the label's color take its place. Give the button an explicit
/// `accessibilityLabel` (and `accessibilityValue("Loading")` while busy).
struct SocialBusyLabel<Label: View>: View {
    let isBusy: Bool
    /// The button's text color.
    var color: Color
    var dotSize: CGFloat = 6
    @ViewBuilder var label: Label

    var body: some View {
        label
            .opacity(isBusy ? 0 : 1)
            .overlay {
                if isBusy {
                    LoadingDots(color: color, dotSize: dotSize)
                        .accessibilityHidden(true)
                        .transition(.opacity)
                }
            }
            .animation(Motion.quick, value: isBusy)
    }
}

// MARK: - People

extension PersonRef {
    /// "You" for the signed-in user, otherwise the first name ("Maya").
    func socialName(meId: String?) -> String {
        id == meId ? "You" : firstName
    }
}

// MARK: - Errors

extension Error {
    /// The short sentence we show for a failed call.
    var socialMessage: String {
        (self as? LocalizedError)?.errorDescription ?? "Something went wrong."
    }
}
