import SwiftUI

/// The loading policy for first loads: a screen's skeleton shows at once, and only a load that is
/// still running after `threshold` gives way to the logo loader (the S drawing on a loop) with a
/// line about what's taking a moment. Reloads never come back here: they keep the content on
/// screen (`sqRefreshing`), and the only way back to a skeleton is a retry after a failed first load.
///
/// `-SQSlowLoadingAfter <seconds>` at launch moves the threshold: `0` shows the S at once, a very
/// large value never does.
enum SlowLoading {
    static let defaultThreshold: Duration = .seconds(2)
    /// Longer than any session: the S never shows. `parse` caps larger values here.
    static let never: Duration = .seconds(neverSeconds)
    /// The app's threshold: `-SQSlowLoadingAfter`, or the default.
    static let threshold: Duration = parse(UserDefaults.standard.string(forKey: "SQSlowLoadingAfter"))

    private static let neverSeconds = 366.0 * 86_400

    /// Seconds as text ("2", "0.5", "0") → the threshold. Nothing, or anything that isn't a
    /// non-negative number, means the default; anything past `never` is `never`.
    static func parse(_ text: String?) -> Duration {
        guard let text, let seconds = Double(text.trimmingCharacters(in: .whitespacesAndNewlines)),
              seconds.isFinite, seconds >= 0 else { return defaultThreshold }
        return .seconds(min(seconds, neverSeconds))
    }

    /// The one rule: the S shows only while a load with nothing to show yet has run for `threshold`.
    static func showsLogo(isLoading: Bool, hasContent: Bool, elapsed: Duration, threshold: Duration = defaultThreshold) -> Bool {
        isLoading && !hasContent && elapsed >= threshold
    }
}

/// Wraps a skeleton: it keeps its height, and once `isLoading` has been true for `after` it
/// cross-fades to `LoadingStateView(lines:logoSize:)` (the S over the lines, centered in the same
/// frame). Turning `isLoading` off, or the skeleton going away, resets the wait.
struct SlowLoadingModifier: ViewModifier {
    let isLoading: Bool
    var after: Duration = SlowLoading.threshold
    var lines: [String] = []
    var logoSize: CGFloat = 40
    /// How long the current load has been running, measured once the wait is over (zero before).
    @State private var elapsed: Duration = .zero
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    /// The S is centered in the skeleton, or in this much of its top when it's taller (a screen's
    /// worth of skeleton runs under the tab bar or off screen).
    private static let centeredWithin: CGFloat = 440

    func body(content: Content) -> some View {
        let logo = SlowLoading.showsLogo(isLoading: isLoading, hasContent: false, elapsed: elapsed, threshold: after)
        content
            .opacity(logo ? 0 : 1)
            .accessibilityHidden(logo)
            .overlay(alignment: .top) {
                if logo {
                    GeometryReader { proxy in
                        LoadingStateView(lines: lines, minHeight: 0, logoSize: logoSize)
                            .frame(width: proxy.size.width, height: min(proxy.size.height, Self.centeredWithin))
                    }
                    .transition(.opacity)
                }
            }
            .animation(reduceMotion ? Motion.reduced : Motion.standard, value: logo)
            .task(id: isLoading) {
                elapsed = .zero
                guard isLoading else { return }
                let started = ContinuousClock.now
                try? await Task.sleep(for: after)
                guard !Task.isCancelled else { return }
                elapsed = started.duration(to: .now)
            }
    }
}

extension View {
    /// The loading policy for a skeleton (see `SlowLoading`): after `after` of `isLoading` it
    /// cross-fades to the logo loader over `lines`, keeping its height. Put it on the skeleton
    /// branch only; content that's reloading keeps showing with `sqRefreshing` instead.
    func sqSlowLoading(_ isLoading: Bool = true, after: Duration = SlowLoading.threshold, lines: [String] = [],
                       logoSize: CGFloat = 40) -> some View {
        modifier(SlowLoadingModifier(isLoading: isLoading, after: after, lines: lines, logoSize: logoSize))
    }
}
