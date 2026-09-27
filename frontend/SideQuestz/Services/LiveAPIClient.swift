import Foundation

/// REST + JSON client for the real backend (GUI_PLAN.md §9).
///
/// - Bearer token on everything except `/auth/*`. On a 401 the client refreshes once (concurrent
///   401s share that one refresh) and retries; if that fails the session is over (`onUnauthorized`).
/// - snake_case JSON, ISO 8601 dates, money in integer cents, `X-Time-Zone: <IANA id>` on every call.
/// - List endpoints may return a bare array or `{ "items": [...], "next_cursor": "…" }`; the client
///   follows `next_cursor` (sent back as `?cursor=`) until it's null.
final class LiveAPIClient: APIClient {
    let baseURL: URL
    let auth: AuthStore
    let clock: AppClock
    /// Called when the session can't be refreshed any more (tokens are already cleared).
    var onUnauthorized: (() -> Void)?
    private let session: URLSession
    private let encoder = APICoding.encoder()
    private let decoder: JSONDecoder
    /// The refresh in flight, shared by every request that got a 401 meanwhile.
    private var refreshTask: Task<AuthTokens, any Error>?

    /// Safety stop for cursor pagination.
    private static let maxPages = 50

    init(baseURL: URL, auth: AuthStore, clock: AppClock, session: URLSession = .shared) {
        self.baseURL = baseURL
        self.auth = auth
        self.clock = clock
        self.session = session
        self.decoder = APICoding.decoder(timeZone: clock.calendar.timeZone)
    }

    // MARK: - Transport

    private struct Empty: Codable {}
    private struct Page<T: Decodable>: Decodable {
        var items: [T]
        var nextCursor: String?
    }
    private struct URLBody: Decodable { var url: URL }

