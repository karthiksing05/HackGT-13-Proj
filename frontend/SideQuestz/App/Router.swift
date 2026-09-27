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
    /// A one-time note for the sign-in screen ("Your session expired. Sign in again.").
    var authNotice: String?
    /// An invite link that was opened (`sidequestz://invite/<code>`); accepted once you're signed in.
    var pendingInvite: String?
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
    /// Non-nil → "Let Muse get your tickets" for this plan (agentic checkout).
    var agentCheckout: AgentCheckoutRoute?
    /// Non-nil → Account › Your tickets slides over the Account tab (`TicketsScreenHost`).
    var tickets: TicketsRoute?
    /// Your tickets › "Go to sidequest": Home opens this stop once it has selected its plan.
    var stopToOpen: String?

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

    /// Signed in, but the account never finished Profile setup (the app was closed mid-setup):
    /// continue at step 2 (the account itself exists).
    func resumeSetup() {
        createDraft = nil
        openThread = nil
        setupRedo = nil
        setupStartStep = 2
        withAnimation(.easeInOut(duration: 0.3)) {
            authRoute = .setup
            phase = .auth
        }
    }

    func enterMain() {
        withAnimation(.easeInOut(duration: 0.35)) {
            tab = .home
            phase = .main
        }
    }

    /// Links into the app. Invites: `sidequestz://invite/<code>` or `https://sidequests.app/invite/<code>`.
    func handleOpenURL(_ url: URL) {
        let parts = ([url.host].compactMap { $0 } + url.pathComponents).filter { $0 != "/" }
        guard let index = parts.firstIndex(of: "invite"), index + 1 < parts.count else { return }
        pendingInvite = parts[index + 1]
    }

    func signedOut() {
        agentCheckout = nil
        tickets = nil
        stopToOpen = nil
        createDraft = nil
        openThread = nil
        setupRedo = nil
        tab = .home
        homeSegment = .itineraries
        showLogin()
    }

    /// Switches tabs with a cross-fade (tabs stay alive underneath, so this is just opacity).
    func select(_ tab: Tab) {
        guard tab != self.tab else { return }
        withMotion(Motion.standard) { self.tab = tab }
    }

    func openCreate(_ draft: CreateDraft = CreateDraft()) {
        createDraft = draft
    }

    func openThread(_ id: String, tab: ThreadTab = .chat, isGroup: Bool? = nil) {
        withAnimation(.easeInOut(duration: 0.28)) { openThread = ThreadRoute(threadId: id, tab: tab, isGroup: isGroup) }
    }

    func closeThread() {
        withAnimation(.easeInOut(duration: 0.28)) { openThread = nil }
    }

    /// Account › "See all & rate" and the Home "N past events to rate" card.
    func showPast() {
        tab = .home
        homeSegment = .past
    }

    /// Account › Your tickets (`ticketId`: with that ticket open).
    func showTickets(ticketId: String? = nil) {
        withAnimation(.easeInOut(duration: 0.28)) { tickets = TicketsRoute(ticketId: ticketId) }
    }

    func closeTickets() {
        withAnimation(.easeInOut(duration: 0.28)) { tickets = nil }
    }

    /// Your tickets › "Go to sidequest": Home, the plan selected (`selectedItineraryId`) and the
    /// stop's sheet open (`stopToOpen`).
    func goToStop(_ itemId: String, itineraryId: String) {
        stopToOpen = itemId
        select(.home)
        homeSegment = .itineraries
        selectedItineraryId = itineraryId
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
            // Screens past sign-in. Mock mode signs in the demo user; live mode has no one to sign
            // in as, so without a stored session the link lands on Login and the route is dropped.
            env.startDemoSessionIfNeeded()
            guard env.auth.isSignedIn else {
                phase = .auth
                authRoute = .login
                return
            }
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
            case "forum", "profile":
                // A profile opens over the Forum (`ForumView` consumes `profile/<userId>`).
                tab = .forum
            case "groups":
                tab = .groups
            case "thread":
                let id = parts.dropFirst().first ?? "g1"
                tab = id.hasPrefix("dm-") ? .account : .groups
                if id.hasPrefix("dm-") { accountSegment = .friends }
                let tabName = parts.dropFirst(2).first ?? "chat"
                // Demo DM ids start with "dm-"; everything else in the demo is a group.
                openThread = ThreadRoute(threadId: id, tab: ThreadTab(rawValue: tabName) ?? .chat, isGroup: !id.hasPrefix("dm-"))
            case "account":
                tab = .account
                accountSegment = parts.dropFirst().first == "friends" ? .friends : .me
                if parts.dropFirst().first == "tickets" { tickets = TicketsRoute() }
            case "tickets":
                // `tickets/<ticketId>`: Account › Your tickets with that ticket open.
                tab = .account
                tickets = TicketsRoute(ticketId: parts.dropFirst().first)
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
    /// Home search › a place: plan a sidequest that ends there (prefills the End pin).
    var destination: Place?
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
    /// Known before the thread loads (from the list row), so the header doesn't guess.
    var isGroup: Bool? = nil
}

struct SetupEntry: Identifiable, Equatable {
    let id = UUID()
    var step: Int
}

/// Demo deep links for screenshots and judging, like the prototype's gallery artboards.
///
/// Launch with `-SQRoute <path>` (e.g. `xcrun simctl launch <device> com.karthiksing05.SideQuestz -SQAPIMode mock -SQRoute home/calendar`).
/// Any route past auth signs in as the demo user, so these need mock mode (live and signed out,
/// they land on Login). Paths:
/// - `splash`, `login`, `forgot/1…4`, `setup/1…5`
/// - `home`, `home/calendar`, `home/past`, `home/sheet/<blockId>`, `home/rate/<pastId>`, `home/checkout/<blockId>`
/// - `create/1…4`, `create/2/calendar`, `create/4/more`, `create/4/swap`
/// - `forum`, `forum/area`, `forum/filter`, `forum/friends`
/// - `profile/<userId>` (someone's profile over the Forum, e.g. `profile/u-mr`)
/// - `groups`, `thread/<id>/<chat|album|splits>`, `thread/g1/splits/expense`, `thread/dm-maya`
/// - `account`, `account/friends`, `account/photo`, `account/facebook`, `account/tickets`, `tickets/<ticketId>`
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
