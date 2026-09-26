import SwiftUI

/// Forgot password · 2 "Check your email" — 6-digit code (numeric, one-time-code autofill).
struct ForgotCodeStep: View {
    let email: String
    @Binding var code: String
    let error: String?
    let busy: Bool
    let resending: Bool
    let resent: Bool
    /// Mock backend only: "Demo: any 6 digits work."
    let showsDemoHint: Bool
    var focus: FocusState<AuthFocus?>.Binding
    let onVerify: () -> Void
    let onResend: () -> Void
    let onChangeEmail: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            ForgotStepIcon(glyph: .envelope)
            AuthStepTitle(text: "Check your email")
            ForgotBodyText(text: Text("We sent a 6-digit code to ")
                + Text(email).fontWeight(.semibold).foregroundStyle(Theme.ink)
                + Text(". It expires in 10 minutes."))
            AuthField(label: "6-digit code", text: $code, placeholder: "000000", kind: .code,
                      focus: focus, field: .code)
                .submitLabel(.continue)
                .onSubmit(onVerify)
            if let error {
                AuthErrorText(message: error)
            }
            if showsDemoHint {
                Text("Demo: any 6 digits work.")
                    .sqFont(13)
                    .foregroundStyle(Theme.text3)
                    .authLineHeight(1.35, size: 13)
            }
            AuthPrimaryButton(title: "Verify code", busy: busy, action: onVerify)
                .padding(.top, 6)
            resendRow
            if resent {
                Text("New code sent.")
                    .sqFont(13)
                    .foregroundStyle(Theme.success)
                    .authLineHeight(1.35, size: 13)
                    .frame(maxWidth: .infinity)
                    .onAppear { AccessibilityNotification.Announcement("New code sent.").post() }
            }
            AuthLinkButton(title: "Use a different email", action: onChangeEmail)
        }
    }

    /// "Didn't get it? Check spam, or **resend code**" (14pt, 6pt gap, centered).
    private var resendRow: some View {
        HStack(spacing: 6) {
            Text("Didn't get it? Check spam, or")
                .foregroundStyle(Theme.text2)
            Button(action: onResend) {
                ZStack {
                    Text("resend code").opacity(resending ? 0 : 1)
                    if resending {
                        ProgressView().controlSize(.small).tint(Theme.sageInk)
                    }
                }
                .fontWeight(.semibold)
                .foregroundStyle(Theme.sageInk)
                .frame(minHeight: Metrics.minTouch)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(resending)
            .authHitHeight(18.9)
        }
        .sqFont(14)
        .lineLimit(1)
        .minimumScaleFactor(0.85)
        // The button lays out at the 18.9pt line box (14 × 1.35).
        .frame(maxWidth: .infinity, minHeight: 18.9)
    }
}
