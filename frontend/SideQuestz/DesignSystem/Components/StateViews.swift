import SwiftUI

/// The state of any network-backed screen section.
enum Loadable<Value> {
    case loading
    case loaded(Value)
    case failed(String)

    var value: Value? {
        if case .loaded(let value) = self { return value }
        return nil
    }

    var isLoading: Bool {
        if case .loading = self { return true }
        return false
    }

    /// Which of the three states this is, for animating between them.
    var phase: LoadablePhase {
        switch self {
        case .loading: .loading
        case .loaded: .loaded
        case .failed: .failed
        }
    }
}

enum LoadablePhase: Hashable {
    case loading, loaded, failed
}

extension Loadable {
    /// Runs `load` and wraps the result, turning any error into a short sentence.
    static func run(_ load: () async throws -> Value) async -> Loadable<Value> {
        do {
            return .loaded(try await load())
        } catch {
            return .failed((error as? LocalizedError)?.errorDescription ?? "Something went wrong.")
        }
    }
}

/// Loading: the logo with its S drawing and erasing on a loop, plus an optional caption. Pass several
/// `lines` for long-running work (plans, routes) and they rotate while the request runs.
struct LoadingStateView: View {
    var label: String? = nil
    var lines: [String] = []
    var minHeight: CGFloat = 160
    var logoSize: CGFloat = 40

    var body: some View {
        VStack(spacing: 12) {
            LogoLoadingView(size: logoSize)
            if lines.count > 1 {
                CyclingStatusText(lines: lines).captionStyle(13)
            } else if let text = label ?? lines.first {
                Text(text).captionStyle(13)
            }
        }
        .frame(maxWidth: .infinity, minHeight: minHeight)
        .accessibilityElement(children: .combine)
    }
}

/// Error: a short sentence + "Try again".
struct ErrorStateView: View {
    let message: String
    var minHeight: CGFloat = 160
    let retry: () -> Void

    var body: some View {
        VStack(spacing: 12) {
            Text(message)
                .sqFont(15)
                .foregroundStyle(Theme.text2)
                .multilineTextAlignment(.center)
            Button("Try again", action: retry)
                .buttonStyle(.sqTintPill)
        }
        .padding(.horizontal, Metrics.side)
        .frame(maxWidth: .infinity, minHeight: minHeight)
    }
}

/// Empty: one short line in `text3`.
struct EmptyStateView: View {
    let message: String
    var minHeight: CGFloat = 120

    var body: some View {
        Text(message)
            .sqFont(15)
            .foregroundStyle(Theme.text3)
            .multilineTextAlignment(.center)
            .padding(20)
            .frame(maxWidth: .infinity, minHeight: minHeight)
    }
}

/// Switches between loading / error / content for a `Loadable`, cross-fading between them. While
/// loading it shows `skeleton` (the content's shape, shimmering) or else the logo loader.
/// Several views passed as content stack with no spacing: wrap rows in your own `VStack(spacing:)`.
struct LoadableView<Value, Content: View>: View {
    let state: Loadable<Value>
    var loadingLabel: String? = nil
    var skeleton: SkeletonLayout? = nil
    var minHeight: CGFloat = 160
    let retry: () -> Void
    @ViewBuilder var content: (Value) -> Content

    var body: some View {
        // A VStack, not a ZStack, so several content views stack instead of overlapping; the
        // cross-fade still overlaps because outgoing views no longer take layout space.
        VStack(spacing: 0) {
            switch state {
            case .loading:
                Group {
                    if let skeleton {
                        SkeletonView(layout: skeleton)
                    } else {
                        LoadingStateView(label: loadingLabel, minHeight: minHeight)
                    }
                }
                .transition(.opacity)
            case .failed(let message):
                ErrorStateView(message: message, minHeight: minHeight, retry: retry)
                    .transition(.opacity)
            case .loaded(let value):
                content(value)
                    .transition(.opacity)
            }
        }
        .animation(Motion.standard, value: state.phase)
    }
}

/// Green confirmation banner with a check that draws in and an "OK" dismiss (Splits: 'Added
/// "Pizza" · …'). Insert it with `.sqTransition(.banner)` inside `withMotion(Motion.arrive)`.
struct SuccessBanner: View {
    let text: String
    let dismiss: () -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            AnimatedCheck(lineWidth: 2.8, delay: 0.2).frame(width: 14, height: 14).padding(.top, 2)
            Text(text).sqFont(14).lineHeight(1.4, fontSize: 14).frame(maxWidth: .infinity, alignment: .leading)
            Button("OK", action: dismiss).buttonStyle(.plain).sqFont(13, .semibold).accessibilityLabel("Dismiss")
        }
        .foregroundStyle(Theme.successBannerText)
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .background(Theme.successBg, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .accessibilityElement(children: .combine)
    }
}
