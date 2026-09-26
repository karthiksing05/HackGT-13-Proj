import SwiftUI

/// Forgot password · 3 "Set a new password" — one Show/Hide toggles both fields; "Reset password"
/// stays grey `#C3C7BD` (ink text) until every rule passes.
struct ForgotNewPasswordStep: View {
    @Binding var password: String
    @Binding var confirm: String
    @Binding var revealed: Bool
    let error: String?
    let busy: Bool
    var focus: FocusState<AuthFocus?>.Binding
    let onReset: () -> Void

    private var rulesPass: Bool { Validation.passwordRulesPass(password, confirm) }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            ForgotStepIcon(glyph: .key)
            AuthStepTitle(text: "Set a new password")
            ForgotBodyText(text: Text("Pick something you haven't used here before."))
            AuthField(label: "New password", text: $password, placeholder: "At least 8 characters", kind: .newPassword,
                      revealed: $revealed, revealsBoth: true, focus: focus, field: .password)
                .submitLabel(.next)
                .onSubmit { focus.wrappedValue = .confirm }
            AuthField(label: "Confirm new password", text: $confirm, placeholder: "Type it again", kind: .newPassword,
                      revealed: $revealed, revealsBoth: true, focus: focus, field: .confirm)
                .submitLabel(.go)
                .onSubmit(onReset)
            PasswordRulesView(password: password, confirm: confirm)
            if let error {
                AuthErrorText(message: error)
                    .sqTransition(.rise)
            }
            AuthPrimaryButton(title: "Reset password", busy: busy, fill: rulesPass ? Theme.sage : Theme.mutedBorder,
                              action: onReset)
                .padding(.top, 6)
                .animation(.easeOut(duration: 0.15), value: rulesPass)
        }
    }
}
