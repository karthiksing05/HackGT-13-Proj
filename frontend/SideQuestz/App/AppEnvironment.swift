import Observation
import SwiftUI
import UIKit

/// App-wide services and session state. One instance, injected with `.environment(env)` and read
/// in views as `@Environment(AppEnvironment.self) private var env`.
///
/// Live is the default: the app talks to the server named in Info.plist (`SQAPIMode = live`,
/// `SQAPIBaseURL`, `SQWebSocketURL`). The offline demo backend is one launch argument away
/// (`-SQAPIMode mock`).
@Observable
final class AppEnvironment {
    enum Mode: String { case mock, live }

    /// Sandy Byte, the account seeded on the live server (Login › "Use the demo account").
    static let demoEmail = "demo@sidequestz.tech"

    let mode: Mode
    let clock: AppClock
    let api: any APIClient
    let auth: AuthStore
    let realtime: RealtimeHub
    let voice: VoiceInput
    let places: PlaceSearch
    let location: LocationService
    /// The demo account's password when this build or launch was given one (`SQ_DEMO_PASSWORD`
    /// build setting, `-SQDemoPassword`); nil hides the demo link on Login.
    let demoPassword: String?
    @ObservationIgnored private let socket: WebSocketService?
    /// Screens that are alive right now and how each reloads its data (pull to refresh).
    @ObservationIgnored private var reloaders: [String: @MainActor () async -> Void] = [:]

    // MARK: Session (shared by Home header, Account, Setup, Photo sheet…)

    var user: User?
    /// The profile photo picked on this device (shown immediately while/after uploading).
    var profileImage: UIImage?
    var preferences: Preferences?

    var isMock: Bool { mode == .mock }
    var format: TimeFormat { TimeFormat(clock: clock) }

    init(mode: Mode, clock: AppClock, api: any APIClient, auth: AuthStore, socketURL: URL?, forceVoiceDemo: Bool,
         demoPassword: String? = nil) {
        self.mode = mode
        self.clock = clock
        self.api = api
        self.auth = auth
        self.demoPassword = demoPassword
        let hub = RealtimeHub()
        self.realtime = hub
        self.voice = VoiceInput(allowDemoFallback: mode == .mock, forceDemo: forceVoiceDemo)
        self.places = PlaceSearch(api: api, isMock: mode == .mock)
        self.location = LocationService(isMock: mode == .mock)
        self.socket = mode == .live ? socketURL.map { url in
            WebSocketService(url: url, hub: hub, timeZone: clock.calendar.timeZone) { [auth] in auth.tokens?.accessToken }
        } : nil
        (api as? MockAPIClient)?.realtime = hub
        (api as? LiveAPIClient)?.onUnauthorized = { [weak self] in self?.sessionDidExpire() }
    }

    /// Reads Info.plist (`SQAPIMode`, `SQAPIBaseURL`, `SQWebSocketURL`, `SQDemoPassword`) and launch
    /// arguments (`-SQAPIMode mock`, `-SQMockFail forum`, `-SQMockLatency 0`, `-SQVoiceDemo YES`,
    /// `-SQDemoPassword …`). A launch argument wins over the plist; both fall back to the live server.
    static func makeDefault() -> AppEnvironment {
        let info = Bundle.main.infoDictionary ?? [:]
        let defaults = UserDefaults.standard
        let modeString = defaults.string(forKey: "SQAPIMode") ?? info["SQAPIMode"] as? String ?? "live"
        let mode = Mode(rawValue: modeString) ?? .live
        let auth = AuthStore()
        let clock: AppClock = mode == .mock ? .demo : .live
        let forceVoiceDemo = defaults.bool(forKey: "SQVoiceDemo")

        let api: any APIClient
        switch mode {
        case .mock:
            let failing = Set((defaults.string(forKey: "SQMockFail") ?? "").split(separator: ",").map { String($0).trimmingCharacters(in: .whitespaces) })
            let latency = defaults.object(forKey: "SQMockLatency") != nil ? defaults.double(forKey: "SQMockLatency") : 1
            api = MockAPIClient(clock: clock, latencyScale: latency, failing: failing)
        case .live:
            let base = URL(string: defaults.string(forKey: "SQAPIBaseURL") ?? info["SQAPIBaseURL"] as? String ?? "") ?? URL(string: "https://api.sidequestz.tech")!
            api = LiveAPIClient(baseURL: base, auth: auth, clock: clock)
        }
        let socketURL = URL(string: defaults.string(forKey: "SQWebSocketURL") ?? info["SQWebSocketURL"] as? String ?? "")
            ?? URL(string: "wss://api.sidequestz.tech/ws")
        let demoPassword = demoPassword(launch: defaults.string(forKey: "SQDemoPassword"), info: info["SQDemoPassword"] as? String)
        return AppEnvironment(mode: mode, clock: clock, api: api, auth: auth, socketURL: socketURL, forceVoiceDemo: forceVoiceDemo,
                              demoPassword: demoPassword)
    }

