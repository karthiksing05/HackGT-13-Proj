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

    // MARK: Integrations + payments
    func integrations() async throws -> [Integration]
    /// Returns the OAuth URL to open in ASWebAuthenticationSession.
    func connectIntegration(_ provider: CalendarProvider) async throws -> URL
    /// Mock only needs this to mark a provider connected after OAuth; live marks it server-side.
    func completeIntegration(_ provider: CalendarProvider) async throws
    func disconnectIntegration(_ provider: CalendarProvider) async throws
    func paymentMethods() async throws -> [PaymentMethod]
    func addPaymentMethod(token: String) async throws -> PaymentMethod
    func deletePaymentMethod(id: String) async throws

    // MARK: Calendar / places / events
    func calendarDays(from: Date, to: Date) async throws -> [CalendarDay]
    func searchPlaces(query: String, near: Coordinate?) async throws -> [Place]
    func reverseGeocode(_ coordinate: Coordinate) async throws -> Place
    /// Full details for any block id (itinerary items, calendar entries, catalog events).
    func eventDetail(id: String) async throws -> ItineraryItem

    // MARK: Planning
    /// First 3 options + cursor.
    func generatePlans(_ request: PlanRequest) async throws -> PlanBatch
    /// 2 more options; `done` when out.
    func moreOptions(cursor: String) async throws -> PlanBatch
    /// Recalculates legs, stop times, arrival and lateness after a reorder.
    func route(_ request: RouteRequest) async throws -> RouteResult
    func createItinerary(_ request: CreateItineraryRequest) async throws -> Itinerary

    // MARK: Itineraries
    func activeItineraries() async throws -> [Itinerary]
    func itinerary(id: String) async throws -> Itinerary
    func deleteItinerary(id: String) async throws
    func updateItemNotes(itineraryId: String?, itemId: String, notes: String) async throws
    func transitOptions(itineraryId: String?, itemId: String) async throws -> [TransitOption]

    // MARK: Past + ratings
    func pastEvents(unratedOnly: Bool) async throws -> [PastEvent]
    func rate(itemId: String, rating: Rating) async throws

    // MARK: Checkout (Visa agent)
    func createCheckoutIntent(itemId: String, quantity: Int) async throws -> CheckoutIntent
    func checkoutIntent(id: String) async throws -> CheckoutIntent
    /// The only step that spends money.
    func approveCheckout(id: String) async throws -> CheckoutIntent
    func cancelCheckout(id: String) async throws

    // MARK: Forum
    func forumPosts(_ query: ForumQuery) async throws -> [ForumPost]
    func myFreePost() async throws -> MyFreePost?
    func postFreeNow(visibility: ForumPostVisibility) async throws -> MyFreePost
    func deleteForumPost(id: String) async throws
    func requestToJoin(postId: String) async throws
    func cancelJoinRequest(postId: String) async throws
    /// Sends a "plan together" message and returns the DM thread.
    func planTogether(postId: String) async throws -> ChatThread

    // MARK: Threads
    func threads() async throws -> [ChatThread]
    func thread(id: String) async throws -> ChatThread
    func messages(threadId: String, before: String?) async throws -> [Message]
    func sendMessage(threadId: String, text: String) async throws -> Message
    func startDM(userId: String) async throws -> ChatThread

    // MARK: Group album
    func groupPhotos(groupId: String) async throws -> [GroupPhoto]
    func uploadGroupPhoto(groupId: String, jpegData: Data) async throws -> GroupPhoto
    func deleteGroupPhoto(groupId: String, photoId: String) async throws

    // MARK: Group splits (equal)
    func ledger(groupId: String) async throws -> GroupLedger
    func addExpense(groupId: String, _ expense: NewExpense) async throws -> Expense
    func deleteExpense(groupId: String, expenseId: String) async throws
    func settleUp(groupId: String) async throws

    // MARK: Friends
    func friends() async throws -> [Friend]
    func searchUsers(query: String) async throws -> [PersonRef]
    func friendRequests() async throws -> [FriendRequest]
    func sendFriendRequest(userId: String) async throws
    func acceptFriendRequest(id: String) async throws
    func declineFriendRequest(id: String) async throws
    func removeFriend(id: String) async throws
    func createInvite() async throws -> URL
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
