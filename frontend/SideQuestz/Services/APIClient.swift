import Foundation

/// The backend contract (GUI_PLAN.md §9, Backend/API_ENDPOINTS.md).
///
/// Two implementations: `MockAPIClient` (default for the demo, fully offline, stateful) and
/// `LiveAPIClient` (REST + JSON over URLSession). `AppEnvironment` decides which one to use.
/// Screens only ever talk to this protocol.
///
/// The server owns plan generation, transit re-timing, equal-split math, age filtering, join
/// limits and taste-profile updates. The client previews and validates but shows server results.
protocol APIClient: AnyObject {
    // MARK: Auth
    func signup(_ request: SignupRequest) async throws -> AuthResponse
    func login(email: String, password: String) async throws -> AuthResponse
    func refresh(refreshToken: String) async throws -> AuthTokens
    func logout() async throws
    /// Always succeeds (never reveals whether the email exists).
    func forgotPassword(email: String) async throws
    /// Returns a short-lived reset token.
    func verifyResetCode(email: String, code: String) async throws -> String
    /// Signs out other sessions.
    func resetPassword(resetToken: String, newPassword: String) async throws
    func resendResetCode(email: String) async throws

    // MARK: Me
    func me() async throws -> User
    func updateMe(_ patch: UserPatch) async throws -> User
    func uploadPhoto(_ jpegData: Data) async throws -> URL
    func deletePhoto() async throws
    func setAvatarColor(_ color: AvatarColor) async throws
    func preferences() async throws -> Preferences
    func savePreferences(_ preferences: Preferences) async throws
    func tasteProfile() async throws -> TasteProfile
    func registerDevice(pushToken: String) async throws
    /// Sign-out: stop pushes to this device.
    func unregisterDevice(pushToken: String) async throws

    // MARK: Integrations + payments
    func integrations() async throws -> [Integration]
    /// Returns the OAuth URL to open in ASWebAuthenticationSession.
    func connectIntegration(_ provider: CalendarProvider) async throws -> URL
    /// Mock only needs this to mark a provider connected after OAuth; live marks it server-side.
    func completeIntegration(_ provider: CalendarProvider) async throws
    func disconnectIntegration(_ provider: CalendarProvider) async throws
    func paymentMethods() async throws -> [PaymentMethod]
    /// Adding a card happens on a hosted page, so card numbers never touch the app: open this URL
    /// in `ASWebAuthenticationSession`; it redirects to `sidequestz://payments/done`, then reload
    /// `paymentMethods()`.
    func paymentSetupURL() async throws -> URL
    /// For a token made by a payment SDK (not used by the hosted flow).
    func addPaymentMethod(token: String) async throws -> PaymentMethod
    func deletePaymentMethod(id: String) async throws

    // MARK: Facebook (Graph API): taste context for preferences
    // The server does the Facebook Login exchange, keeps the token, reads the Graph API and stores
    // what it read. Use `FacebookConnector.connect(env:)` rather than calling these one by one.

    /// Whether Facebook is connected, and the latest import.
    func facebookConnection() async throws -> FacebookConnection
    /// Facebook's Login dialog, built by the server (with its `state` and redirect). Open it in
    /// `ASWebAuthenticationSession`; the server's callback redirects to
    /// `sidequestz://integrations/facebook?status=connected|denied|error`. `rerequest` asks again for
    /// permissions the person turned off last time.
    func facebookConnectURL(rerequest: Bool) async throws -> URL
    /// Reads Facebook now (liked Pages, city, friends on SideQuests), stores it and returns what it
    /// suggests. Nothing changes in the person's preferences until they save them.
    func importFacebook() async throws -> FacebookImport
    /// Revokes SideQuests on Facebook and deletes the token and everything imported. Preferences the
    /// person already saved stay.
    func disconnectFacebook() async throws

    // MARK: Calendar / places / events
    func calendarDays(from: Date, to: Date) async throws -> [CalendarDay]
    func searchPlaces(query: String, near: Coordinate?) async throws -> [Place]
    func reverseGeocode(_ coordinate: Coordinate) async throws -> Place
    /// Full details for any block id (itinerary items, calendar entries, catalog events).
    func eventDetail(id: String) async throws -> ItineraryItem
    /// Home search: your sidequests, people, places and Forum posts matching `query`.
    func search(query: String) async throws -> SearchResults

