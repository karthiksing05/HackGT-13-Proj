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
/// Reconnects with backoff while connected; publishes into the `RealtimeHub`.
final class WebSocketService {
    private let url: URL
    private let hub: RealtimeHub
    private let session: URLSession
    private var task: URLSessionWebSocketTask?
    private var token: String?
    private var shouldRun = false
    private var retryDelay: UInt64 = 1

    private let decoder: JSONDecoder = {
        let d = JSONDecoder()
        d.keyDecodingStrategy = .convertFromSnakeCase
        d.dateDecodingStrategy = .iso8601
        return d
    }()

    init(url: URL, hub: RealtimeHub, session: URLSession = .shared) {
        self.url = url
        self.hub = hub
        self.session = session
    }

    func connect(token: String?) {
        self.token = token
        shouldRun = true
        open()
    }

    func disconnect() {
        shouldRun = false
        task?.cancel(with: .goingAway, reason: nil)
        task = nil
    }

    private func open() {
        var request = URLRequest(url: url)
        if let token { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
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
                guard shouldRun else { return }
                try? await Task.sleep(nanoseconds: retryDelay * 1_000_000_000)
                retryDelay = min(retryDelay * 2, 30)
                if shouldRun, task === self.task { open() }
                return
            }
        }
    }

    private struct Envelope: Decodable { var type: String }
    private struct MessageNew: Decodable { var data: Payload; struct Payload: Decodable { var threadId: String; var message: Message } }
    private struct JoinRequest: Decodable { var data: Payload; struct Payload: Decodable { var postId: String; var from: PersonRef } }
    private struct FriendStatus: Decodable { var data: Payload; struct Payload: Decodable { var userId: String; var statusLine: String } }
    private struct CheckoutStatus: Decodable { var data: Payload; struct Payload: Decodable { var intentId: String; var state: CheckoutState } }
    private struct TransitDelay: Decodable { var data: Payload; struct Payload: Decodable { var itineraryId: String; var itemId: String; var minutes: Int } }

    private func handle(_ data: Data) {
        guard let envelope = try? decoder.decode(Envelope.self, from: data) else { return }
        switch envelope.type {
        case "message.new":
            if let e = try? decoder.decode(MessageNew.self, from: data) { hub.publish(.messageNew(threadId: e.data.threadId, message: e.data.message)) }
        case "join.request":
            if let e = try? decoder.decode(JoinRequest.self, from: data) { hub.publish(.joinRequest(postId: e.data.postId, from: e.data.from)) }
        case "friend.status":
            if let e = try? decoder.decode(FriendStatus.self, from: data) { hub.publish(.friendStatus(userId: e.data.userId, statusLine: e.data.statusLine)) }
        case "forum.update":
            hub.publish(.forumUpdate)
        case "checkout.status":
            if let e = try? decoder.decode(CheckoutStatus.self, from: data) { hub.publish(.checkoutStatus(intentId: e.data.intentId, state: e.data.state)) }
        case "transit.delay":
            if let e = try? decoder.decode(TransitDelay.self, from: data) {
                hub.publish(.transitDelay(itineraryId: e.data.itineraryId, itemId: e.data.itemId, minutes: e.data.minutes))
            }
        default:
            break
        }
    }
}
