import SwiftUI

/// Routes Splash (first launch) → Auth (Login, Forgot password, Profile setup) → Main.
struct RootView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router

    var body: some View {
        GeometryReader { proxy in
            ZStack {
                Theme.cream.ignoresSafeArea()
                switch router.phase {
                case .splash:
                    SplashView {
                        if !env.auth.isSignedIn {
                            router.showLogin()
                        } else if env.user?.setupComplete == false {
                            router.resumeSetup()
                        } else {
                            router.enterMain()
                        }
                    }
                    .transition(.opacity)
                case .auth:
                    AuthFlowView()
                        .transition(.opacity)
                case .main:
                    MainShell()
                        .transition(.opacity)
                }
                if router.showsDesignGallery {
                    DesignGalleryView().zIndex(10)
                }
            }
            .environment(\.safeAreaTop, proxy.safeAreaInsets.top)
            .environment(\.safeAreaBottom, proxy.safeAreaInsets.bottom)
            // A stored session whose account never finished setup (closed mid-setup): resume it
            // once `GET /me` says so.
            .onChange(of: env.user?.setupComplete) { _, complete in
                guard complete == false, router.phase == .main, env.auth.isSignedIn else { return }
                router.resumeSetup()
            }
            // The server ended the session: back to sign-in, which says why.
            .onChange(of: env.sessionExpired) { _, expired in
                guard expired else { return }
                env.sessionExpired = false
                router.authNotice = "Your session expired. Sign in again."
                router.signedOut()
            }
        }
    }
}

/// Login ⇄ Forgot password ⇄ Profile setup. Forgot and Setup are full-screen flows.
struct AuthFlowView: View {
    @Environment(Router.self) private var router

    var body: some View {
        ZStack {
            switch router.authRoute {
            case .login:
                LoginView()
                    .transition(.asymmetric(insertion: .move(edge: .leading), removal: .move(edge: .leading)).combined(with: .opacity))
            case .forgot:
                ForgotPasswordFlow()
                    .transition(.move(edge: .trailing).combined(with: .opacity))
            case .setup:
                SetupFlowView(startStep: router.setupStartStep, isRedo: false)
                    .transition(.move(edge: .trailing).combined(with: .opacity))
            }
        }
        .background(Theme.cream.ignoresSafeArea())
    }
}
