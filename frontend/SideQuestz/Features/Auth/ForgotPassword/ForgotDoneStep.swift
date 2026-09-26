import SwiftUI

/// Forgot password · 4 "Password updated" — back to Login with the email prefilled.
///
/// Arrives celebrating: the sage circle pops in and its check draws itself, then the title, the
/// text and the button rise in one after another (Reduce Motion: they just fade in).
struct ForgotDoneStep: View {
    let onBackToSignIn: () -> Void

    @State private var shown = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        VStack(spacing: 14) {
            VStack(spacing: 14) {
                ZStack {
                    Circle().fill(Theme.sage)
                    // Prototype: 38pt check, stroke 2.4 in a 24-unit box (≈3.8pt).
                    AnimatedCheck(lineWidth: 2.4, delay: 0.4)
                        .foregroundStyle(Theme.ink)
                        .frame(width: 38, height: 38)
                }
                .frame(width: 76, height: 76)
                .scaleEffect(shown || reduceMotion ? 1 : 0.4)
                .opacity(shown ? 1 : 0)
                .animation(reduceMotion ? Motion.reduced : .spring(duration: 0.5, bounce: 0.35).delay(0.12), value: shown)
                .accessibilityHidden(true)

                AuthStepTitle(text: "Password updated")
                    .multilineTextAlignment(.center)
                    .forgotDoneRise(shown, delay: 0.3, reduceMotion: reduceMotion)
                Text("You're all set. Sign in with your new password. We also signed you out on other devices.")
                    .sqFont(16)
                    .foregroundStyle(Theme.text2)
                    .authLineHeight(1.45, size: 16)
                    .multilineTextAlignment(.center)
                    .frame(maxWidth: 290)
                    .fixedSize(horizontal: false, vertical: true)
                    .forgotDoneRise(shown, delay: 0.37, reduceMotion: reduceMotion)
            }
            .frame(maxWidth: .infinity)
            .padding(.top, 40)
            .accessibilityElement(children: .combine)

            AuthPrimaryButton(title: "Back to sign in", action: onBackToSignIn)
                .padding(.top, 20)
                .forgotDoneRise(shown, delay: 0.44, reduceMotion: reduceMotion)
        }
        .onAppear { shown = true }
    }
}

private extension View {
    /// Fades in rising 12pt, `delay` seconds after the step appears.
    func forgotDoneRise(_ shown: Bool, delay: Double, reduceMotion: Bool) -> some View {
        opacity(shown ? 1 : 0)
            .offset(y: shown || reduceMotion ? 0 : 12)
            .animation((reduceMotion ? Motion.reduced : Motion.arrive).delay(delay), value: shown)
    }
}
