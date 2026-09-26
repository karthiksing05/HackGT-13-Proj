import SwiftUI

// MARK: - Loading container

/// Home's loading / error / content switch. While the first load runs it shows `skeleton` (the
/// section's own shape; the logo loader over `slowLines` takes its place after `slowAfter`, see
/// `SlowLoading`), then cross-fades to the content. The content is stacked leading with no
/// spacing, exactly as it sits in its parent, so a multi-view section keeps its layout.
struct HomeLoadable<Value, Skeleton: View, Content: View>: View {
    let state: Loadable<Value>
    var minHeight: CGFloat = 160
    var slowAfter: Duration = SlowLoading.threshold
    var slowLines: [String] = []
    var slowLogoSize: CGFloat = 40
    let retry: () -> Void
    @ViewBuilder var skeleton: () -> Skeleton
    @ViewBuilder var content: (Value) -> Content
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        ZStack(alignment: .topLeading) {
            switch state {
            case .loading:
                skeleton()
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .sqSlowLoading(after: slowAfter, lines: slowLines, logoSize: slowLogoSize)
                    .transition(.opacity)
            case .failed(let message):
                ErrorStateView(message: message, minHeight: minHeight, retry: retry)
                    .transition(.opacity)
            case .loaded(let value):
                VStack(alignment: .leading, spacing: 0) { content(value) }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .transition(.opacity)
            }
        }
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: state.phase)
    }
}

// MARK: - First-load arrival

/// When a section's items may stagger in: from the start of its first load until a moment after
/// the content lands. Items that show up later (coming back to the segment, a reload) don't.
struct HomeArrivalWindow {
    private var until = Date.distantPast

    var isOpen: Bool { Date.now < until }

    /// The section appeared: its items will stagger in if its data is still on the way. (Called
    /// from `onAppear`, so the parent's body never reads the data just to set this up.)
    mutating func begin(loading: Bool) {
        if loading { until = .distantFuture }
    }

    mutating func update(_ phase: LoadablePhase) {
        switch phase {
        case .loading: until = .distantFuture
        case .loaded: if until == .distantFuture { until = Date.now.addingTimeInterval(1) }
        case .failed: break
        }
    }
}

/// While `enabled` (the section is replacing its skeleton), the view fades and rises into place the
/// first time it appears, `index` steps after the first item. Views that appear later (coming back
/// to a segment, a reload) are simply there; inserted items use their own transition instead.
private struct HomeArrivalModifier: ViewModifier {
    let index: Int
    let enabled: Bool
    /// nil until the first appear decides whether this view arrives.
    @State private var hidden: Bool?
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func body(content: Content) -> some View {
        let isHidden = hidden ?? enabled
        content
            .opacity(isHidden ? 0 : 1)
            .offset(y: isHidden && !reduceMotion ? 10 : 0)
            .onAppear {
                guard hidden == nil else { return }
                guard enabled else {
                    hidden = false
                    return
                }
                withAnimation((reduceMotion ? Motion.reduced : Motion.arrive).delay(Motion.stagger(index))) { hidden = false }
            }
    }
}

/// Springs up from 40% the first time it appears (a confirmation badge).
private struct HomePopInModifier: ViewModifier {
    let delay: Double
    @State private var shown = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func body(content: Content) -> some View {
        content
            .scaleEffect(shown || reduceMotion ? 1 : 0.4)
            .opacity(shown ? 1 : 0)
            .onAppear {
                guard !shown else { return }
                withAnimation((reduceMotion ? Motion.reduced : .spring(duration: 0.5, bounce: 0.4)).delay(delay)) { shown = true }
            }
    }
}

extension View {
    /// Staggered first-load arrival (see `HomeArrivalModifier`).
    func homeArrival(_ index: Int, enabled: Bool) -> some View {
        modifier(HomeArrivalModifier(index: index, enabled: enabled))
    }

    /// Springs up from 40% when it first appears.
    func homePopIn(delay: Double = 0) -> some View {
        modifier(HomePopInModifier(delay: delay))
    }

    /// Insert/remove transition that respects Reduce Motion, for transitions the kit doesn't name.
    func homeTransition(_ transition: AnyTransition) -> some View {
        modifier(HomeTransitionModifier(transition: transition))
    }
}

private struct HomeTransitionModifier: ViewModifier {
    let transition: AnyTransition
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func body(content: Content) -> some View {
        content.transition(reduceMotion ? .opacity : transition)
    }
}

extension AnyTransition {
    /// A card dropping into place from just above (the "N past events to rate" card).
    static var homeDrop: AnyTransition {
        .asymmetric(insertion: .opacity.combined(with: .offset(y: -14)).combined(with: .scale(scale: 0.97, anchor: .top)),
                    removal: .opacity.combined(with: .scale(scale: 0.97, anchor: .top)))
    }

