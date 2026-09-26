import SwiftUI

// MARK: - Curves

/// The app's motion vocabulary. Every animation uses one of these curves so timing feels the same
/// everywhere; with Reduce Motion on, movement collapses to short cross-fades.
enum Motion {
    /// Taps, toggles, chips, selection changes.
    static let quick = Animation.snappy(duration: 0.22)
    /// Content swaps: loading → loaded, tab and segment changes, text that updates.
    static let standard = Animation.smooth(duration: 0.32)
    /// Things arriving: list rows, chat bubbles, cards, banners.
    static let arrive = Animation.spring(duration: 0.42, bounce: 0.22)
    /// Bigger moves: step changes, panels.
    static let gentle = Animation.smooth(duration: 0.45)
    /// Stand-in for any of the above when Reduce Motion is on.
    static let reduced = Animation.easeInOut(duration: 0.2)

    /// Delay for the `index`th item of a list that animates in one after another.
    static func stagger(_ index: Int, step: Double = 0.045, cap: Double = 0.3) -> Double {
        min(Double(max(index, 0)) * step, cap)
    }
}

/// `withAnimation` that respects Reduce Motion (movement becomes a short fade).
@discardableResult
func withMotion<Result>(_ animation: Animation = Motion.standard, _ body: () throws -> Result) rethrows -> Result {
    try withAnimation(UIAccessibility.isReduceMotionEnabled ? Motion.reduced : animation, body)
}

// MARK: - Transitions

/// Insert/remove transitions. Apply with `.sqTransition(_:)` so Reduce Motion gets a plain fade.
enum SQTransition {
    /// Fade in while rising 10pt (cards, rows, messages, errors).
    case rise
    /// Fade + scale from 92% (badges, chips, small confirmations).
    case pop
    /// Slide down from the top edge (success banners).
    case banner
    /// Slide up from the bottom edge (action bars, toasts).
    case slideUp
    /// A chat bubble growing out of its tail corner.
    case bubble(mine: Bool)
    /// Step flows: the new step comes from the side you're heading to.
    case step(forward: Bool)

    var transition: AnyTransition {
        switch self {
        case .rise:
            .opacity.combined(with: .offset(y: 10))
        case .pop:
            .opacity.combined(with: .scale(scale: 0.92))
        case .banner:
            .move(edge: .top).combined(with: .opacity)
        case .slideUp:
            .move(edge: .bottom).combined(with: .opacity)
        case .bubble(let mine):
            .asymmetric(insertion: .opacity
                            .combined(with: .scale(scale: 0.86, anchor: mine ? .bottomTrailing : .bottomLeading))
                            .combined(with: .offset(y: 6)),
                        removal: .opacity)
        case .step(let forward):
            .asymmetric(insertion: .move(edge: forward ? .trailing : .leading).combined(with: .opacity),
                        removal: .move(edge: forward ? .leading : .trailing).combined(with: .opacity))
        }
    }
}

private struct SQTransitionModifier: ViewModifier {
    let kind: SQTransition
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func body(content: Content) -> some View {
        content.transition(reduceMotion ? .opacity : kind.transition)
    }
}

extension View {
    /// Transition for a view that's inserted or removed inside `withMotion` / `withAnimation`.
    func sqTransition(_ kind: SQTransition) -> some View {
        modifier(SQTransitionModifier(kind: kind))
    }
}

// MARK: - Appear

private struct AppearModifier: ViewModifier {
    let index: Int
    let offset: CGFloat
    @State private var shown = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func body(content: Content) -> some View {
        content
            .opacity(shown ? 1 : 0)
            .offset(y: shown || reduceMotion ? 0 : offset)
            .onAppear {
                guard !shown else { return }
                withAnimation((reduceMotion ? Motion.reduced : Motion.arrive).delay(Motion.stagger(index))) { shown = true }
            }
    }
}

extension View {
    /// Fades and rises into place the first time it appears. Give list items their `index` so they
    /// arrive one after another.
    func sqAppear(_ index: Int = 0, offset: CGFloat = 10) -> some View {
        modifier(AppearModifier(index: index, offset: offset))
    }
}

// MARK: - Feedback

private struct BounceModifier: ViewModifier {
    let isOn: Bool
    let scale: CGFloat
    @State private var count = 0
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func body(content: Content) -> some View {
        content
            .keyframeAnimator(initialValue: CGFloat(1), trigger: count) { view, value in
                view.scaleEffect(value)
            } keyframes: { _ in
                SpringKeyframe(scale, duration: 0.12, spring: .snappy)
                SpringKeyframe(1, duration: 0.3, spring: .bouncy)
            }
            .onChange(of: isOn) { _, on in
                if on && !reduceMotion { count += 1 }
            }
    }
}

extension View {
    /// A quick springy pop each time `isOn` becomes true (a selected tab, a liked item, a star).
    func sqBounce(when isOn: Bool, scale: CGFloat = 1.15) -> some View {
        modifier(BounceModifier(isOn: isOn, scale: scale))
    }

