import AuthenticationServices
import SwiftUI

/// One agent checkout for an item, from creating the intent until it's booked, failed or cancelled
/// (API_CONTRACT.md › Agent checkout). `HomeStore` keeps it per item, so a purchase that's still
/// paying when its sheets close keeps being followed ("Get tickets" reads "Booking" until it lands)
/// and reopening the item picks it up again instead of starting a second one.
///
/// States: `preparing` → `awaitingApproval` → `processing` → `booked`, or `failed` / `cancelled`.
/// While the intent is in progress the session follows it: `checkout.status` events for it plus a
/// fresh copy every 2 s. Nothing is spent until `approve()`, unless the server did an instant
/// checkout (`CheckoutIntent.instant`, within the user's limit).
@Observable
final class HomeCheckoutSession {
    let item: ItineraryItem
    private(set) var intent: Loadable<CheckoutIntent> = .loading
    private(set) var isApproving = false
    private(set) var approveError: String?
    /// Your saved cards, for Checkout › "Change".
    private(set) var cards: Loadable<[PaymentMethod]> = .loading
    /// A card switch (or adding one) is on its way to the server.
    private(set) var isChangingCard = false
    private(set) var cardError: String?
    /// Instant checkout was asked for (the user has it on); the server decides whether it applies.
    private(set) var requestedInstant = false
    /// Once booked: the item's ticket as the server has it (nil inside = no ticket on it yet).
    private(set) var ticket: Loadable<Ticket?>?
    /// Once booked: the item reloaded from the server (it carries the ticket).
    private(set) var bookedItem: ItineraryItem?
    /// Called once when the purchase is booked, with the reloaded item (nil if it couldn't be loaded).
    @ObservationIgnored var onBooked: ((ItineraryItem?) -> Void)?

    @ObservationIgnored private let env: AppEnvironment
    @ObservationIgnored private var followTask: Task<Void, Never>?
    @ObservationIgnored private var followingId: String?
    @ObservationIgnored private var bookedHandled = false
    @ObservationIgnored private var started = false
    /// The sheet closed while the intent was still being created: drop it once it exists.
    @ObservationIgnored private var abandoned = false
    /// Closed before paying and cancelled: a new "Get tickets" starts over.
    private(set) var dropped = false

    init(item: ItineraryItem, env: AppEnvironment) {
        self.item = item
        self.env = env
    }

    var state: CheckoutState? { intent.value?.state }
    /// Money is moving (approval sent, or the agent is paying).
    var isPaying: Bool { isApproving || state == .processing }
    /// Closing now leaves nothing to finish: the intent can simply be cancelled.
    var isPending: Bool { !isApproving && (state == .preparing || state == .awaitingApproval) }
    var canApprove: Bool { state == .awaitingApproval && !isApproving && !isChangingCard }
    var canChangeCard: Bool { (state == .preparing || state == .awaitingApproval) && !isApproving }
    /// Still going: being created, waiting for approval, or paying.
    var isOngoing: Bool {
        guard !dropped else { return false }
        if intent.isLoading { return true }
        return isApproving || state?.isInProgress == true || state == .awaitingApproval
    }
    /// The server skipped approval (instant checkout).
    var isInstant: Bool { intent.value?.instant == true }

    // MARK: Starting

    /// The Checkout sheet is on screen (again).
    func sheetOpened() {
        abandoned = false
    }

    /// Creates the intent the first time the sheet shows. The session owns the request, so closing
    /// the sheet meanwhile doesn't cancel it (the intent is dropped once it exists instead).
    func startIfNeeded() {
        guard !started else { return }
        started = true
        Task { await start() }
    }

    /// "Try again" / "Start again".
    func restart() {
        Task { await start() }
    }

    /// Creates the intent (again after a failure or a cancel, with the card picked last) and loads
    /// your cards alongside. With instant checkout on, the server may skip approval.
    func start() async {
        stopFollowing()
        bookedHandled = false
        dropped = false
        let card = intent.value?.paymentMethodId
        withMotion {
            intent = .loading
            isApproving = false
            approveError = nil
            cardError = nil
            ticket = nil
            bookedItem = nil
        }
        let instant = env.preferences?.instantCheckout == true
        requestedInstant = instant
        async let cardsLoaded: Void = loadCards()
        let result = await Loadable.run { [env, item] in
            try await env.api.createCheckoutIntent(itemId: item.id, quantity: 1, paymentMethodId: card, instant: instant)
        }
        withMotion(Motion.gentle) { intent = result }
        if let created = result.value {
            settle(created)
            // Closed while it was being made: nothing was approved, so drop it (unless the server
            // already went ahead with an instant purchase; that one is followed until it lands).
            if abandoned {
                abandoned = false
                cancelIfPending()
            }
        }
        _ = await cardsLoaded
    }

