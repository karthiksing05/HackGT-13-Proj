import Foundation

/// Fan-out of realtime events to any number of listeners (chat, forum, checkout…).
///
/// Usage in a view: `.task { for await event in env.realtime.subscribe() { … } }`
final class RealtimeHub {
    private var continuations: [UUID: AsyncStream<RealtimeEvent>.Continuation] = [:]

    func subscribe() -> AsyncStream<RealtimeEvent> {
        let id = UUID()
        return AsyncStream { continuation in
            continuations[id] = continuation
            continuation.onTermination = { [weak self] _ in
                let hub = self
                Task { @MainActor in hub?.remove(id) }
            }
        }
    }

    func publish(_ event: RealtimeEvent) {
        for continuation in continuations.values { continuation.yield(event) }
    }

    private func remove(_ id: UUID) {
        continuations[id] = nil
    }
}

/// `WS /ws` over URLSessionWebSocketTask. Events arrive as JSON `{ "type": "message.new", "data": {…} }`.
/// Reconnects with backoff while connected (with whatever access token is current at that moment);
/// publishes into the `RealtimeHub`. `connect()` is idempotent, so calling it again (sign-in, pull to
/// refresh) never opens a second socket.
final class WebSocketService {
    private let url: URL
    private let hub: RealtimeHub
    private let session: URLSession
    private let token: () -> String?
    private let decoder: JSONDecoder
    private var task: URLSessionWebSocketTask?
    private var shouldRun = false
    private var retryDelay: UInt64 = 1

    init(url: URL, hub: RealtimeHub, timeZone: TimeZone, session: URLSession = .shared, token: @escaping () -> String?) {
        self.url = url
        self.hub = hub
        self.session = session
        self.token = token
        self.decoder = APICoding.decoder(timeZone: timeZone)
    }

    func connect() {
        guard !shouldRun || task == nil else { return }
        shouldRun = true
        open()
    }

    func disconnect() {
        shouldRun = false
        task?.cancel(with: .goingAway, reason: nil)
        task = nil
    }

    private func open() {
        task?.cancel(with: .goingAway, reason: nil)
        var request = URLRequest(url: url)
        if let token = token() { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        let task = session.webSocketTask(with: request)
        self.task = task
        task.resume()
        Task { await receiveLoop(task) }
    }

    private func receiveLoop(_ task: URLSessionWebSocketTask) async {
        while shouldRun, task === self.task {
            do {
                let message = try await task.receive()
                retryDelay = 1
                switch message {
                case .string(let text): handle(Data(text.utf8))
                case .data(let data): handle(data)
                @unknown default: break
                }
            } catch {
                guard shouldRun, task === self.task else { return }
                try? await Task.sleep(nanoseconds: retryDelay * 1_000_000_000)
                retryDelay = min(retryDelay * 2, 30)
                if shouldRun, task === self.task { open() }
                return
            }
        }
    }

    private struct Envelope: Decodable { var type: String }
    private struct Event<Payload: Decodable>: Decodable { var data: Payload }
    private struct MessageNew: Decodable { var threadId: String; var message: Message }
    private struct ThreadRead: Decodable { var threadId: String }
    private struct JoinRequest: Decodable { var postId: String; var from: PersonRef }
    private struct JoinUpdate: Decodable { var postId: String; var result: JoinResult }
    private struct FriendStatus: Decodable { var userId: String; var statusLine: String }
    private struct CheckoutStatus: Decodable { var intentId: String; var state: CheckoutState }
    private struct TransitDelay: Decodable { var itineraryId: String; var itemId: String; var minutes: Int }
    private struct ItineraryRemoved: Decodable { var itineraryId: String }
    private struct ExpenseAdded: Decodable { var groupId: String; var expense: Expense }
    private struct PhotoAdded: Decodable { var groupId: String; var photo: GroupPhoto }

    private func payload<T: Decodable>(_ type: T.Type, _ data: Data) -> T? {
        try? decoder.decode(Event<T>.self, from: data).data
    }

    private func handle(_ data: Data) {
        guard let envelope = try? decoder.decode(Envelope.self, from: data) else { return }
        let event: RealtimeEvent?
        switch envelope.type {
        case "message.new": event = payload(MessageNew.self, data).map { .messageNew(threadId: $0.threadId, message: $0.message) }
        case "thread.updated": event = payload(ChatThread.self, data).map { .threadUpdated($0) }
        case "thread.read": event = payload(ThreadRead.self, data).map { .threadRead(threadId: $0.threadId) }
        case "join.request": event = payload(JoinRequest.self, data).map { .joinRequest(postId: $0.postId, from: $0.from) }
        case "join.update": event = payload(JoinUpdate.self, data).map { .joinUpdate(postId: $0.postId, result: $0.result) }
        case "friend.status": event = payload(FriendStatus.self, data).map { .friendStatus(userId: $0.userId, statusLine: $0.statusLine) }
        case "friend.request": event = payload(FriendRequest.self, data).map { .friendRequest($0) }
        case "forum.update": event = .forumUpdate
        case "checkout.status": event = payload(CheckoutStatus.self, data).map { .checkoutStatus(intentId: $0.intentId, state: $0.state) }
        case "transit.delay": event = payload(TransitDelay.self, data).map { .transitDelay(itineraryId: $0.itineraryId, itemId: $0.itemId, minutes: $0.minutes) }
        case "itinerary.updated": event = payload(Itinerary.self, data).map { .itineraryUpdated($0) }
        case "itinerary.removed": event = payload(ItineraryRemoved.self, data).map { .itineraryRemoved(id: $0.itineraryId) }
        case "expense.added": event = payload(ExpenseAdded.self, data).map { .expenseAdded(groupId: $0.groupId, expense: $0.expense) }
        case "photo.added": event = payload(PhotoAdded.self, data).map { .photoAdded(groupId: $0.groupId, photo: $0.photo) }
        default: event = nil
        }
        if let event { hub.publish(event) }
    }
}
