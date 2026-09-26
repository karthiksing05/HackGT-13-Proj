import SwiftUI

/// Profile setup (GUI_PLAN.md §7.4): 5 steps after "Create an account" — basics, calendar,
/// likes, money, open questions. Leaving step 1 signs up (`POST /auth/signup`); Finish or Skip
/// saves `PUT /me/preferences` and goes Home.
///
/// Redo (`isRedo`, from Account › "Redo setup questions"): starts at `startStep` (3) with the
/// saved preferences, never signs up, and closes when done (Back on the first step cancels).
struct SetupFlowView: View {
    let startStep: Int
    let isRedo: Bool

    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router
    @Environment(\.safeAreaBottom) private var safeBottom
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    @State private var step: Int
    @State private var draft = SetupDraft()
    @State private var busy = false
    @State private var basicsTried = false
    @State private var stepError: String?
    @State private var showPhoto = false
    /// Waiting for saved preferences (redo) or the demo deep link's account. Starts true so no
    /// step loads anything before `prefill` decides (it clears this right away when not needed).
    @State private var preparing = true
    /// Redo: the saved preferences couldn't be loaded (don't overwrite them with defaults).
    @State private var loadError: String?
    /// Redo: saving on Back failed once; the next Back leaves without saving.
    @State private var redoSaveFailed = false
    @State private var scrollTarget: SetupScrollTarget?
    @FocusState private var focus: AuthFocus?

    init(startStep: Int, isRedo: Bool) {
        self.startStep = startStep
        self.isRedo = isRedo
        _step = State(initialValue: min(max(startStep, 1), 5))
    }

    private var firstStep: Int { isRedo ? min(max(startStep, 1), 5) : 1 }

    var body: some View {
        VStack(spacing: 0) {
            header
            progressBar
                .padding(.top, 6)
                .padding(.horizontal, 20)
            ScrollViewReader { proxy in
                ScrollView {
                    Group {
                        if preparing {
                            LoadingStateView(minHeight: 320)
                        } else if let loadError {
                            ErrorStateView(message: loadError, minHeight: 320) {
                                Task { await prepare() }
                            }
                        } else {
                            stepContent
                        }
                    }
                    .padding(.horizontal, 20)
                    .padding(.top, 16)
                    .padding(.bottom, 20)
                }
                .id(step)
                .transition(.opacity)
                .scrollBounceBehavior(.basedOnSize)
                .scrollDismissesKeyboard(.interactively)
                .safeAreaPadding(.bottom, focus == nil ? footerHeight : 12)
                .onChange(of: scrollTarget) { _, target in
                    guard let target else { return }
                    scrollTarget = nil
                    DispatchQueue.main.async {
                        withAnimation(.easeInOut(duration: 0.3)) { proxy.scrollTo(target, anchor: .center) }
                    }
                }
            }
            .padding(.top, 12)
        }
        .overlay(alignment: .bottom) { footer }
        .background(Theme.cream.ignoresSafeArea())
        .sqSheet(isPresented: $showPhoto) {
            PhotoSheet(initials: draft.initials, color: $draft.avatarColor, savesToServer: draft.signedUp) {
                showPhoto = false
            }
        }
        .onAppear(perform: prefill)
        .task { await prepare() }
        .onDisappear { env.voice.cancel() }
    }

    // MARK: Header, progress, footer