    func loadCards() async {
        let result = await Loadable.run { [env] in try await env.api.paymentMethods() }
        if case .failed = result, cards.value != nil { return }
        withMotion { cards = result }
    }

    // MARK: Approving

    /// The only step that spends money. Comes back `processing` (then `booked` via event / poll),
    /// or already settled.
    func approve() {
        guard canApprove, let current = intent.value else { return }
        withMotion {
            isApproving = true
            approveError = nil
        }
        Task {
            do {
                let updated = try await env.api.approveCheckout(id: current.id)
                withMotion(Motion.gentle) { isApproving = false }
                apply(updated)
            } catch {
                // It may have gone through anyway (say the response was lost): check before saying no.
                if let fresh = try? await env.api.checkoutIntent(id: current.id), fresh.state != .awaitingApproval {
                    withMotion(Motion.gentle) { isApproving = false }
                    apply(fresh)
                    return
                }
                withMotion(Motion.arrive) {
                    isApproving = false
                    approveError = Self.message(error, "Couldn't reach the checkout agent. Try again.")
                }
            }
        }
    }

    /// The Checkout sheet closed. A purchase that isn't approved yet is dropped (nothing was
    /// spent); one that's paying keeps being followed until it lands.
    func sheetClosed() {
        if intent.isLoading {
            abandoned = true
            return
        }
        cancelIfPending()
    }

    private func cancelIfPending() {
        guard isPending, let id = intent.value?.id else { return }
        stopFollowing()
        dropped = true
        let api = env.api
        Task { try? await api.cancelCheckout(id: id) }
    }

    // MARK: Card

    func isSelected(_ card: PaymentMethod, in current: CheckoutIntent) -> Bool {
        if let id = current.paymentMethodId { return id == card.id }
        return card.brand == current.cardBrand && card.last4 == current.cardLast4
    }

    /// Checkout › Change › a saved card. The row shows loading dots until the server switched it.
    func choose(_ card: PaymentMethod) {
        guard canChangeCard, !isChangingCard, let current = intent.value, !isSelected(card, in: current) else { return }
        withMotion(Motion.quick) {
            isChangingCard = true
            cardError = nil
        }
        Task {
            do {
                let updated = try await env.api.updateCheckoutIntent(id: current.id, paymentMethodId: card.id)
                withMotion { isChangingCard = false }
                apply(updated)
            } catch {
                withMotion(Motion.arrive) {
                    isChangingCard = false
                    cardError = Self.message(error, "Couldn't switch cards. Try again.")
                }
            }
        }
    }

    /// Checkout › Change › "Add a card": the hosted card page (a demo card in the demo), then pay
    /// with the card you just added.
    func addCard() {
        guard canChangeCard, !isChangingCard, let current = intent.value else { return }
        withMotion(Motion.quick) {
            isChangingCard = true
            cardError = nil
        }
        let known = cards.value.map { Set($0.map(\.id)) }
        Task {
            do {
                let list = try await PaymentMethodConnector.addCard(env: env)
                withMotion { cards = .loaded(list) }
                if let known, let added = list.first(where: { !known.contains($0.id) }),
                   canChangeCard, intent.value?.id == current.id {
                    let updated = try await env.api.updateCheckoutIntent(id: current.id, paymentMethodId: added.id)
                    apply(updated)
                }
                withMotion { isChangingCard = false }
            } catch {
                withMotion(Motion.arrive) {
                    isChangingCard = false
                    if !Self.isDismissal(error) { cardError = Self.message(error, "Couldn't add the card. Try again.") }
                }
            }
        }
    }

    /// The checkout failed because there's no card: add one, then start over with it.
    func addCardAndRestart() {
        guard !isChangingCard else { return }
        withMotion(Motion.quick) {
            isChangingCard = true
            cardError = nil
        }
        Task {
            do {
                let list = try await PaymentMethodConnector.addCard(env: env)
                withMotion {
                    cards = .loaded(list)
                    isChangingCard = false
                }
                await start()
            } catch {
                withMotion(Motion.arrive) {
                    isChangingCard = false
                    if !Self.isDismissal(error) { cardError = Self.message(error, "Couldn't add the card. Try again.") }
                }
            }
        }
    }

    // MARK: Following the intent

    func stopFollowing() {
        followTask?.cancel()
        followTask = nil
        followingId = nil
    }

