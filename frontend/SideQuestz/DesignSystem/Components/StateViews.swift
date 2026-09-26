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

/// Loading: a small logo with the S drawing in on a loop.
struct LoadingStateView: View {
    var label: String? = nil
    var minHeight: CGFloat = 160

    var body: some View {
        VStack(spacing: 10) {
            LogoLoadingView(size: 40)
            if let label { Text(label).captionStyle(13) }
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

/// Switches between loading / error / content for a `Loadable`.
struct LoadableView<Value, Content: View>: View {
    let state: Loadable<Value>
    var loadingLabel: String? = nil
    var minHeight: CGFloat = 160
    let retry: () -> Void
    @ViewBuilder var content: (Value) -> Content

    var body: some View {
        switch state {
        case .loading:
            LoadingStateView(label: loadingLabel, minHeight: minHeight)
        case .failed(let message):
            ErrorStateView(message: message, minHeight: minHeight, retry: retry)
        case .loaded(let value):
            content(value)
        }
    }
}

/// Green confirmation banner with a check and an "OK" dismiss (Splits: 'Added "Pizza" · …').
struct SuccessBanner: View {
    let text: String
    let dismiss: () -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            CheckGlyph(lineWidth: 2.8).frame(width: 14, height: 14).padding(.top, 2)
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