    /// The demo account's password from `-SQDemoPassword` (`launch`), else Info.plist's
    /// `SQDemoPassword` (`info`, the `SQ_DEMO_PASSWORD` build setting). Values are trimmed and a
    /// blank one counts as not given, so an empty build setting leaves the demo link hidden.
    static func demoPassword(launch: String?, info: String?) -> String? {
        for candidate in [launch, info] {
            if let value = candidate?.trimmingCharacters(in: .whitespacesAndNewlines), !value.isEmpty { return value }
        }
        return nil
    }

    /// Test/preview environment: mock backend, no delay.
    static func preview() -> AppEnvironment {
        let env = AppEnvironment(mode: .mock, clock: .demo, api: MockAPIClient(clock: .demo, latencyScale: 0), auth: AuthStore(service: "preview"),
                                 socketURL: nil, forceVoiceDemo: true)
        env.user = MockData.user()
        env.preferences = MockData.preferences
        return env
    }

    // MARK: Session lifecycle

    /// After login / signup: store tokens, remember the user, connect realtime.
    func startSession(_ response: AuthResponse) {
        auth.save(response.tokens)
        user = response.user
        socket?.connect()
        Task { await refreshSession() }
    }

    /// Mock-only shortcut used by demo launch routes: signs in as Jordan Lee.
    func startDemoSessionIfNeeded() {
        guard isMock else { return }
        if !auth.isSignedIn {
            auth.save(AuthTokens(accessToken: "mock-access", refreshToken: "mock-refresh", expiresAt: nil))
        }
        if user == nil { user = MockData.user() }
    }

    /// Reloads the current user and preferences (on launch when already signed in).
    func refreshSession() async {
        // Both at once, so a pull to refresh waits for one round trip, not two.
        let meCall = Task { try await api.me() }
        let prefsCall = Task { try await api.preferences() }
        if let me = try? await meCall.value { user = me }
        if let prefs = try? await prefsCall.value { preferences = prefs }
        if !isMock, auth.isSignedIn { socket?.connect() }
    }

    // MARK: Pull to refresh

    /// A screen that shows API data registers how to reload it (see `sqReloadable`).
    func registerReload(_ key: String, _ reload: @escaping @MainActor () async -> Void) {
        reloaders[key] = reload
    }

    func unregisterReload(_ key: String) {
        reloaders[key] = nil
    }

    /// Pull to refresh anywhere: reloads the profile and every live screen's data in parallel, and
    /// returns when all of it is back (so the spinner lasts exactly as long as the requests).
    func reloadAll() async {
        let jobs = Array(reloaders.values)
        await withTaskGroup(of: Void.self) { group in
            group.addTask { await self.refreshSession() }
            for job in jobs {
                group.addTask { await job() }
            }
        }
    }

    /// Set when the server ended the session (the refresh token stopped working). `RootView` sends
    /// the app back to sign-in and Login explains why.
    var sessionExpired = false

    private func sessionDidExpire() {
        socket?.disconnect()
        voice.cancel()
        user = nil
        profileImage = nil
        preferences = nil
        sessionExpired = true
    }

    func signOut() async {
        try? await api.logout()
        auth.clear()
        socket?.disconnect()
        voice.cancel()
        user = nil
        profileImage = nil
        preferences = nil
    }

}
