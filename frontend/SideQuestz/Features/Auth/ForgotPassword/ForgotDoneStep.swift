import SwiftUI

/// Forgot password · 4 "Password updated" — back to Login with the email prefilled.
struct ForgotDoneStep: View {
    let onBackToSignIn: () -> Void

    var body: some View {
        VStack(spacing: 14) {
            VStack(spacing: 14) {
                ZStack {
                    Circle().fill(Theme.sage)
                    // Prototype: 38pt check, stroke 2.4 in a 24-unit box (≈3.8pt).
                    CheckGlyph(lineWidth: 2.4)
                        .foregroundStyle(Theme.ink)
                        .frame(width: 38, height: 38)
                }
                .frame(width: 76, height: 76)
                .accessibilityHidden(true)

                AuthStepTitle(text: "Password updated")
                    .multilineTextAlignment(.center)
                Text("You're all set. Sign in with your new password. We also signed you out on other devices.")
                    .sqFont(16)
                    .foregroundStyle(Theme.text2)
                    .authLineHeight(1.45, size: 16)
                    .multilineTextAlignment(.center)
                    .frame(maxWidth: 290)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .frame(maxWidth: .infinity)
            .padding(.top, 40)
            .accessibilityElement(children: .combine)

            AuthPrimaryButton(title: "Back to sign in", action: onBackToSignIn)
                .padding(.top, 20)
        }
    }
}
