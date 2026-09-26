import Combine
import SwiftUI
import UIKit

/// Thread › Chat (GUI_PLAN.md §7.9): day divider, bubbles (mine sage on the right, others white on
/// the left with the sender's name in groups) and the composer. New messages arrive over the socket.
struct ThreadChatView: View {
    let thread: ChatThread
    @Binding var draft: String
    /// False while another tab is showing (the view stays alive to keep its state).
    var isActive: Bool

    @Environment(AppEnvironment.self) private var env
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    @State private var messages: Loadable<[Message]> = .loading
    @State private var sending = false
    @State private var sendError: String?
    @State private var keyboardShown = false
    @FocusState private var composerFocused: Bool

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                messageList
                    .padding(.horizontal, 14)
                    // Prototype: groups' messages start 15pt under the header, DMs' 21pt.
                    .padding(.top, thread.isGroup ? 15 : 21)
                    .padding(.bottom, 16)
            }
            .scrollIndicators(.hidden)
            .scrollDismissesKeyboard(.interactively)
            .onChange(of: messages.value?.last?.id) { old, _ in
                scrollToNewest(proxy, animated: old != nil)
            }
            .onChange(of: keyboardShown) { _, shown in
                if shown { scrollToNewest(proxy, animated: true) }
            }
        }
        .safeAreaInset(edge: .bottom, spacing: 0) { composer }
        .task { await load() }
        .task { await listenForMessages() }
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

    @ViewBuilder private var messageList: some View {
        switch messages {
        case .loading:
            LoadingStateView()
        case .failed(let message):
            ErrorStateView(message: message) { Task { await load(showLoading: true) } }
        case .loaded(let list) where list.isEmpty:
            EmptyStateView(message: "No messages yet.")
        case .loaded(let list):
            VStack(spacing: 8) {
                ForEach(Array(list.enumerated()), id: \.element.id) { index, message in
                    if index == 0 || !env.clock.calendar.isDate(list[index - 1].sentAt, inSameDayAs: message.sentAt) {
                        Text(dayLabel(message.sentAt))
                            .socialText(12)
                            .foregroundStyle(Theme.text3)
                            .frame(maxWidth: .infinity)
                            .accessibilityAddTraits(.isHeader)
                    }
                    bubble(message)
                }
            }
        }
    }

    private func bubble(_ message: Message) -> some View {
        let mine = message.senderId == env.user?.id
        return VStack(alignment: mine ? .trailing : .leading, spacing: 2) {
            if !mine && thread.isGroup {
                Text(message.senderName)
                    .socialText(11)
                    .foregroundStyle(Theme.text3)
                    .padding(.horizontal, 10)
            }
            BubbleWidth(maxWidth: 260) {
                Text(message.text)
                    .socialText(15)
                    .foregroundStyle(Theme.ink)
            }
            .padding(.horizontal, 13)
            .padding(.vertical, 9)
            .background(mine ? Theme.sage : .white, in: RoundedRectangle(cornerRadius: 18, style: .continuous))
        }
        .frame(maxWidth: .infinity, alignment: mine ? .trailing : .leading)
        .id(message.id)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(mine ? "You" : message.senderName): \(message.text)")
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
        guard let last = messages.value?.last?.id else { return }
        DispatchQueue.main.async {
            if animated && !reduceMotion {
                withAnimation(.easeOut(duration: 0.25)) { proxy.scrollTo(last, anchor: .bottom) }
            } else {
                proxy.scrollTo(last, anchor: .bottom)
            }
        }
    }

    // MARK: Composer

    private var composer: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let sendError {
                Text(sendError)
                    .socialText(12)
                    .foregroundStyle(Theme.dangerText)
                    .padding(.horizontal, 4)
            }
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
                .disabled(sending)
                .accessibilityLabel("Send message")
            }
            .frame(height: 44)
        }
        .padding(.horizontal, 12)
        .padding(.top, 10)
        // Prototype: the 84pt bar's field sits 4pt into the home-indicator area.
        .padding(.bottom, keyboardShown ? 8 : -4)
        .frame(maxWidth: .infinity)
        .background(Color.white.ignoresSafeArea(edges: .bottom))
        .overlay(alignment: .top) { Rectangle().fill(Theme.line).frame(height: 1) }
    }

    private func send() {
        let text = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, !sending else { return }
        sending = true
        sendError = nil
        draft = ""
        Task {
            do {
                let message = try await env.api.sendMessage(threadId: thread.id, text: text)
                append(message)
            } catch {
                if draft.isEmpty { draft = text }
                sendError = error.socialMessage
            }
            sending = false
        }
    }

    // MARK: Loading

    private func load(showLoading: Bool = false) async {
        if showLoading || messages.value == nil { messages = .loading }
        messages = await Loadable.run { try await env.api.messages(threadId: thread.id, before: nil) }
    }

    private func listenForMessages() async {
        for await event in env.realtime.subscribe() {
            if case .messageNew(let threadId, let message) = event, threadId == thread.id {
                append(message)
            }
        }
    }

    private func append(_ message: Message) {
        guard var list = messages.value, !list.contains(where: { $0.id == message.id }) else { return }
        list.append(message)
        if reduceMotion {
            messages = .loaded(list)
        } else {
            withAnimation(.easeOut(duration: 0.2)) { messages = .loaded(list) }
        }
    }
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
