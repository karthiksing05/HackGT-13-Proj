import Combine
import SwiftUI
import UIKit

/// Thread › Chat (GUI_PLAN.md §7.9): day divider, bubbles (mine sage on the right, others white on
/// the left with the sender's name in groups) and the composer. New messages arrive over the socket.
///
/// Sending is optimistic: your bubble appears right away, a little lighter, with "Sending" and
/// loading dots under it, and settles in place when the server confirms it. A failed send reads
/// "Not sent · Tap to retry" and keeps its place in the conversation (newer messages arrive below
/// it). Sends go out one at a time so they arrive in the order you wrote them. Pending messages
/// live only in this view (the API contract is unchanged).
struct ThreadChatView: View {
    let threadId: String
    /// Group chats show the sender's name above other people's bubbles.
    let isGroup: Bool
    @Binding var draft: String
    /// False while another tab is showing (the view stays alive to keep its state).
    var isActive: Bool

    @Environment(AppEnvironment.self) private var env
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    @State private var messages: Loadable<[Message]> = .loading
    /// Messages sent from here that the server hasn't confirmed yet, or that failed.
    @State private var outbox: [ChatOutgoing] = []
    /// Server id → local id of the messages sent from here, so a confirmed bubble stays the same
    /// view (it settles in place instead of being replaced).
    @State private var localIds: [String: String] = [:]
    /// Messages from the load that replaced the skeleton: only these arrive one after another.
    @State private var arrivingIds: Set<String> = []
    /// The send in flight; the next one waits for it.
    @State private var lastSend: Task<Void, Never>?
    @State private var keyboardShown = false
    @FocusState private var composerFocused: Bool

