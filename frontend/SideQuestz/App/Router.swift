import Observation
import SwiftUI

/// Where the app is. Owned by `SideQuestzApp`, read with `@Environment(Router.self) private var router`.
///
/// Root: Splash (first launch) → Auth (Login, Forgot password, Profile setup) → Main.
/// Main is the current tab + the custom tab bar. Full-screen flows hide the tab bar:
/// Create (fullScreenCover), Thread (group chat / DM overlay), Setup redo (fullScreenCover).
@Observable
final class Router {
    enum Phase: Equatable { case splash, auth, main }
    enum AuthRoute: Equatable { case login, forgot, setup }
    enum Tab: Hashable, CaseIterable { case home, forum, groups, account }
    enum HomeSegment: Hashable, CaseIterable { case itineraries, calendar, past }
    enum AccountSegment: Hashable { case me, friends }

    var phase: Phase
    var authRoute: AuthRoute = .login
    /// Email typed on Login, carried into Forgot password and Setup (and back).
    var authEmail = ""
    /// Setup starts here (1…5) when entered from Login.
    var setupStartStep = 1

    var tab: Tab = .home
    var homeSegment: HomeSegment = .itineraries
    var accountSegment: AccountSegment = .me
    /// Home selects (and scrolls to) this itinerary when set — e.g. right after Create.
    var selectedItineraryId: String?

    /// Non-nil → Create flow is presented full screen.
    var createDraft: CreateDraft?
    /// Non-nil → a thread (group chat or DM) covers the tabs.
    var openThread: ThreadRoute?
    /// Non-nil → "Redo setup questions" presented full screen while signed in.
    var setupRedo: SetupEntry?

    /// Demo deep link from `-SQRoute …`; features consume the parts meant for them.
    private(set) var pendingLaunch: [String]?
    /// `-SQRoute gallery` shows the design-system gallery (development aid).
    var showsDesignGallery = false

    init(phase: Phase) {
        self.phase = phase
    }

    // MARK: Navigation

    func showLogin(email: String? = nil) {
        if let email { authEmail = email }
        withAnimation(.easeInOut(duration: 0.3)) {
            authRoute = .login
            phase = .auth
        }
    }

    func showForgotPassword() {
        withAnimation(.easeInOut(duration: 0.3)) { authRoute = .forgot }
    }

    func showSetup(step: Int = 1) {
        setupStartStep = step
        withAnimation(.easeInOut(duration: 0.3)) { authRoute = .setup }
    }

    func enterMain() {
        withAnimation(.easeInOut(duration: 0.35)) {
            tab = .home
            phase = .main
        }
    }

    func signedOut() {
        createDraft = nil
        openThread = nil
        setupRedo = nil
        tab = .home
        homeSegment = .itineraries
        showLogin()
    }

    func select(_ tab: Tab) {
        self.tab = tab
    }

    func openCreate(_ draft: CreateDraft = CreateDraft()) {
        createDraft = draft
    }

    func openThread(_ id: String, tab: ThreadTab = .chat) {
        withAnimation(.easeInOut(duration: 0.28)) { openThread = ThreadRoute(threadId: id, tab: tab) }
    }

    func closeThread() {
        withAnimation(.easeInOut(duration: 0.28)) { openThread = nil }
    }

    /// Account › "See all & rate" and the Home "N past events to rate" card.
    func showPast() {
        tab = .home
        homeSegment = .past
    }

    // MARK: Demo deep links

