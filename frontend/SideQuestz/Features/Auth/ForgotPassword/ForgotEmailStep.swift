import SwiftUI

/// Forgot password · 1 "Forgot your password?" — the email is prefilled from Login.
struct ForgotEmailStep: View {
    @Binding var email: String
    let error: String?
    let busy: Bool
    var focus: FocusState<AuthFocus?>.Binding
    let onSend: () -> Void
    let onSignIn: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            ForgotStepIcon(glyph: .lock)
            AuthStepTitle(text: "Forgot your password?")
            ForgotBodyText(text: Text("Enter the email you signed up with. We'll send you a 6-digit code."))
            AuthField(label: "Email", text: $email, placeholder: "you@school.edu", kind: .email,
                      focus: focus, field: .email)
                .submitLabel(.send)
                .onSubmit(onSend)
            if let error {
                AuthErrorText(message: error)
            }
            AuthPrimaryButton(title: "Send code", busy: busy, action: onSend)
                .padding(.top, 6)
            AuthPromptButton(prompt: "Remembered it?", action: "Sign in", onTap: onSignIn)
        }
    }
}