    private var hasDraft: Bool { !draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                messageList
                    .padding(.horizontal, 14)
                    // Prototype: groups' messages start 15pt under the header, DMs' 21pt.
                    .padding(.top, isGroup ? 15 : 21)
                    .padding(.bottom, 16)
            }
            .scrollIndicators(.hidden)
            .scrollDismissesKeyboard(.interactively)
            .modifier(ChatFollowsNewest())
            .sqPullToRefresh()
            .onChange(of: rows.last?.id) { old, _ in
                scrollToNewest(proxy, animated: old != nil)
            }
            .onChange(of: keyboardShown) { _, shown in
                if shown { scrollToNewest(proxy, animated: true) }
            }
        }
        .safeAreaInset(edge: .bottom, spacing: 0) { composer }
        .task { await load() }
        .task { await listenForMessages() }
        .sqReloadable("thread.\(threadId).chat") { await reload() }
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillShowNotification)) { _ in
            keyboardShown = true
        }
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillHideNotification)) { _ in
            keyboardShown = false
        }
        .onChange(of: isActive) { _, active in
            if !active { composerFocused = false }
        }
    }

    // MARK: Messages

    /// The server's messages (one sent from here keeps the local id it was sent with), with each
    /// message still sending or failed right after the one it was written after.
    private var rows: [ChatRow] {
        guard let list = messages.value else { return [] }
        let myId = env.user?.id
        var result = list.map { message in
            ChatRow(id: localIds[message.id] ?? message.id, senderName: message.senderName, text: message.text,
                    sentAt: message.sentAt, mine: message.senderId == myId, delivery: .sent)
        }
        for item in outbox {
            let row = ChatRow(id: item.id, senderName: "You", text: item.text, sentAt: item.sentAt, mine: true,
                              delivery: item.failed ? .failed : .sending)
            if let anchor = item.afterId, let index = result.firstIndex(where: { $0.id == anchor }) {
                result.insert(row, at: index + 1)
            } else {
                result.append(row)
            }
        }
        return result
    }

    private var messageList: some View {
        ZStack(alignment: .top) {
            switch messages {
            case .loading:
                SkeletonView(layout: .bubbles(count: 5))
                    .transition(.opacity)
            case .failed(let message):
                ErrorStateView(message: message) { Task { await retryLoad() } }
                    .transition(.opacity)
            case .loaded:
                let rows = rows
                if rows.isEmpty {
                    EmptyStateView(message: "No messages yet.")
                        .transition(.opacity)
                } else {
                    VStack(spacing: 8) {
                        ForEach(Array(rows.enumerated()), id: \.element.id) { index, row in
                            if index == 0 || !env.clock.calendar.isDate(rows[index - 1].sentAt, inSameDayAs: row.sentAt) {
                                Text(dayLabel(row.sentAt))
                                    .socialText(12)
                                    .foregroundStyle(Theme.text3)
                                    .frame(maxWidth: .infinity)
                                    .accessibilityAddTraits(.isHeader)
                                    .transition(.opacity)
                            }
                            let firstLoad = arrivingIds.contains(row.id)
                            bubble(row)
                                .id(row.id)
                                .modifier(SocialFirstArrival(index: index, staggered: firstLoad))
                                // New bubbles grow out of their tail: yours as you send, others' as they arrive.
                                .transition(firstLoad ? SocialMotion.settled
                                                      : SocialMotion.transition(.bubble(mine: row.mine), reduceMotion: reduceMotion))
                        }
                    }
                    .transition(.opacity)
                }
            }
        }
        .animation(Motion.standard, value: messages.phase)
    }

    private func bubble(_ row: ChatRow) -> some View {
        VStack(alignment: row.mine ? .trailing : .leading, spacing: 2) {
            if !row.mine && isGroup {
                Text(row.senderName)
                    .socialText(11)
                    .foregroundStyle(Theme.text3)
                    .padding(.horizontal, 10)
            }
            BubbleWidth(maxWidth: 260) {
                Text(row.text)
                    .socialText(15)
                    .foregroundStyle(Theme.ink)
            }
            .padding(.horizontal, 13)
            .padding(.vertical, 9)
            .background(row.mine ? Theme.sage : .white, in: RoundedRectangle(cornerRadius: 18, style: .continuous))
            // Not on the server yet: a little lighter until it settles.
            .opacity(row.delivery == .sent ? 1 : 0.7)
            if row.delivery != .sent {
                deliveryStatus(row.delivery)
                    .padding(.horizontal, 6)
                    .transition(.opacity)
            }
        }
        .contentShape(Rectangle())
        .onTapGesture {
            if row.delivery == .failed { retry(row.id) }
        }
        .frame(maxWidth: .infinity, alignment: row.mine ? .trailing : .leading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(row.mine ? "You" : row.senderName): \(row.text)")
        .accessibilityValue(row.delivery == .sending ? "Sending" : row.delivery == .failed ? "Not sent" : "")
        .accessibilityHint(row.delivery == .failed ? "Double-tap to send it again" : "")
        .accessibilityAddTraits(row.delivery == .failed ? .isButton : [])
        .accessibilityAction {
            if row.delivery == .failed { retry(row.id) }
        }
    }

    /// "Sending ···" while the server has it, "Not sent · Tap to retry" after a failure.
    private func deliveryStatus(_ delivery: ChatDelivery) -> some View {
        ZStack(alignment: .trailing) {
            if delivery == .failed {
                Text("Not sent · Tap to retry")
                    .socialText(11, .semibold)
                    .foregroundStyle(Theme.dangerText)
                    .transition(.opacity)
            } else {
                HStack(alignment: .lastTextBaseline, spacing: 4) {
                    Text("Sending")
                        .socialText(11)
                        .foregroundStyle(Theme.text3)
                    LoadingDots(color: Theme.text3, dotSize: 3.5)
                        .alignmentGuide(.lastTextBaseline) { $0[.bottom] - 1 }
                }
                .transition(.opacity)
            }
        }
        .animation(Motion.quick, value: delivery)
    }

    private func dayLabel(_ date: Date) -> String {
        let clock = env.clock
        if clock.isToday(date) { return "Today" }
        if let yesterday = clock.calendar.date(byAdding: .day, value: -1, to: clock.now),
           clock.calendar.isDate(date, inSameDayAs: yesterday) {
            return "Yesterday"
        }
        return env.format.shortDate(date)
    }

    private func scrollToNewest(_ proxy: ScrollViewProxy, animated: Bool) {
        guard let last = rows.last?.id else { return }
        DispatchQueue.main.async {
            if animated {
                withMotion(Motion.standard) { proxy.scrollTo(last, anchor: .bottom) }
            } else {
                proxy.scrollTo(last, anchor: .bottom)
            }
        }
    }

    // MARK: Composer

    private var composer: some View {
        HStack(spacing: 8) {
            TextField("", text: $draft, prompt: Text("Message").foregroundStyle(Theme.text3))
                .sqFont(15)
                .foregroundStyle(Theme.ink)
                .focused($composerFocused)
                .submitLabel(.send)
                .onSubmit {
                    send()
                    composerFocused = true  // keep the keyboard up after Return
                }
                .padding(.horizontal, 14)
                .frame(height: 38)
                .background(Theme.field, in: Capsule())
                .overlay(Capsule().strokeBorder(Theme.lineStrong, lineWidth: 1))
                .accessibilityLabel("Message")
            Button(action: send) {
                SocialGlyph(kind: .arrowUp, size: 18, lineWidth: 2.4)
                    .foregroundStyle(Theme.ink)
                    .frame(width: 38, height: 38)
                    .background(Theme.sage, in: Circle())
                    .contentShape(Circle().inset(by: -3))
            }
            .buttonStyle(.plain)
            // Springs when there's something to send.
            .sqBounce(when: hasDraft)
            .accessibilityLabel("Send message")
        }
        .frame(height: 44)
        .padding(.horizontal, 12)
        .padding(.top, 10)
        // Prototype: the 84pt bar's field sits 4pt into the home-indicator area.
        .padding(.bottom, keyboardShown ? 8 : -4)
        .frame(maxWidth: .infinity)
        .background(Color.white.ignoresSafeArea(edges: .bottom))
        .overlay(alignment: .top) { Rectangle().fill(Theme.line).frame(height: 1) }
    }

    /// Shows the message right away and queues it for the server.
    private func send() {
        let text = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, messages.value != nil else { return }
        draft = ""
        let item = ChatOutgoing(id: "local-\(UUID().uuidString)", text: text, sentAt: env.clock.now,
                                afterId: rows.last?.id)
        withMotion(Motion.arrive) { outbox.append(item) }
        deliver(item.id)
    }

    /// Sends after the message before it has gone, so the server gets them in order.
    private func deliver(_ id: String) {
        let previous = lastSend
        lastSend = Task {
            await previous?.value
            await transmit(id)
        }
    }

    private func transmit(_ id: String) async {
        // Already confirmed (its echo came over the socket) or no longer waiting.
        guard let item = outbox.first(where: { $0.id == id }), !item.failed else { return }
        do {
            let message = try await env.api.sendMessage(threadId: threadId, text: item.text)
            confirm(id, with: message)
        } catch {
            guard let index = outbox.firstIndex(where: { $0.id == id }) else { return }
            withMotion { outbox[index].failed = true }
        }
    }

    /// The server has it: the bubble settles where it is (same view, the pending line fades out).
    private func confirm(_ localId: String, with message: Message) {
        localIds[message.id] = localId
        let shown = rows
        withMotion {
            if var list = messages.value, !list.contains(where: { $0.id == message.id }) {
                // Into the server list right after the server message shown above the bubble.
                var index = list.count
                if let position = shown.firstIndex(where: { $0.id == localId }) {
                    if let above = shown[..<position].last(where: { $0.delivery == .sent }) {
                        if let found = list.firstIndex(where: { (localIds[$0.id] ?? $0.id) == above.id }) { index = found + 1 }
                    } else {
                        index = 0
                    }
                }
                list.insert(message, at: index)
                messages = .loaded(list)
            }
            outbox.removeAll { $0.id == localId }
        }
    }

    private func retry(_ id: String) {
        guard let index = outbox.firstIndex(where: { $0.id == id }), outbox[index].failed else { return }
        withMotion(Motion.quick) { outbox[index].failed = false }
        deliver(id)
    }

    // MARK: Loading

    private func load() async {
        let result = await Loadable.run { try await env.api.messages(threadId: threadId, before: nil) }
        show(result)
    }

    private func retryLoad() async {
        withMotion { messages = .loading }
        await load()
    }

    /// Pull to refresh: swaps in the latest messages and keeps the conversation if the call fails.
    private func reload() async {
        guard let fresh = try? await env.api.messages(threadId: threadId, before: nil) else { return }
        show(.loaded(fresh))
    }

    private func show(_ result: Loadable<[Message]>) {
        let fromSkeleton = messages.value == nil
        if fromSkeleton, let list = result.value { arrivingIds = Set(list.map(\.id)) }
        withMotion(fromSkeleton ? Motion.standard : Motion.arrive) { messages = result }
    }

    private func listenForMessages() async {
        for await event in env.realtime.subscribe() {
            if case .messageNew(let id, let message) = event, id == threadId {
                receive(message)
            }
        }
    }

    /// A message from the socket slides in. Your own message coming back before the send call
    /// returns confirms its pending bubble instead of adding a second one.
    private func receive(_ message: Message) {
        guard var list = messages.value, !list.contains(where: { $0.id == message.id }) else { return }
        if message.senderId == env.user?.id,
           let pending = outbox.first(where: { !$0.failed && $0.text == message.text }) {
            confirm(pending.id, with: message)
            return
        }
        list.append(message)
        withMotion(Motion.arrive) { messages = .loaded(list) }
    }
}

