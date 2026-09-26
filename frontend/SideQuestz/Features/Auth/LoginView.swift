import SwiftUI

/// Login (GUI_PLAN.md §7.2): email + password only. Cream, sides 28, top 72, bottom 36.
struct LoginView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router

    @State private var email = ""
    @State private var password = ""
    @State private var revealed = false
    /// Set by a failed submit; the validation message then updates as you type (like the prototype).
    @State private var tried = false
    @State private var serverError: String?
    @State private var submitting = false
    /// Signed in: the form is fading out with its password already cleared.
    @State private var signedIn = false
    @FocusState private var focus: AuthFocus?

    var body: some View {
        GeometryReader { proxy in
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    intro
                    Spacer(minLength: 28)
                    form
                }
                .padding(.horizontal, 28)
                .designTopPadding(72)
                .authDesignBottomPadding(36, minimum: 2)
                .frame(minHeight: proxy.size.height)
            }
            .scrollBounceBehavior(.basedOnSize)
            .scrollDismissesKeyboard(.interactively)
        }
        .background(Theme.cream.ignoresSafeArea())
        .onAppear {
            if email.isEmpty { email = router.authEmail }
        }
        .onChange(of: email) { serverError = nil }
        .onChange(of: password) { serverError = nil }
    }

    // MARK: Sections

    private var intro: some View {
        VStack(alignment: .leading, spacing: 18) {
            LogoMark(size: 84)
                // Hidden: long-press the logo to replay the opening animation.
                .onLongPressGesture(minimumDuration: 0.8, perform: replayIntro)
                .accessibilityAction(named: "Replay intro", replayIntro)
            // The prototype's <h1>: a 44pt line-height-1 wordmark on a 30pt/1.35 SF strut → 47.4pt box.
            Wordmark(size: 44)
                .padding(.top, 44 * (1 - AuthFontMetrics.mono) / 2)
                .padding(.bottom, 44 * (1 - AuthFontMetrics.mono) / 2 + 3.39)
                .accessibilityAddTraits(.isHeader)
            Text("Turn waiting into wandering.")
                .sqFont(21, .semibold)
                .foregroundStyle(Theme.ink)
                .authLineHeight(1.3, size: 21)
            Text("Tell us how long you've got and what you're in the mood for. We'll plan the rest.")
                .sqFont(16)
                .foregroundStyle(Theme.text2)
                .authLineHeight(1.45, size: 16)
                .frame(maxWidth: 300, alignment: .leading)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private var form: some View {
        VStack(spacing: 12) {
            AuthField(label: "Email", text: $email, placeholder: "you@school.edu", kind: .email,
                      hasError: showsValidation, focus: $focus, field: .email)
                .submitLabel(.next)
                .onSubmit {
                    if password.isEmpty { focus = .password } else { signIn() }
                }
            AuthField(label: "Password", text: $password, placeholder: signedIn ? "" : "Your password", kind: .password,
                      revealed: $revealed, hasError: showsValidation, focus: $focus, field: .password)
                .submitLabel(.go)
                .onSubmit(signIn)

            if let message = shownError {
                AuthErrorText(message: message)
            }

            Button("Forgot password?") {
                router.authEmail = email.trimmingCharacters(in: .whitespaces)
                router.showForgotPassword()
            }
            .buttonStyle(.sqLink(size: 14))
            .authHitHeight(28)
            .frame(maxWidth: .infinity, alignment: .trailing)

            AuthPrimaryButton(title: "Sign in", busy: submitting, action: signIn)

            AuthPromptButton(prompt: "New here?", action: "Create an account") {
                router.authEmail = email.trimmingCharacters(in: .whitespaces)
                router.showSetup(step: 1)
            }

            Text("By signing in you agree to the Terms and Privacy Policy.")
                .sqFont(12)
                .foregroundStyle(Theme.text3)
                .authLineHeight(1.35, size: 12)
                .multilineTextAlignment(.center)
                .frame(maxWidth: .infinity)
        }
    }

    // MARK: Validation + sign in

    private var trimmedEmail: String { email.trimmingCharacters(in: .whitespacesAndNewlines) }

    private var validationError: String? {
        if !Validation.isValidEmail(trimmedEmail) { return "Enter a valid email address." }
        if password.isEmpty { return "Enter your password." }
        return nil
    }

    private var showsValidation: Bool { tried && validationError != nil }

    private var shownError: String? { showsValidation ? validationError : serverError }

    private func signIn() {
        guard !submitting else { return }
        serverError = nil
        guard validationError == nil else {
            tried = true
            return
        }
        tried = false
        focus = nil
        submitting = true
        Task {
            defer { submitting = false }
            do {
                let response = try await env.api.login(email: trimmedEmail, password: password)
                env.startSession(response)
                // Clear the password before the form goes away: otherwise iOS starts its "Save
                // Password?" flow as the form disappears, and on the simulator that flow leaves an
                // invisible window over the app that swallows every touch.
                signedIn = true
                password = ""
                router.enterMain()
            } catch {
                serverError = authMessage(for: error, fallback: "Couldn't sign in. Try again.")
            }
        }
    }

    private func replayIntro() {
        UserDefaults.standard.set(false, forKey: "hasSeenIntro")
        withAnimation(.easeInOut(duration: 0.3)) { router.phase = .splash }
    }
}

#Preview {
    LoginView()
        .environment(AppEnvironment.preview())
        .environment(Router(phase: .auth))
}