    /// A fresh copy from the server. States only move forward (a slow poll can't undo an event),
    /// and a settled intent stays settled.
    private func apply(_ fresh: CheckoutIntent) {
        if let current = intent.value, current.id == fresh.id {
            let old = Self.rank(current.state), new = Self.rank(fresh.state)
            guard new > old || (new == old && (new < Self.settledRank || fresh.state == current.state)) else { return }
        }
        withMotion(Motion.gentle) { intent = .loaded(fresh) }
        settle(fresh)
    }

    /// `checkout.status` said where it is before the next fetch lands. Paying and booked show right
    /// away; a new quote (`awaitingApproval`) and a failure wait for the fetch, so Approve never
    /// shows a total you haven't seen and a failure always comes with its reason.
    private func hint(_ state: CheckoutState, id: String) {
        guard var current = intent.value, current.id == id, Self.rank(state) > Self.rank(current.state) else { return }
        switch state {
        case .processing, .booked:
            current.state = state
            if state == .booked { current.steps = current.steps.map { CheckoutStep(text: $0.text, done: true) } }
            withMotion(Motion.gentle) { intent = .loaded(current) }
            if state == .booked { handleBooked() }
        case .preparing, .awaitingApproval, .failed, .cancelled:
            break
        }
    }

    private func settle(_ current: CheckoutIntent) {
        if current.state.isInProgress { follow(current.id) } else { stopFollowing() }
        if current.state == .booked { handleBooked() }
    }

    private func follow(_ id: String) {
        guard followingId != id else { return }
        followTask?.cancel()
        followingId = id
        let env = env
        followTask = Task { [weak self] in
            await HomeCheckoutWatch.follow(id: id, env: env,
                                           hint: { self?.hint($0, id: id) },
                                           update: { self?.apply($0) })
        }
    }

    /// Booked: reload the item for its ticket (the server attaches it as the booking lands; ask
    /// again a couple of times if it isn't there yet), then tell whoever's listening.
    private func handleBooked() {
        guard !bookedHandled else { return }
        bookedHandled = true
        withMotion { ticket = .loading }
        let env = env, itemId = item.id
        Task {
            var fresh: ItineraryItem?
            for attempt in 0..<3 {
                if attempt > 0 { try? await Task.sleep(for: HomeCheckoutWatch.interval) }
                if let loaded = try? await env.api.eventDetail(id: itemId) {
                    fresh = loaded
                    if loaded.ticket != nil { break }
                }
            }
            withMotion(Motion.arrive) {
                ticket = .loaded(fresh?.ticket)
                bookedItem = fresh
            }
            onBooked?(fresh)
        }
    }

    // MARK: Helpers

    private static let settledRank = 3

    private static func rank(_ state: CheckoutState) -> Int {
        switch state {
        case .preparing: 0
        case .awaitingApproval: 1
        case .processing: 2
        case .booked, .failed, .cancelled: settledRank
        }
    }

    static func message(_ error: any Error, _ fallback: String) -> String {
        (error as? LocalizedError)?.errorDescription ?? fallback
    }

    /// Closing the hosted card page isn't an error worth a message.
    private static func isDismissal(_ error: any Error) -> Bool {
        (error as? ASWebAuthenticationSessionError)?.code == .canceledLogin
    }
}

/// Follows a checkout intent until it settles: `checkout.status` events for it (passed to `hint`
/// the moment they arrive), and a fresh copy every 2 s and right after each event (passed to
/// `update`). Returns once a fetched copy is no longer in progress, or when the task is cancelled.
enum HomeCheckoutWatch {
    static let interval: Duration = .seconds(2)

    static func follow(id: String, env: AppEnvironment,
                       hint: @escaping (CheckoutState) -> Void,
                       update: @escaping (CheckoutIntent) -> Void) async {
        // Wake-ups coalesce, so a slow request never queues a burst of polls behind it.
        let (wakeups, wake) = AsyncStream.makeStream(of: Void.self, bufferingPolicy: .bufferingNewest(1))
        let listener = Task {
            for await event in env.realtime.subscribe() {
                guard case .checkoutStatus(let intentId, let state) = event, intentId == id else { continue }
                hint(state)
                wake.yield()
            }
        }
        let ticker = Task {
            while !Task.isCancelled {
                try? await Task.sleep(for: interval)
                guard !Task.isCancelled else { break }
                wake.yield()
            }
        }
        defer {
            listener.cancel()
            ticker.cancel()
            wake.finish()
        }
        for await _ in wakeups {
            guard let fresh = try? await env.api.checkoutIntent(id: id), !Task.isCancelled else { continue }
            update(fresh)
            if !fresh.state.isInProgress { return }
        }
    }
}