    // MARK: Planning
    /// Create › Vibe › Must-see: events and places in the user's catalog matching `q` (an empty `q`
    /// suggests that day's events, then the best places near `near`). `near` is the plan's start,
    /// `date` the plan's day; `limit` at most 50.
    func searchActivities(q: String, near: Coordinate?, date: Date?, limit: Int) async throws -> [ActivityHit]
    /// First 3 options + cursor. Every option includes every `request.mustInclude` pick, or the batch
    /// is empty with a `must_include_…` reason.
    func generatePlans(_ request: PlanRequest) async throws -> PlanBatch
    /// 2 more options; `done` when out.
    func moreOptions(cursor: String) async throws -> PlanBatch
    /// Recalculates legs, stop times, arrival and lateness after a reorder, swap or removal.
    func route(_ request: RouteRequest) async throws -> RouteResult
    /// Similar things that could take `stopId`'s place in the option (Review › hold a stop › Swap).
    /// `stopOrder` is the option's current order, so suggestions fit around the other stops.
    func stopAlternatives(optionId: String, stopId: String, stopOrder: [String]) async throws -> [PlanAlternative]
    /// Review › tap a stop: what the catalog knows about the stop's activity (`PlanStop.activityId`),
    /// with the opening hours on `date` (the plan's day; the account's today when nil). An id the
    /// user's catalog doesn't have, or a server without this endpoint, is `APIError.notFound`.
    func activity(id: String, date: Date?) async throws -> ActivityDetail
    func createItinerary(_ request: CreateItineraryRequest) async throws -> Itinerary

    // MARK: Itineraries
    func activeItineraries() async throws -> [Itinerary]
    func itinerary(id: String) async throws -> Itinerary
    /// Edit a plan you host (`PATCH /itineraries/{id}`, only the set fields). The server re-times the
    /// route when the date, window or stops change and returns the updated itinerary.
    func updateItinerary(id: String, _ update: ItineraryUpdate) async throws -> Itinerary
    /// Delete a plan you host; everyone who joined loses it too.
    func deleteItinerary(id: String) async throws
    /// Leave a plan someone else hosts (`POST /itineraries/{id}/leave`).
    func leaveItinerary(id: String) async throws
    /// `scope` nil keeps the item's current scope.
    func updateItemNotes(itineraryId: String?, itemId: String, notes: String, scope: NotesScope?) async throws
    func transitOptions(itineraryId: String?, itemId: String) async throws -> [TransitOption]
    /// Saves the "Getting there" choice (`ItineraryItem.transitMode`).
    func selectTransit(itineraryId: String?, itemId: String, mode: TravelMode) async throws

    // MARK: Past + ratings
    func pastEvents(unratedOnly: Bool) async throws -> [PastEvent]
    /// What the sidequests you liked have in common (Home › Past, top card).
    func pastInsights() async throws -> PastInsights
    func rate(itemId: String, rating: Rating) async throws

    // MARK: Checkout (single item)
    /// `paymentMethodId` nil = the default card. The intent usually starts `preparing`; follow it with
    /// `checkout.status` events or `checkoutIntent(id:)` until it's `awaitingApproval`.
    /// `instant`: the user has instant checkout on; the server skips approval when the total is
    /// within their limit (`CheckoutIntent.instant` says whether it did).
    func createCheckoutIntent(itemId: String, quantity: Int, paymentMethodId: String?, instant: Bool) async throws -> CheckoutIntent
    func checkoutIntent(id: String) async throws -> CheckoutIntent
    /// The only step that spends money.
    /// Checkout › "Change": pay with another saved card.
    func updateCheckoutIntent(id: String, paymentMethodId: String) async throws -> CheckoutIntent
    func approveCheckout(id: String) async throws -> CheckoutIntent
    func cancelCheckout(id: String) async throws

    // MARK: Agentic checkout (Muse buys a plan's tickets; Stripe test mode)
    /// What Muse would buy for this plan, the suggested budget and the card.
    func checkoutPlan(itineraryId: String) async throws -> CheckoutPlan
    /// Starts Muse on the approved items within `budgetCents` (after Face ID). 409 when the plan is
    /// already booked or a run is going; follow it with `checkout.run` events or `checkoutRun(id:)`.
    func startCheckoutRun(itineraryId: String, _ request: CreateCheckoutRun) async throws -> CheckoutRun
    func checkoutRun(id: String) async throws -> CheckoutRun
    /// Stops what hasn't been bought yet; tickets already bought stay.
    func cancelCheckoutRun(id: String) async throws -> CheckoutRun

