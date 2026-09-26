import SwiftUI

/// Routes Splash (first launch) → Auth (Login, Forgot password, Profile setup) → Main.
struct RootView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router
    @AppStorage("hasSeenIntro") private var hasSeenIntro = false

    var body: some View {
        GeometryReader { proxy in
            ZStack {
                Theme.cream.ignoresSafeArea()
                switch router.phase {
                case .splash:
                    SplashView {
                        hasSeenIntro = true
                        if env.auth.isSignedIn { router.enterMain() } else { router.showLogin() }
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