    /// Applies `-SQRoute` (see `LaunchRoute`). Top-level parts are handled here; the rest waits in
    /// `pendingLaunch` for the feature to consume with `consumeLaunch(_:)`.
    func applyLaunch(_ route: LaunchRoute, env: AppEnvironment) {
        let parts = route.parts
        guard let head = parts.first else { return }
        switch head {
        case "gallery":
            env.startDemoSessionIfNeeded()
            showsDesignGallery = true
            return
        case "splash":
            phase = .splash
            return
        case "login":
            phase = .auth
            authRoute = .login
            return
        case "forgot":
            phase = .auth
            authRoute = .forgot
            authEmail = "jordan@gatech.edu"
        case "setup":
            phase = .auth
            authRoute = .setup
            setupStartStep = Int(parts.dropFirst().first ?? "1") ?? 1
        default:
            env.startDemoSessionIfNeeded()
            phase = .main
            switch head {
            case "home":
                tab = .home
                switch parts.dropFirst().first {
                case "calendar": homeSegment = .calendar
                case "past": homeSegment = .past
                default: homeSegment = .itineraries
                }
            case "create":
                tab = .home
                let step = Int(parts.dropFirst().first ?? "1") ?? 1
                createDraft = CreateDraft(step: step)
            case "forum":
                tab = .forum
            case "groups":
                tab = .groups
            case "thread":
                let id = parts.dropFirst().first ?? "g1"
                tab = id.hasPrefix("dm-") ? .account : .groups
                if id.hasPrefix("dm-") { accountSegment = .friends }
                let tabName = parts.dropFirst(2).first ?? "chat"
                openThread = ThreadRoute(threadId: id, tab: ThreadTab(rawValue: tabName) ?? .chat)
            case "account":
                tab = .account
                accountSegment = parts.dropFirst().first == "friends" ? .friends : .me
            default:
                break
            }
        }
        pendingLaunch = parts
    }

    /// Returns the launch parts after `prefix` (e.g. `consumeLaunch("home")` → ["calendar"]) and
    /// clears them, or nil when the pending route isn't for this feature.
    func consumeLaunch(_ prefix: String) -> [String]? {
        guard let parts = pendingLaunch, parts.first == prefix else { return nil }
        pendingLaunch = nil
        return Array(parts.dropFirst())
    }

    /// Non-destructive check (for views that need to know before they consume).
    func peekLaunch(_ prefix: String) -> [String]? {
        guard let parts = pendingLaunch, parts.first == prefix else { return nil }
        return Array(parts.dropFirst())
    }
}

/// Create flow presentation + prefill (Calendar › "Plan this window" passes date, start and end).
struct CreateDraft: Identifiable, Equatable {
    let id = UUID()
    var date: Date?
    var start: Date?
    var end: Date?
    /// 1 Where · 2 When · 3 Vibe · 4 Review
    var step = 1
}

enum ThreadTab: String, CaseIterable, Identifiable {
    case chat, album, splits
    var id: String { rawValue }
    var label: String { rawValue.capitalized }
}

struct ThreadRoute: Identifiable, Equatable {
    var id: String { threadId }
    let threadId: String
    var tab: ThreadTab = .chat
}

struct SetupEntry: Identifiable, Equatable {
    let id = UUID()
    var step: Int
}

/// Demo deep links for screenshots and judging, like the prototype's gallery artboards.
///
/// Launch with `-SQRoute <path>` (e.g. `xcrun simctl launch <device> com.karthiksing05.SideQuestz -SQRoute home/calendar`).
/// Any route past auth signs in as the demo user. Paths:
/// - `splash`, `login`, `forgot/1…4`, `setup/1…5`
/// - `home`, `home/calendar`, `home/past`, `home/sheet/<blockId>`, `home/rate/<pastId>`, `home/checkout/<blockId>`
/// - `create/1…4`, `create/2/calendar`, `create/4/more`
/// - `forum`, `forum/area`, `forum/filter`
/// - `groups`, `thread/<id>/<chat|album|splits>`, `thread/g1/splits/expense`, `thread/dm-maya`
/// - `account`, `account/friends`, `account/photo`
struct LaunchRoute: Equatable {
    var parts: [String]

    init?(_ string: String?) {
        guard let string, !string.isEmpty else { return nil }
        parts = string.split(separator: "/").map(String.init)
    }

    static func fromLaunchArguments() -> LaunchRoute? {
        LaunchRoute(UserDefaults.standard.string(forKey: "SQRoute"))
    }
}