    private var header: some View {
        AuthFlowHeader {
            AuthBackButton(action: back)
        } center: {
            Text("Step \(step) of 5")
                .sqFont(15, .semibold)
                .foregroundStyle(Theme.text3)
                .authLineHeight(1.35, size: 15)
                .accessibilityAddTraits(.isHeader)
        } trailing: {
            if step >= 2 {
                Button(action: finish) {
                    Text("Skip")
                        .sqFont(16)
                        .foregroundStyle(Theme.text3)
                        .padding(.trailing, 8)
                        .frame(minWidth: Metrics.minTouch, minHeight: Metrics.minTouch, alignment: .trailing)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .disabled(busy || preparing || loadError != nil)
                .accessibilityHint("Saves your answers so far and finishes setup")
            }
        }
    }

    /// 4pt track (`segmentBg`), sage fill at step × 20%.
    private var progressBar: some View {
        GeometryReader { proxy in
            Rectangle()
                .fill(Theme.sage)
                .frame(width: proxy.size.width * CGFloat(step) / 5)
        }
        .frame(height: 4)
        .background(Theme.segmentBg)
        .clipShape(RoundedRectangle(cornerRadius: 2, style: .continuous))
        .animation(reduceMotion ? nil : .easeInOut(duration: 0.25), value: step)
        .accessibilityHidden(true)
    }

    /// Footer height above the home-indicator inset. The prototype footer is 96 tall with the
    /// button 31pt above the screen's bottom edge, so on a 34pt-inset phone the button dips 3pt in.
    private var footerHeight: CGFloat { max(96 - safeBottom, 53) }

    /// Cream footer with a top border; the button sits 31pt above the screen's bottom edge.
    private var footer: some View {
        VStack(spacing: 0) {
            Rectangle().fill(Theme.line).frame(height: 1)
            AuthPrimaryButton(title: nextLabel, busy: busy, action: next)
                .padding(.horizontal, 20)
                .padding(.top, 12)
                .padding(.bottom, footerHeight - 65)
        }
        .background(Theme.cream.ignoresSafeArea(edges: .bottom))
        .ignoresSafeArea(.keyboard)
        .disabled(preparing || loadError != nil)
    }

    private var nextLabel: String {
        switch step {
        case 2: draft.hasCalendar ? "Continue" : "Continue without a calendar"
        case 5: "Finish setup"
        default: "Continue"
        }
    }

    // MARK: Steps

    @ViewBuilder private var stepContent: some View {
        VStack(alignment: .leading, spacing: 12) {
            switch step {
            case 1:
                SetupBasicsStep(draft: draft, showsErrors: basicsTried, serverError: stepError, focus: $focus) {
                    focus = nil
                    showPhoto = true
                }
            case 2:
                SetupConnectStep(draft: draft)
            case 3:
                SetupLikesStep(draft: draft)
            case 4:
                SetupMoneyStep(draft: draft)
            default:
                SetupMoreStep(draft: draft)
            }
            if step != 1, let stepError {
                ErrorBox(messages: [stepError])
                    .id(SetupScrollTarget.stepError)
            }
        }
    }

    // MARK: Navigation

    private func next() {
        guard !busy else { return }
        stepError = nil
        switch step {
        case 1: submitBasics()
        case 5: finish()
        default: go(to: step + 1)
        }
    }

    private func back() {
        if step > firstStep {
            go(to: step - 1)
        } else if isRedo {
            // Back on the first step keeps your edits (saves, then closes).
            if redoSaveFailed || loadError != nil || preparing {
                env.voice.cancel()
                router.setupRedo = nil
            } else {
                finish(leaving: true)
            }
        } else {
            let email = draft.trimmedEmail
            if draft.signedUp {
                // Leaving setup for Login: don't stay half signed in.
                Task { await env.signOut() }
            }
            router.showLogin(email: email)
        }
    }

    private func go(to newStep: Int) {
        focus = nil
        stepError = nil
        redoSaveFailed = false
        env.voice.cancel()
        withAnimation(reduceMotion ? nil : .easeInOut(duration: 0.22)) { step = newStep }
    }

    // MARK: Step 1 · sign up

    private func submitBasics() {
        guard draft.basicsErrors.isEmpty else {
            basicsTried = true
            scrollTarget = .basicsErrors
            return
        }
        if let age = draft.age(on: env.clock.now, calendar: env.clock.calendar), age < 13 {
            // The age note already says why (red box).
            scrollTarget = .ageNote
            return
        }
        focus = nil
        busy = true
        Task {
            defer { busy = false }
            do {
                try await saveAccount()
                basicsTried = false
                go(to: 2)
            } catch {
                stepError = authMessage(for: error, fallback: "Couldn't create your account. Try again.")
                scrollTarget = .basicsErrors
            }
        }
    }

    /// Signs up the first time; afterwards (Back to step 1) updates the account instead.
    private func saveAccount() async throws {
        if draft.signedUp {
            env.user = try await env.api.updateMe(UserPatch(name: draft.trimmedName, username: draft.cleanUsername,
                                                            dateOfBirth: draft.birthDate))
        } else {
            let request = SignupRequest(name: draft.trimmedName, email: draft.trimmedEmail, password: draft.password,
                                        username: draft.cleanUsername, dateOfBirth: draft.birthDate)
            let response = try await env.api.signup(request)
            env.startSession(response)
            draft.signedUp = true
            draft.lookPending = env.profileImage != nil || draft.avatarColor != response.user.avatarColor
        }
        if draft.lookPending {
            try await sendPickedLook()
            draft.lookPending = false
        }
    }

    /// The photo and initials color picked in the Photo sheet before the account existed.
    private func sendPickedLook() async throws {
        if env.user?.avatarColor != draft.avatarColor {
            try await env.api.setAvatarColor(draft.avatarColor)
            env.user?.avatarColor = draft.avatarColor
        }
        if let image = env.profileImage, let data = image.uploadJPEG() {
            env.user?.photoURL = try await env.api.uploadPhoto(data)
        }
    }

    // MARK: Finish / Skip

    private func finish() {
        finish(leaving: false)
    }

    /// Saves the answers, then goes Home (or closes the redo). `leaving`: Back in redo mode.
    private func finish(leaving: Bool) {
        guard !busy else { return }
        env.voice.cancel()
        focus = nil
        stepError = nil
        busy = true
        Task {
            defer { busy = false }
            do {
                try await env.api.savePreferences(draft.preferences)
                env.preferences = draft.preferences
                if isRedo {
                    router.setupRedo = nil
                } else {
                    router.enterMain()
                }
            } catch {
                let message = authMessage(for: error, fallback: "Couldn't save your answers. Try again.")
                if leaving {
                    redoSaveFailed = true
                    stepError = message + " Tap Back again to leave without saving."
                } else {
                    stepError = message
                }
                scrollTarget = .stepError
            }
        }
    }

    // MARK: Start

    /// Synchronous prefill, before the first frame.
    private func prefill() {
        if !isRedo && draft.email.isEmpty { draft.email = router.authEmail }
        if isRedo, let saved = env.preferences { draft.preferences = saved }
        preparing = (isRedo && env.preferences == nil) || isDemoDeepLinkMidSetup
    }

    private func prepare() async {
        if isRedo && env.preferences == nil {
            preparing = true
            loadError = nil
            do {
                let saved = try await env.api.preferences()
                env.preferences = saved
                draft.preferences = saved
            } catch {
                loadError = authMessage(for: error, fallback: "Couldn't load your answers.")
            }
        }
        let midSetupDeepLink = isDemoDeepLinkMidSetup
        if router.consumeLaunch("setup") != nil, midSetupDeepLink {
            await createDemoAccountForDeepLink()
        }
        preparing = false
    }

    /// `-SQRoute setup/2…5` in mock mode: the later steps come after sign-up, so create the account
    /// step 1 would have (as the current mock user) before showing them.
    private var isDemoDeepLinkMidSetup: Bool {
        env.isMock && !isRedo && step >= 2 && router.peekLaunch("setup") != nil
    }

    private func createDemoAccountForDeepLink() async {
        guard let current = try? await env.api.me() else { return }
        let request = SignupRequest(name: current.name, email: current.email, password: "demo-pass-1",
                                    username: current.username, dateOfBirth: nil)
        guard let response = try? await env.api.signup(request) else { return }
        env.startSession(response)
        draft.name = current.name
        draft.email = current.email
        draft.signedUp = true
        draft.integrations = .loading // the new account's calendars, not the old ones
    }
}

#Preview {
    SetupFlowView(startStep: 1, isRedo: false)
        .environment(AppEnvironment.preview())
        .environment(Router(phase: .auth))
}