    /// Numbers roll to their new value (money, times, counts) when they change inside an animation.
    func sqNumeric() -> some View {
        contentTransition(.numericText())
    }
}

// MARK: - Shimmer

private struct ShimmerModifier: ViewModifier {
    let active: Bool
    @State private var start = Date()
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    // The overlay is always there (only its inside changes), so turning the shimmer on and off
    // keeps the content's identity and its own animations (e.g. a number rolling) intact.
    func body(content: Content) -> some View {
        content
            .overlay {
                if active && !reduceMotion {
                    TimelineView(.animation(minimumInterval: 1.0 / 30)) { timeline in
                        let cycle = 1.4
                        let t = timeline.date.timeIntervalSince(start).truncatingRemainder(dividingBy: cycle) / cycle
                        GeometryReader { proxy in
                            let width = proxy.size.width
                            LinearGradient(colors: [.white.opacity(0), .white.opacity(0.7), .white.opacity(0)],
                                           startPoint: .leading, endPoint: .trailing)
                                .frame(width: width * 0.45)
                                .offset(x: -width * 0.45 + CGFloat(t) * width * 1.45)
                        }
                    }
                    .mask(content)
                    .allowsHitTesting(false)
                }
            }
    }
}

extension View {
    /// A soft highlight sweeping across loading placeholders.
    func sqShimmer(active: Bool = true) -> some View {
        modifier(ShimmerModifier(active: active))
    }
}

// MARK: - Indicators

/// Three dots rising in turn: the loading indicator inside buttons, "Sending…" and typing.
struct LoadingDots: View {
    var color: Color = Theme.ink
    var dotSize: CGFloat = 6
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        TimelineView(.animation(minimumInterval: 1.0 / 30)) { timeline in
            let t = timeline.date.timeIntervalSinceReferenceDate
            HStack(spacing: dotSize * 0.65) {
                ForEach(0..<3, id: \.self) { i in
                    let wave = sin(t * 2 * .pi / 1.1 - Double(i) * 0.9)
                    let lift = max(0, wave)
                    Circle()
                        .fill(color)
                        .frame(width: dotSize, height: dotSize)
                        .opacity(0.35 + 0.65 * (reduceMotion ? (wave + 1) / 2 : lift))
                        .offset(y: reduceMotion ? 0 : -dotSize * 0.55 * lift)
                }
            }
            .frame(height: dotSize * 1.8, alignment: .bottom)
        }
        .accessibilityElement()
        .accessibilityLabel("Loading")
    }
}

/// The prototype's check, drawing itself in (confirmations, "Sent", "Connected").
struct AnimatedCheck: View {
    /// SVG stroke width in the 24-unit viewBox.
    var lineWidth: CGFloat = 2.6
    var delay: Double = 0.05
    @State private var progress: CGFloat = 0
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        GeometryReader { proxy in
            let s = min(proxy.size.width, proxy.size.height) / 24
            CheckShape()
                .trim(from: 0, to: reduceMotion ? 1 : progress)
                .stroke(style: StrokeStyle(lineWidth: lineWidth * s, lineCap: .round, lineJoin: .round))
        }
        .onAppear {
            guard !reduceMotion else { return }
            withAnimation(.easeOut(duration: 0.35).delay(delay)) { progress = 1 }
        }
        .accessibilityHidden(true)
    }
}

/// Rotates through short status lines with a soft cross-fade while a long request runs
/// ("Finding spots near you…", "Timing the transit…"). The request decides how long it shows.
struct CyclingStatusText: View {
    let lines: [String]
    var interval: Duration = .seconds(1.8)
    @State private var index = 0
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        ZStack {
            if !lines.isEmpty {
                Text(lines[index % lines.count])
                    .id(index)
                    .transition(reduceMotion ? .opacity : .opacity.combined(with: .offset(y: 8)))
            }
        }
        .task(id: lines) {
            index = 0
            guard lines.count > 1 else { return }
            while !Task.isCancelled {
                try? await Task.sleep(for: interval)
                if Task.isCancelled { break }
                withMotion { index += 1 }
            }
        }
        .accessibilityAddTraits(.updatesFrequently)
    }
}

// MARK: - Refreshing

private struct RefreshingModifier: ViewModifier {
    let isRefreshing: Bool

    func body(content: Content) -> some View {
        content
            .opacity(isRefreshing ? 0.5 : 1)
            .overlay(alignment: .top) {
                if isRefreshing {
                    LoadingDots(color: Theme.sageInk)
                        .padding(.horizontal, 14)
                        .padding(.vertical, 9)
                        .background(.white, in: Capsule())
                        .shadow(color: .black.opacity(0.1), radius: 8, y: 2)
                        .padding(.top, 8)
                        .sqTransition(.pop)
                }
            }
            .animation(Motion.standard, value: isRefreshing)
    }
}

extension View {
    /// Keeps content on screen while it reloads (a filter change, a refresh after an action): dims it
    /// and floats a small loading pill on top, instead of flashing back to a skeleton.
    func sqRefreshing(_ isRefreshing: Bool) -> some View {
        modifier(RefreshingModifier(isRefreshing: isRefreshing))
    }
}