    // MARK: Forum
    func forumPosts(_ query: ForumQuery) async throws -> [ForumPost]
    /// `GET /forum/posts/mine`: your live "I'm free" post, or nil.
    func myFreePost() async throws -> MyFreePost?
    func postFreeNow(_ post: NewFreePost) async throws -> MyFreePost
    func deleteForumPost(id: String) async throws
    /// Returns where you stand: `requested`, or `joined` when the plan auto-accepts (then the plan
    /// and its group chat exist).
    @discardableResult
    func requestToJoin(postId: String) async throws -> JoinResult
    func cancelJoinRequest(postId: String) async throws
    /// Sends a "plan together" message and returns the DM thread.
    func planTogether(postId: String) async throws -> ChatThread

    // MARK: Threads
    func threads() async throws -> [ChatThread]
    func thread(id: String) async throws -> ChatThread
    func messages(threadId: String, before: String?) async throws -> [Message]
    /// `clientId`: an id this device made up; resending with the same one never posts twice, and
    /// the server echoes it on the message (and its `message.new` event).
    func sendMessage(threadId: String, text: String, clientId: String?) async throws -> Message
    /// Clears the thread's unread count (call when it's on screen).
    func markThreadRead(id: String) async throws
    func startDM(userId: String) async throws -> ChatThread

    // MARK: Group album
    func groupPhotos(groupId: String) async throws -> [GroupPhoto]
    func uploadGroupPhoto(groupId: String, jpegData: Data) async throws -> GroupPhoto
    func deleteGroupPhoto(groupId: String, photoId: String) async throws

    // MARK: Group splits (equal)
    func ledger(groupId: String) async throws -> GroupLedger
    func addExpense(groupId: String, _ expense: NewExpense) async throws -> Expense
    func deleteExpense(groupId: String, expenseId: String) async throws
    /// Pays what the Splits screen showed. The server rejects a stale amount (409) so nobody is
    /// charged something they didn't see. `paymentMethodId` nil = the default card.
    func settleUp(groupId: String, amountCents: Int, paymentMethodId: String?) async throws

    // MARK: Friends
    func friends() async throws -> [Friend]
    /// Each result says how they relate to you (friend, requested, asked you).
    func searchUsers(query: String) async throws -> [UserSearchResult]
    /// People whose taste matches yours, best first, with a match percent (not you, not friends).
    func suggestedPeople() async throws -> [PersonSuggestion]
    func friendRequests() async throws -> [FriendRequest]
    /// Returns your outgoing request, so it can be cancelled.
    @discardableResult
    func sendFriendRequest(userId: String) async throws -> FriendRequest
    func cancelFriendRequest(id: String) async throws
    func acceptFriendRequest(id: String) async throws
    func declineFriendRequest(id: String) async throws
    func removeFriend(id: String) async throws
    func createInvite() async throws -> URL
    /// Opening someone's invite link (`sidequestz://invite/<code>` or `https://sidequests.app/invite/<code>`).
    func acceptInvite(code: String) async throws -> Friend
}

/// The shorter forms screens used before the fuller calls existed.
extension APIClient {
    func updateItemNotes(itineraryId: String?, itemId: String, notes: String) async throws {
        try await updateItemNotes(itineraryId: itineraryId, itemId: itemId, notes: notes, scope: nil)
    }

    func createCheckoutIntent(itemId: String, quantity: Int) async throws -> CheckoutIntent {
        try await createCheckoutIntent(itemId: itemId, quantity: quantity, paymentMethodId: nil, instant: false)
    }

    func createCheckoutIntent(itemId: String, quantity: Int, paymentMethodId: String?) async throws -> CheckoutIntent {
        try await createCheckoutIntent(itemId: itemId, quantity: quantity, paymentMethodId: paymentMethodId, instant: false)
    }

    func postFreeNow(visibility: ForumPostVisibility) async throws -> MyFreePost {
        try await postFreeNow(NewFreePost(visibility: visibility))
    }

    func sendMessage(threadId: String, text: String) async throws -> Message {
        try await sendMessage(threadId: threadId, text: text, clientId: nil)
    }
}

enum APIError: LocalizedError, Equatable {
    case invalidCredentials
    case invalidCode
    case unauthorized
    case notFound
    case validation(String)
    case server(status: Int, message: String?)
    case network(String)
    case decoding(String)

    var errorDescription: String? {
        switch self {
        case .invalidCredentials: "That email and password don't match."
        case .invalidCode: "That code didn't work. Check the email or resend a new one."
        case .unauthorized: "Your session expired. Sign in again."
        case .notFound: "We couldn't find that."
        case .validation(let message): message
        case .server(_, let message): message ?? "Something went wrong on our end."
        case .network: "You're offline or the server is unreachable."
        case .decoding: "We got an unexpected response."
        }
    }
}