/// Keeps the newest message in view when the conversation grows (a message sent or received as
/// the list starts to overflow, where `scrollTo` alone can't reach it yet). iOS 18+; the
/// `scrollTo` on each new message covers the rest.
private struct ChatFollowsNewest: ViewModifier {
    func body(content: Content) -> some View {
        if #available(iOS 18.0, *) {
            content.defaultScrollAnchor(.bottom, for: .sizeChanges)
        } else {
            content
        }
    }
}

private enum ChatDelivery {
    case sent, sending, failed
}

/// A message typed here that isn't on the server yet.
private struct ChatOutgoing: Identifiable, Equatable {
    let id: String
    let text: String
    let sentAt: Date
    /// The row it was written after (it stays there; newer messages arrive below it).
    let afterId: String?
    var failed = false
}

/// One bubble in the list: a server message or one still going out.
private struct ChatRow: Identifiable {
    let id: String
    let senderName: String
    let text: String
    let sentAt: Date
    let mine: Bool
    let delivery: ChatDelivery
}

/// Proposes at most `maxWidth` and takes the child's own size, so short bubbles hug their text while
/// long ones wrap at 260pt (a finite `.frame(maxWidth:)` would always grow to 260).
private struct BubbleWidth: Layout {
    let maxWidth: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        guard let child = subviews.first else { return .zero }
        let width = min(proposal.width ?? maxWidth, maxWidth)
        return child.sizeThatFits(ProposedViewSize(width: width, height: nil))
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        subviews.first?.place(at: bounds.origin, proposal: ProposedViewSize(width: bounds.width, height: bounds.height))
    }
}
