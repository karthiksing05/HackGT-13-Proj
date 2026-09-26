import SwiftUI

/// Opening animation on every cold launch (GUI_PLAN.md §6, Brand frames 01–04, `Splash` artboard),
/// played 1.3× faster than the prototype's timings:
/// 0.08s rise + spin · 1.25s the road fades in while the S draws from the top-right down to the
/// pin · 2.0s the pin and quest marker pop in, the wordmark fades up · 3.5s `onFinish` (so about
/// 2.7s in all). Reduce Motion: the finished logo fades in, holds, then `onFinish`.
struct SplashView: View {
    let onFinish: () -> Void

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    // 01 · Rise + 02 · Spin
    @State private var risen = false
    @State private var tileOpacity = 0.0
    // 03 · Draw
    @State private var roadOpacity = 0.0
    @State private var drawProgress: CGFloat = 0
    // 04 · Reveal
    @State private var pinOpacity = 0.0
    @State private var markerScale: CGFloat = 0.0001
    @State private var wordOpacity = 0.0
    @State private var wordLifted = false
    @State private var finished = false

    var body: some View {
        VStack(spacing: 22) {
            LogoMark(size: 132, drawProgress: drawProgress, roadOpacity: roadOpacity,
                     pinOpacity: pinOpacity, markerScale: markerScale)
                .scaleEffect(risen ? 1 : 0.55)
                .rotationEffect(.degrees(risen ? 0 : -540))
                .offset(y: risen ? 0 : 560)
                .opacity(tileOpacity)

            VStack(spacing: 8) {
                // CSS line-height: 1 → a 40pt box.
                Wordmark(size: 40)
                    .authLineHeight(1, size: 40, mono: true)
                Text("Turn waiting into wandering.")
                    .sqFont(15)
                    .foregroundStyle(Theme.text3)
                    .authLineHeight(1.35, size: 15)
            }
            .opacity(wordOpacity)
            .offset(y: wordLifted ? 0 : 14)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .ignoresSafeArea()
        .background(Theme.cream.ignoresSafeArea())
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("SideQuests. Turn waiting into wandering.")
        .task { await play() }
    }

    // MARK: Timeline

    private func play() async {
        let start = ContinuousClock.now
        func wait(until seconds: Double) async -> Bool {
            do {
                try await Task.sleep(until: start + .milliseconds(Int(Self.t(seconds) * 1000)), clock: .continuous)
                return true
            } catch {
                return false
            }
        }

        if reduceMotion {
            risen = true
            roadOpacity = 1
            drawProgress = 1
            pinOpacity = 1
            markerScale = 1
            wordLifted = true
            withAnimation(.easeOut(duration: Self.t(0.3))) {
                tileOpacity = 1
                wordOpacity = 1
            }
            guard await wait(until: 1.8) else { return }
            finish()
            return
        }

        guard await wait(until: 0.08) else { return }
        withAnimation(.timingCurve(0.22, 1, 0.36, 1, duration: Self.t(1.1))) { risen = true }
        withAnimation(Self.cssEase(0.25)) { tileOpacity = 1 }

        guard await wait(until: 1.25) else { return }
        withAnimation(Self.cssEase(0.4)) { roadOpacity = 1 }
        withAnimation(.easeInOut(duration: Self.t(0.75))) { drawProgress = 1 }

        guard await wait(until: 2.0) else { return }
        withAnimation(Self.cssEase(0.2)) { pinOpacity = 1 }
        withAnimation(.timingCurve(0.34, 1.56, 0.64, 1, duration: Self.t(0.38))) { markerScale = 1 }
        withAnimation(Self.cssEase(0.45)) {
            wordOpacity = 1
            wordLifted = true
        }

        guard await wait(until: 3.5) else { return }
        finish()
    }

    private func finish() {
        guard !finished else { return }
        finished = true
        onFinish()
    }

    /// The whole intro plays this much faster than the prototype's timings (which the numbers in
    /// `play()` still read as).
    private static let speed = 1.3

    /// A prototype time or duration at `speed`.
    private static func t(_ seconds: Double) -> Double { seconds / speed }

    /// CSS `ease` (cubic-bezier(0.25, 0.1, 0.25, 1)), at `speed`.
    private static func cssEase(_ duration: Double) -> Animation {
        .timingCurve(0.25, 0.1, 0.25, 1, duration: t(duration))
    }
}

#Preview {
    SplashView {}
        .environment(AppEnvironment.preview())
        .environment(Router(phase: .splash))
}