    /// A header swapping to the next page: the new text slides in from the side you're heading to.
    static func homeSlide(forward: Bool) -> AnyTransition {
        .asymmetric(insertion: .opacity.combined(with: .offset(x: forward ? 18 : -18)),
                    removal: .opacity.combined(with: .offset(x: forward ? -10 : 10)))
    }
}

// MARK: - Skeleton text lines

/// A placeholder bar exactly as tall as one line of `size`-point text in Home's CSS line box, so
/// a skeleton has the same rhythm as the text that replaces it. `width` nil = the full width.
struct HomeSkeletonLine: View {
    let size: CGFloat
    var multiplier: CGFloat = 1.35
    var width: CGFloat? = nil
    var color: Color = Theme.skeleton

    var body: some View {
        Text(verbatim: "A")
            .sqFont(size)
            .homeLine(size, multiplier)
            .hidden()
            .frame(width: width)
            .frame(maxWidth: width == nil ? .infinity : nil, alignment: .leading)
            .overlay {
                SkeletonBlock(width: width, height: max(6, (size * 0.72).rounded()), radius: max(3, size * 0.3), color: color)
            }
            .accessibilityHidden(true)
    }
}

// MARK: - Save status

/// Where an autosave stands: nothing to show, a request running, or just saved.
enum HomeSaveState: Equatable {
    case idle, saving, saved
}

/// "Saving…" while the request runs, then a check drawing in + "Saved". Empty when idle.
struct HomeSaveStatusLabel: View {
    let state: HomeSaveState
    var size: CGFloat = 11

    var body: some View {
        ZStack(alignment: .leading) {
            switch state {
            case .idle:
                EmptyView()
            case .saving:
                Text("Saving…")
                    .foregroundStyle(Theme.text3)
                    .transition(.opacity)
            case .saved:
                HStack(spacing: 4) {
                    AnimatedCheck(lineWidth: 3).frame(width: size, height: size)
                    Text("Saved")
                }
                .foregroundStyle(Theme.sageInk)
                .transition(.opacity)
            }
        }
        .sqFont(size, .semibold, relativeTo: .caption2)
        .lineLimit(1)
        .fixedSize()
        .animation(Motion.standard, value: state)
        .accessibilityElement(children: .combine)
    }
}

// MARK: - Rated stars

/// A Past row's stars (14pt, drawn exactly like `StarRow`). When the rating was just saved the lit
/// stars fill in one after another with a small bounce; editing the rating fills them again.
struct HomeRatedStars: View {
    let rating: Int
    var size: CGFloat = 14
    var spacing: CGFloat = 1
    /// Fill in when first shown (the Rate pill just turned into these stars).
    var fillsIn = false
    @State private var runs = 0
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    /// Each lit star starts `step` after the previous one and takes `each` to settle.
    private static let step = 0.08
    private static let each = 0.42

    var body: some View {
        let lit = min(max(rating, 0), 5)
        let total = Self.each + Self.step * Double(max(lit - 1, 0))
        KeyframeAnimator(initialValue: fillsIn ? 0.0 : 1.0, trigger: runs) { t in
            HStack(spacing: spacing) {
                ForEach(1...5, id: \.self) { n in
                    star(n, lit: n <= lit, progress: progress(n, t: t, total: total))
                }
            }
        } keyframes: { _ in
            MoveKeyframe(0.0)
            LinearKeyframe(1.0, duration: total)
        }
        .onAppear { if fillsIn { runs += 1 } }
        .onChange(of: rating) { _, _ in runs += 1 }
        .accessibilityElement()
        .accessibilityLabel("\(rating) of 5 stars")
    }

    /// 0…1 for star `n` at overall time `t` (0…1 of `total`).
    private func progress(_ n: Int, t: Double, total: Double) -> Double {
        let start = Self.step * Double(n - 1)
        return min(max((t * total - start) / Self.each, 0), 1)
    }

    @ViewBuilder
    private func star(_ n: Int, lit: Bool, progress p: Double) -> some View {
        let stroke = StrokeStyle(lineWidth: 1.8 * size / 24, lineJoin: .round)
        if lit {
            let fill = min(1, p * 3)
            ZStack {
                StarShape().stroke(Theme.mutedStar, style: stroke).opacity(1 - fill)
                ZStack {
                    StarShape().fill(Theme.sage)
                    StarShape().stroke(Theme.sage, style: stroke)
                }
                .opacity(fill)
                .scaleEffect(reduceMotion ? 1 : Self.bounce(p))
            }
            .frame(width: size, height: size)
        } else {
            StarShape().stroke(Theme.mutedStar, style: stroke).frame(width: size, height: size)
        }
    }

    /// Grows from 40% past full size and settles, like a damped spring (1 at the end).
    private static func bounce(_ p: Double) -> CGFloat {
        guard p < 1 else { return 1 }
        return CGFloat(1 - 0.6 * exp(-4 * p) * cos(8 * p))
    }
}