    private func makeRequest(_ method: String, _ path: String, query: [URLQueryItem], body: (any Encodable)?, authorized: Bool) throws -> URLRequest {
        var components = URLComponents(url: baseURL.appendingPathComponent(path), resolvingAgainstBaseURL: false)!
        if !query.isEmpty { components.queryItems = query }
        var request = URLRequest(url: components.url!)
        request.httpMethod = method
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        // Day-based data (calendar days, "today", plan dates) is computed in the user's time zone.
        request.setValue(clock.calendar.timeZone.identifier, forHTTPHeaderField: "X-Time-Zone")
        if let body {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = try encoder.encode(body)
        }
        if authorized, let token = auth.tokens?.accessToken {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        return request
    }

    private func perform(_ request: URLRequest, authorized: Bool, retry: Bool) async throws -> Data {
        let data: Data, response: URLResponse
        do {
            (data, response) = try await session.data(for: request)
        } catch {
            throw APIError.network(error.localizedDescription)
        }
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        if status == 401, authorized {
            guard retry, let tokens = try? await refreshedTokens() else {
                expireSession()
                throw APIError.unauthorized
            }
            var again = request
            again.setValue("Bearer \(tokens.accessToken)", forHTTPHeaderField: "Authorization")
            return try await perform(again, authorized: authorized, retry: false)
        }
        guard (200..<300).contains(status) else {
            let message = (try? JSONSerialization.jsonObject(with: data) as? [String: Any]).flatMap { $0["message"] as? String ?? $0["detail"] as? String }
            switch status {
            case 401: throw APIError.unauthorized
            case 404: throw APIError.notFound
            case 400, 422: throw APIError.validation(message ?? "Check the details and try again.")
            default: throw APIError.server(status: status, message: message)
            }
        }
        return data
    }

    private func call<T: Decodable>(_ method: String, _ path: String, query: [URLQueryItem] = [], body: (any Encodable)? = nil, authorized: Bool = true) async throws -> T {
        let request = try makeRequest(method, path, query: query, body: body, authorized: authorized)
        let data = try await perform(request, authorized: authorized, retry: true)
        if T.self == Empty.self, data.isEmpty { return Empty() as! T }
        do {
            return try decoder.decode(T.self, from: data)
        } catch {
            throw APIError.decoding(String(describing: error))
        }
    }

    /// One refresh at a time: a 401 that arrives while a refresh is running waits for it.
    private func refreshedTokens() async throws -> AuthTokens {
        if let refreshTask { return try await refreshTask.value }
        guard let refreshToken = auth.tokens?.refreshToken else { throw APIError.unauthorized }
        let task = Task { try await self.refresh(refreshToken: refreshToken) }
        refreshTask = task
        defer { refreshTask = nil }
        let tokens = try await task.value
        auth.save(tokens)
        return tokens
    }

    private func expireSession() {
        guard auth.isSignedIn else { return }
        auth.clear()
        onUnauthorized?()
    }

    /// Every page of a list endpoint, following `next_cursor`.
    private func list<T: Decodable>(_ path: String, query: [URLQueryItem] = []) async throws -> [T] {
        var items: [T] = []
        var cursor: String?
        for _ in 0..<Self.maxPages {
            let pageQuery = query + (cursor.map { [URLQueryItem(name: "cursor", value: $0)] } ?? [])
            let request = try makeRequest("GET", path, query: pageQuery, body: nil, authorized: true)
            let data = try await perform(request, authorized: true, retry: true)
            if let array = try? decoder.decode([T].self, from: data) { return items + array }
            do {
                let page = try decoder.decode(Page<T>.self, from: data)
                items += page.items
                guard let next = page.nextCursor, !next.isEmpty else { return items }
                cursor = next
            } catch {
                throw APIError.decoding(String(describing: error))
            }
        }
        return items
    }

    /// Like `call`, but an empty body (204) or JSON `null` comes back as nil.
    private func callIfPresent<T: Decodable>(_ method: String, _ path: String, query: [URLQueryItem] = [], body: (any Encodable)? = nil) async throws -> T? {
        let request = try makeRequest(method, path, query: query, body: body, authorized: true)
        let data = try await perform(request, authorized: true, retry: true)
        if data.isEmpty { return nil }
        do {
            return try decoder.decode(T?.self, from: data)
        } catch {
            throw APIError.decoding(String(describing: error))
        }
    }

    private func send(_ method: String, _ path: String, query: [URLQueryItem] = [], body: (any Encodable)? = nil, authorized: Bool = true) async throws {
        let _: Empty = try await callAllowingEmpty(method, path, query: query, body: body, authorized: authorized)
    }

    private func callAllowingEmpty(_ method: String, _ path: String, query: [URLQueryItem], body: (any Encodable)?, authorized: Bool) async throws -> Empty {
        let request = try makeRequest(method, path, query: query, body: body, authorized: authorized)
        _ = try await perform(request, authorized: authorized, retry: true)
        return Empty()
    }

    private func upload<T: Decodable>(_ path: String, jpegData: Data, field: String = "photo") async throws -> T {
        let boundary = "sq-\(UUID().uuidString)"
        var request = try makeRequest("POST", path, query: [], body: nil, authorized: true)
        request.setValue("multipart/form-data; boundary=\(boundary)", forHTTPHeaderField: "Content-Type")
        var body = Data()
        body.append(Data("--\(boundary)\r\n".utf8))
        body.append(Data("Content-Disposition: form-data; name=\"\(field)\"; filename=\"photo.jpg\"\r\n".utf8))
        body.append(Data("Content-Type: image/jpeg\r\n\r\n".utf8))
        body.append(jpegData)
        body.append(Data("\r\n--\(boundary)--\r\n".utf8))
        request.httpBody = body
        let data = try await perform(request, authorized: true, retry: true)
        do { return try decoder.decode(T.self, from: data) } catch { throw APIError.decoding(String(describing: error)) }
    }

    private func dayString(_ date: Date) -> String { clock.dayKey(date) }

    // MARK: - Auth

    func signup(_ request: SignupRequest) async throws -> AuthResponse {
        try await call("POST", "auth/signup", body: request, authorized: false)
    }

    func login(email: String, password: String) async throws -> AuthResponse {
        struct Body: Encodable { var email: String; var password: String }
        do {
            return try await call("POST", "auth/login", body: Body(email: email, password: password), authorized: false)
        } catch APIError.unauthorized {
            throw APIError.invalidCredentials
        }
    }

    /// The server may rotate the refresh token or only return a new access token; keep the old
    /// refresh token when it doesn't send one.
    func refresh(refreshToken: String) async throws -> AuthTokens {
        struct Body: Encodable { var refreshToken: String }
        struct Response: Decodable { var accessToken: String; var refreshToken: String?; var expiresAt: Date? }
        let response: Response = try await call("POST", "auth/refresh", body: Body(refreshToken: refreshToken), authorized: false)
        return AuthTokens(accessToken: response.accessToken, refreshToken: response.refreshToken ?? refreshToken, expiresAt: response.expiresAt)
    }

    /// Sends the refresh token so the server can revoke this session.
    func logout() async throws {
        struct Body: Encodable { var refreshToken: String? }
        try await send("POST", "auth/logout", body: Body(refreshToken: auth.tokens?.refreshToken))
    }

    func forgotPassword(email: String) async throws {
        struct Body: Encodable { var email: String }
        try await send("POST", "auth/password/forgot", body: Body(email: email), authorized: false)
    }

    func verifyResetCode(email: String, code: String) async throws -> String {
        struct Body: Encodable { var email: String; var code: String }
        struct Response: Decodable { var resetToken: String }
        do {
            let response: Response = try await call("POST", "auth/password/verify", body: Body(email: email, code: code), authorized: false)
            return response.resetToken
        } catch APIError.validation, APIError.unauthorized {
            throw APIError.invalidCode
        }
    }

    func resetPassword(resetToken: String, newPassword: String) async throws {
        struct Body: Encodable { var resetToken: String; var newPassword: String }
        try await send("POST", "auth/password/reset", body: Body(resetToken: resetToken, newPassword: newPassword), authorized: false)
    }

    func resendResetCode(email: String) async throws {
        struct Body: Encodable { var email: String }
        try await send("POST", "auth/password/resend", body: Body(email: email), authorized: false)
    }

    // MARK: - Me

    func me() async throws -> User { try await call("GET", "me") }
    func updateMe(_ patch: UserPatch) async throws -> User { try await call("PATCH", "me", body: patch) }

    func uploadPhoto(_ jpegData: Data) async throws -> URL {
        let response: URLBody = try await upload("me/photo", jpegData: jpegData)
        return response.url
    }

    func deletePhoto() async throws { try await send("DELETE", "me/photo") }

    func setAvatarColor(_ color: AvatarColor) async throws {
        struct Body: Encodable { var color: AvatarColor }
        try await send("PATCH", "me/avatar", body: Body(color: color))
    }

    func preferences() async throws -> Preferences { try await call("GET", "me/preferences") }
    func savePreferences(_ preferences: Preferences) async throws { try await send("PUT", "me/preferences", body: preferences) }
    func tasteProfile() async throws -> TasteProfile { try await call("GET", "me/taste-profile") }

    func registerDevice(pushToken: String) async throws {
        struct Body: Encodable { var pushToken: String; var platform = "ios" }
        try await send("POST", "me/devices", body: Body(pushToken: pushToken))
    }

    func unregisterDevice(pushToken: String) async throws { try await send("DELETE", "me/devices/\(pushToken)") }

    // MARK: - Integrations + payments

    func integrations() async throws -> [Integration] { try await list("integrations") }

    func connectIntegration(_ provider: CalendarProvider) async throws -> URL {
        let response: URLBody = try await call("POST", "integrations/\(provider.rawValue)/connect")
        return response.url
    }

    /// The backend finishes OAuth in its callback; nothing else to send.
    func completeIntegration(_ provider: CalendarProvider) async throws {}

    func disconnectIntegration(_ provider: CalendarProvider) async throws {
        try await send("DELETE", "integrations/\(provider.rawValue)")
    }

    func paymentMethods() async throws -> [PaymentMethod] { try await list("me/payment-methods") }

    func paymentSetupURL() async throws -> URL {
        let response: URLBody = try await call("POST", "me/payment-methods/setup")
        return response.url
    }

    func addPaymentMethod(token: String) async throws -> PaymentMethod {
        struct Body: Encodable { var token: String }
        return try await call("POST", "me/payment-methods", body: Body(token: token))
    }

    func deletePaymentMethod(id: String) async throws { try await send("DELETE", "me/payment-methods/\(id)") }

    // MARK: - Facebook (Graph API)

    func facebookConnection() async throws -> FacebookConnection { try await call("GET", "integrations/facebook") }

    func facebookConnectURL(rerequest: Bool) async throws -> URL {
        struct Body: Encodable { var rerequest: Bool }
        let response: URLBody = try await call("POST", "integrations/facebook/connect", body: Body(rerequest: rerequest))
        return response.url
    }

    func importFacebook() async throws -> FacebookImport { try await call("POST", "integrations/facebook/import") }

    func disconnectFacebook() async throws { try await send("DELETE", "integrations/facebook") }

    // MARK: - Calendar / places / events

    func calendarDays(from: Date, to: Date) async throws -> [CalendarDay] {
        try await list("calendar/days", query: [URLQueryItem(name: "from", value: dayString(from)), URLQueryItem(name: "to", value: dayString(to))])
    }

    func searchPlaces(query: String, near: Coordinate?) async throws -> [Place] {
        var items = [URLQueryItem(name: "q", value: query)]
        if let near { items.append(URLQueryItem(name: "near", value: "\(near.lat),\(near.lng)")) }
        return try await list("places/search", query: items)
    }

    func reverseGeocode(_ coordinate: Coordinate) async throws -> Place {
        try await call("GET", "places/reverse", query: [URLQueryItem(name: "lat", value: String(coordinate.lat)),
                                                        URLQueryItem(name: "lng", value: String(coordinate.lng))])
    }

    func eventDetail(id: String) async throws -> ItineraryItem { try await call("GET", "events/\(id)") }

    func search(query: String) async throws -> SearchResults {
        try await call("GET", "search", query: [URLQueryItem(name: "q", value: query)])
    }

    // MARK: - Planning

    func searchActivities(q: String, near: Coordinate?, date: Date?, limit: Int) async throws -> [ActivityHit] {
        var items = [URLQueryItem(name: "q", value: q)]
        if let near { items.append(URLQueryItem(name: "near", value: "\(near.lat),\(near.lng)")) }
        if let date { items.append(URLQueryItem(name: "date", value: dayString(date))) }
        items.append(URLQueryItem(name: "limit", value: String(limit)))
        return try await list("activities/search", query: items)
    }

    func generatePlans(_ request: PlanRequest) async throws -> PlanBatch { try await call("POST", "plans/generate", body: request) }

    func moreOptions(cursor: String) async throws -> PlanBatch {
        struct Body: Encodable { var cursor: String }
        return try await call("POST", "plans/generate/more", body: Body(cursor: cursor))
    }

    func route(_ request: RouteRequest) async throws -> RouteResult { try await call("POST", "plans/route", body: request) }

    func stopAlternatives(optionId: String, stopId: String, stopOrder: [String]) async throws -> [PlanAlternative] {
        struct Body: Encodable { var optionId: String; var stopId: String; var stopOrder: [String] }
        return try await call("POST", "plans/alternatives", body: Body(optionId: optionId, stopId: stopId, stopOrder: stopOrder))
    }

    func activity(id: String, date: Date?) async throws -> ActivityDetail {
        try await call("GET", "activities/\(id)", query: date.map { [URLQueryItem(name: "date", value: dayString($0))] } ?? [])
    }

    func createItinerary(_ request: CreateItineraryRequest) async throws -> Itinerary { try await call("POST", "itineraries", body: request) }

    // MARK: - Itineraries

    func activeItineraries() async throws -> [Itinerary] { try await list("itineraries", query: [URLQueryItem(name: "status", value: "active")]) }
    func itinerary(id: String) async throws -> Itinerary { try await call("GET", "itineraries/\(id)") }
    func updateItinerary(id: String, _ update: ItineraryUpdate) async throws -> Itinerary {
        try await call("PATCH", "itineraries/\(id)", body: update)
    }
    func deleteItinerary(id: String) async throws { try await send("DELETE", "itineraries/\(id)") }
    func leaveItinerary(id: String) async throws { try await send("POST", "itineraries/\(id)/leave") }

    func updateItemNotes(itineraryId: String?, itemId: String, notes: String, scope: NotesScope?) async throws {
        struct Body: Encodable { var notes: String; var notesScope: NotesScope? }
        let path = itineraryId.map { "itineraries/\($0)/items/\(itemId)" } ?? "events/\(itemId)"
        try await send("PATCH", path, body: Body(notes: notes, notesScope: scope))
    }

    func transitOptions(itineraryId: String?, itemId: String) async throws -> [TransitOption] {
        let path = itineraryId.map { "itineraries/\($0)/items/\(itemId)/transit" } ?? "events/\(itemId)/transit"
        return try await list(path)
    }

    func selectTransit(itineraryId: String?, itemId: String, mode: TravelMode) async throws {
        struct Body: Encodable { var mode: TravelMode }
        let path = itineraryId.map { "itineraries/\($0)/items/\(itemId)/transit" } ?? "events/\(itemId)/transit"
        try await send("PUT", path, body: Body(mode: mode))
    }

    // MARK: - Past + ratings

    func pastEvents(unratedOnly: Bool) async throws -> [PastEvent] {
        try await list("me/past-events", query: [URLQueryItem(name: "unrated", value: unratedOnly ? "true" : "false")])
    }

    func rate(itemId: String, rating: Rating) async throws { try await send("PUT", "ratings/\(itemId)", body: rating) }
    func pastInsights() async throws -> PastInsights { try await call("GET", "me/insights") }

    // MARK: - Checkout

    func createCheckoutIntent(itemId: String, quantity: Int, paymentMethodId: String?, instant: Bool) async throws -> CheckoutIntent {
        struct Body: Encodable { var itemId: String; var quantity: Int; var paymentMethodId: String?; var instant: Bool }
        return try await call("POST", "checkout/intents", body: Body(itemId: itemId, quantity: quantity, paymentMethodId: paymentMethodId, instant: instant))
    }

    func checkoutIntent(id: String) async throws -> CheckoutIntent { try await call("GET", "checkout/intents/\(id)") }

    func updateCheckoutIntent(id: String, paymentMethodId: String) async throws -> CheckoutIntent {
        struct Body: Encodable { var paymentMethodId: String }
        return try await call("PATCH", "checkout/intents/\(id)", body: Body(paymentMethodId: paymentMethodId))
    }
    func approveCheckout(id: String) async throws -> CheckoutIntent { try await call("POST", "checkout/intents/\(id)/approve") }
    func cancelCheckout(id: String) async throws { try await send("POST", "checkout/intents/\(id)/cancel") }

    func checkoutPlan(itineraryId: String) async throws -> CheckoutPlan { try await call("GET", "itineraries/\(itineraryId)/checkout") }
    func startCheckoutRun(itineraryId: String, _ request: CreateCheckoutRun) async throws -> CheckoutRun {
        try await call("POST", "itineraries/\(itineraryId)/checkout-runs", body: request)
    }
    func checkoutRun(id: String) async throws -> CheckoutRun { try await call("GET", "checkout/runs/\(id)") }
    func cancelCheckoutRun(id: String) async throws -> CheckoutRun { try await call("POST", "checkout/runs/\(id)/cancel") }

    func myTickets() async throws -> [MyTicket] { try await list("me/tickets") }

    // MARK: - Forum

    func forumPosts(_ query: ForumQuery) async throws -> [ForumPost] {
        // "Current location" arrives here with the device's coordinate already filled in.
        let center = query.area.coordinate ?? ForumArea.midtown.coordinate ?? Coordinate(lat: 33.7838, lng: -84.3833)
        var items = [
            URLQueryItem(name: "lat", value: String(center.lat)), URLQueryItem(name: "lng", value: String(center.lng)),
            URLQueryItem(name: "area", value: query.area.name),
            URLQueryItem(name: "radius", value: String(query.radiusMi)), URLQueryItem(name: "scope", value: query.scope.rawValue),
            URLQueryItem(name: "type", value: query.type.rawValue), URLQueryItem(name: "when", value: query.when.rawValue),
            URLQueryItem(name: "open_only", value: query.openOnly ? "true" : "false"), URLQueryItem(name: "sort", value: query.sort.rawValue),
        ]
        if let max = query.maxDistanceMi { items.append(URLQueryItem(name: "max_dist", value: String(max))) }
        if !query.cost.isEmpty { items.append(URLQueryItem(name: "cost", value: query.cost.sorted().map(String.init).joined(separator: ","))) }
        if !query.tags.isEmpty { items.append(URLQueryItem(name: "tags", value: query.tags.sorted().joined(separator: ","))) }
        return try await list("forum/posts", query: items)
    }

    func myFreePost() async throws -> MyFreePost? { try await callIfPresent("GET", "forum/posts/mine") }

    func postFreeNow(_ post: NewFreePost) async throws -> MyFreePost {
        struct Body: Encodable {
            var type = "free_now"
            var visibility: ForumPostVisibility
            var until: Date?
            var lat: Double?
            var lng: Double?
            var areaLabel: String?
            var radiusMi: Int?
        }
        return try await call("POST", "forum/posts", body: Body(visibility: post.visibility, until: post.until,
                                                                  lat: post.area?.coordinate?.lat, lng: post.area?.coordinate?.lng,
                                                                  areaLabel: post.area?.name, radiusMi: post.radiusMi))
    }

    func deleteForumPost(id: String) async throws { try await send("DELETE", "forum/posts/\(id)") }
    func requestToJoin(postId: String) async throws -> JoinResult {
        let result: JoinResult? = try await callIfPresent("POST", "forum/posts/\(postId)/join-requests")
        return result ?? JoinResult(status: .requested)
    }
    func cancelJoinRequest(postId: String) async throws { try await send("DELETE", "forum/posts/\(postId)/join-requests") }
    func planTogether(postId: String) async throws -> ChatThread { try await call("POST", "forum/posts/\(postId)/plan-together") }

    // MARK: - Threads

    func threads() async throws -> [ChatThread] { try await list("threads") }
    func thread(id: String) async throws -> ChatThread { try await call("GET", "threads/\(id)") }

    func messages(threadId: String, before: String?) async throws -> [Message] {
        try await list("threads/\(threadId)/messages", query: before.map { [URLQueryItem(name: "before", value: $0)] } ?? [])
    }

    func sendMessage(threadId: String, text: String, clientId: String?) async throws -> Message {
        struct Body: Encodable { var text: String; var clientId: String? }
        return try await call("POST", "threads/\(threadId)/messages", body: Body(text: text, clientId: clientId))
    }

    func markThreadRead(id: String) async throws { try await send("POST", "threads/\(id)/read") }

    func startDM(userId: String) async throws -> ChatThread {
        struct Body: Encodable { var userId: String }
        return try await call("POST", "threads/dm", body: Body(userId: userId))
    }

    // MARK: - Album

    func groupPhotos(groupId: String) async throws -> [GroupPhoto] { try await list("groups/\(groupId)/photos") }
    func uploadGroupPhoto(groupId: String, jpegData: Data) async throws -> GroupPhoto { try await upload("groups/\(groupId)/photos", jpegData: jpegData) }
    func deleteGroupPhoto(groupId: String, photoId: String) async throws { try await send("DELETE", "groups/\(groupId)/photos/\(photoId)") }

    // MARK: - Splits

    func ledger(groupId: String) async throws -> GroupLedger {
        let thread: ChatThread = try await call("GET", "threads/\(groupId)")
        let expenses: [Expense] = try await list("groups/\(groupId)/expenses")
        let balances: [Balance] = try await list("groups/\(groupId)/balances")
        return GroupLedger(members: thread.members, expenses: expenses, balances: balances)
    }

    func addExpense(groupId: String, _ expense: NewExpense) async throws -> Expense { try await call("POST", "groups/\(groupId)/expenses", body: expense) }
    func deleteExpense(groupId: String, expenseId: String) async throws { try await send("DELETE", "groups/\(groupId)/expenses/\(expenseId)") }
    func settleUp(groupId: String, amountCents: Int, paymentMethodId: String?) async throws {
        struct Body: Encodable { var amountCents: Int; var paymentMethodId: String? }
        try await send("POST", "groups/\(groupId)/settle", body: Body(amountCents: amountCents, paymentMethodId: paymentMethodId))
    }

    // MARK: - Friends

    func friends() async throws -> [Friend] { try await list("friends") }
    func searchUsers(query: String) async throws -> [UserSearchResult] {
        try await list("users/search", query: [URLQueryItem(name: "q", value: query)])
    }
    func suggestedPeople() async throws -> [PersonSuggestion] { try await list("people/suggested") }
    func profile(userId: String) async throws -> PublicProfile { try await call("GET", "users/\(userId)/profile") }
    func friendRequests() async throws -> [FriendRequest] { try await list("friends/requests") }

    func sendFriendRequest(userId: String) async throws -> FriendRequest {
        struct Body: Encodable { var userId: String }
        return try await call("POST", "friends/requests", body: Body(userId: userId))
    }

    func cancelFriendRequest(id: String) async throws { try await send("DELETE", "friends/requests/\(id)") }

    func acceptFriendRequest(id: String) async throws { try await send("POST", "friends/requests/\(id)/accept") }
    func declineFriendRequest(id: String) async throws { try await send("POST", "friends/requests/\(id)/decline") }
    func removeFriend(id: String) async throws { try await send("DELETE", "friends/\(id)") }

    func createInvite() async throws -> URL {
        let response: URLBody = try await call("POST", "invites")
        return response.url
    }

    func acceptInvite(code: String) async throws -> Friend { try await call("POST", "invites/\(code)/accept") }
}
