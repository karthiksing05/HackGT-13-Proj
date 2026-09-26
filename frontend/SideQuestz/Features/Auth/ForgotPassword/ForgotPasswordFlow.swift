import SwiftUI

/// Forgot password (GUI_PLAN.md §7.3): email → 6-digit code → new password → done.
/// Full screen, cream; "‹ Back" + progress dots on top. Steps live in this folder.
struct ForgotPasswordFlow: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    @State private var step = 1
    @State private var email = ""
    @State private var code = ""
    /// Short-lived token from `verifyResetCode`, spent by `resetPassword`.
    @State private var resetToken = ""
    @State private var newPassword = ""
    @State private var confirmPassword = ""
    @State private var revealed = false
    /// Set by a failed submit; the step's validation message then updates as you type.
    @State private var tried = false
    @State private var serverError: String?
    @State private var busy = false
    @State private var resending = false
    @State private var resent = false
    @FocusState private var focus: AuthFocus?

    var body: some View {
        VStack(spacing: 0) {
            AuthFlowHeader {
                if step < 4 { AuthBackButton(action: back) }
            } center: {
                // Without the Back button the prototype's header row collapses to the dots' height,
                // so on "Password updated" they sit at the top of the row.
                ProgressDots(total: 4, current: step)
                    .frame(maxHeight: .infinity, alignment: step < 4 ? .center : .top)
            } trailing: {
                EmptyView()
            }

            ScrollView {
                stepContent
                    .padding(.horizontal, 24)
                    .padding(.top, 16)
                    .padding(.bottom, 40)
                    .id(step)
                    .transition(.opacity)
            }
            .scrollBounceBehavior(.basedOnSize)
            .scrollDismissesKeyboard(.interactively)
            .padding(.top, 16)
        }
        .background(Theme.cream.ignoresSafeArea())
        .onAppear(perform: start)
        .onChange(of: email) { serverError = nil }
        .onChange(of: code) { _, new in
            let digits = String(new.filter { $0.isASCII && $0.isWholeNumber }.prefix(6))
            if digits != new { code = digits }
            serverError = nil
        }
        .sensoryFeedback(.success, trigger: step) { _, new in new == 4 }
    }

    @ViewBuilder private var stepContent: some View {
        switch step {
        case 1:
            ForgotEmailStep(email: $email, error: shownError, busy: busy, focus: $focus,
                            onSend: submit, onSignIn: { router.showLogin(email: trimmedEmail) })
        case 2:
            ForgotCodeStep(email: trimmedEmail, code: $code, error: shownError, busy: busy,
                           resending: resending, resent: resent, showsDemoHint: env.isMock, focus: $focus,
                           onVerify: submit, onResend: resend, onChangeEmail: changeEmail)
        case 3:
            ForgotNewPasswordStep(password: $newPassword, confirm: $confirmPassword, revealed: $revealed,
                                  error: shownError, busy: busy, focus: $focus, onReset: submit)
        default:
            ForgotDoneStep { router.showLogin(email: trimmedEmail) }
        }
    }

    // MARK: Validation

    private var trimmedEmail: String { email.trimmingCharacters(in: .whitespacesAndNewlines) }

    private var validationError: String? {
        switch step {
        case 1:
            return Validation.isValidEmail(trimmedEmail) ? nil : "Enter a valid email address."
        case 2:
            return code.count == 6 ? nil : "Enter the 6-digit code from the email."
        case 3:
            return Validation.passwordRulesPass(newPassword, confirmPassword) ? nil : "Check the password rules above."
        default:
            return nil
        }
    }

    private var shownError: String? {
        if tried, let validationError { return validationError }
        return serverError
    }

    // MARK: Actions

    private func start() {
        if email.isEmpty { email = router.authEmail }
        // Demo deep link `-SQRoute forgot/N`.
        if let parts = router.consumeLaunch("forgot"), let n = parts.first.flatMap({ Int($0) }) {
            step = min(max(n, 1), 4)
        }
    }

    /// Send code · Verify code · Reset password.
    private func submit() {
        guard !busy else { return }
        serverError = nil
        guard validationError == nil else {
            tried = true
            return
        }
        tried = false
        focus = nil
        busy = true
        let current = step
        Task {
            defer { busy = false }
            do {
                switch current {
                case 1:
                    try await env.api.forgotPassword(email: trimmedEmail)
                case 2:
                    resetToken = try await env.api.verifyResetCode(email: trimmedEmail, code: code)
                case 3:
                    try await env.api.resetPassword(resetToken: resetToken, newPassword: newPassword)
                default:
                    break
                }
                // Went Back while this was running: stay where the user is now.
                guard step == current else { return }
                go(to: current + 1)
            } catch {
                serverError = authMessage(for: error)
            }
        }
    }

    private func resend() {
        guard !resending else { return }
        resending = true
        serverError = nil
        Task {
            defer { resending = false }
            do {
                try await env.api.resendResetCode(email: trimmedEmail)
                code = ""
                tried = false
                resent = true
            } catch {
                serverError = authMessage(for: error)
            }
        }
    }

    private func changeEmail() {
        code = ""
        go(to: 1)
    }

    private func back() {
        if step > 1 {
            go(to: step - 1)
        } else {
            router.showLogin(email: trimmedEmail)
        }
    }

    private func go(to newStep: Int) {
        focus = nil
        tried = false
        serverError = nil
        resent = false
        withAnimation(reduceMotion ? nil : .easeInOut(duration: 0.22)) { step = newStep }
    }
}

// MARK: - Shared step pieces

/// 16pt `text2` body copy under a step title (line height 1.45).
struct ForgotBodyText: View {
    let text: Text

    var body: some View {
        text
            .sqFont(16)
            .foregroundStyle(Theme.text2)
            .authLineHeight(1.45, size: 16)
            .fixedSize(horizontal: false, vertical: true)
    }
}

/// 56pt rounded-16 `sageTint` tile with the step's sageInk icon, sized to the prototype's 28pt
/// line icons (the key lies diagonally, ring bottom-left).
struct ForgotStepIcon: View {
    enum Glyph {
        case lock, envelope, key
    }

    let glyph: Glyph

    var body: some View {
        icon
            .foregroundStyle(Theme.sageInk)
            .frame(width: 56, height: 56)
            .background(Theme.sageTint, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
            .accessibilityHidden(true)
    }

    @ViewBuilder private var icon: some View {
        switch glyph {
        case .lock:
            Image(systemName: "lock").font(.system(size: 23, weight: .medium))
        case .envelope:
            Image(systemName: "envelope").font(.system(size: 19, weight: .medium))
        case .key:
            // Mirrored + tilted so the ring sits bottom-left and the teeth face down-right.
            Image(systemName: "key.horizontal").font(.system(size: 22, weight: .medium))
                .scaleEffect(x: -1, y: 1)
                .rotationEffect(.degrees(-45))
        }
    }
}

#Preview {
    ForgotPasswordFlow()
        .environment(AppEnvironment.preview())
        .environment(Router(phase: .auth))
}
