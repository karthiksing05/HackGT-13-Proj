import SwiftUI

// PLACEHOLDER — replaced by the feature implementation.
struct SplashView: View {
    let onFinish: () -> Void

    var body: some View {
        VStack(spacing: 22) {
            LogoMark(size: 132)
            Wordmark(size: 40)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Theme.cream.ignoresSafeArea())
        .task {
            try? await Task.sleep(nanoseconds: 1_200_000_000)
            onFinish()
        }
    }
}
