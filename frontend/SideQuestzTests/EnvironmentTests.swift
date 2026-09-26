import Foundation
import Testing
@testable import SideQuestz

/// How the app reads its launch configuration: the demo password, and where a deep link may land.
@MainActor
struct EnvironmentTests {
    /// `-SQDemoPassword` wins over Info.plist's `SQDemoPassword`; both are trimmed, and a blank
    /// value (the empty `SQ_DEMO_PASSWORD` build setting) means there is no demo account.
    @Test func demoPasswordParsing() {
        #expect(AppEnvironment.demoPassword(launch: "launch-pw", info: "plist-pw") == "launch-pw")
        #expect(AppEnvironment.demoPassword(launch: nil, info: "plist-pw") == "plist-pw")
        #expect(AppEnvironment.demoPassword(launch: "  ", info: "plist-pw") == "plist-pw")
        #expect(AppEnvironment.demoPassword(launch: " launch-pw\n", info: nil) == "launch-pw")
        #expect(AppEnvironment.demoPassword(launch: nil, info: " plist-pw ") == "plist-pw")
        #expect(AppEnvironment.demoPassword(launch: nil, info: "") == nil)
        #expect(AppEnvironment.demoPassword(launch: "", info: " \t") == nil)
        #expect(AppEnvironment.demoPassword(launch: nil, info: nil) == nil)
        #expect(AppEnvironment.demoEmail == "demo@sidequestz.tech")
    }

    /// `-SQRoute` past sign-in: the demo session only exists in mock mode. Live and signed out, the
    /// link lands on Login with nothing left to consume; a stored live session applies it as usual.
    @Test func liveDeepLinksNeedASession() {
        let auth = AuthStore(service: "tests.environment.live")
        auth.clear()
        let api = LiveAPIClient(baseURL: URL(string: "https://api.sidequestz.tech")!, auth: auth, clock: .live)
        let env = AppEnvironment(mode: .live, clock: .live, api: api, auth: auth, socketURL: nil, forceVoiceDemo: true)
        #expect(env.demoPassword == nil)

        let signedOut = Router(phase: .auth)
        signedOut.applyLaunch(LaunchRoute("home/calendar")!, env: env)
        #expect(signedOut.phase == .auth)
        #expect(signedOut.authRoute == .login)
        #expect(signedOut.peekLaunch("home") == nil)
        #expect(!env.auth.isSignedIn && env.user == nil)

        auth.save(AuthTokens(accessToken: "stored", refreshToken: "stored", expiresAt: nil))
        let signedIn = Router(phase: .main)
        signedIn.applyLaunch(LaunchRoute("account/friends")!, env: env)
        #expect(signedIn.phase == .main)
        #expect(signedIn.tab == .account && signedIn.accountSegment == .friends)
        auth.clear()

        let mock = Router(phase: .auth)
        mock.applyLaunch(LaunchRoute("home/calendar")!, env: AppEnvironment.preview())
        #expect(mock.phase == .main)
        #expect(mock.homeSegment == .calendar)
        #expect(mock.peekLaunch("home") == ["calendar"])
    }
}
