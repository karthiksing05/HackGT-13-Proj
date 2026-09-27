import Foundation

// MARK: - Agentic checkout (Muse buys a plan's tickets)
//
// After a plan is saved, Muse can buy the tickets for its paid stops on the SideQuestz Events
// sandbox merchant (events.sidequestz.tech) within one budget the user approves with Face ID.
// Payment is Stripe test mode: no real money moves. `GET /itineraries/{id}/checkout` says what it
// would buy; `POST /itineraries/{id}/checkout-runs` starts it; each item is a `CheckoutIntent`
// (with `runId`), and `checkout.run` / `checkout.status` events follow it.

/// One paid stop Muse can buy tickets for.
struct CheckoutPlanItem: Codable, Identifiable, Hashable {
    var itemId: String
    var title: String
    var start: Date
    /// "events.sidequestz.tech"
    var merchant: String
    var ticketURL: URL?
    /// Per ticket, before fees; nil = unknown.
    var priceCents: Int?
    /// Suggested number of tickets.
    var quantity: Int
    /// You already have a ticket for it.
    var booked: Bool
    var intentState: String?
    var confirmation: String?

    var id: String { itemId }

    enum CodingKeys: String, CodingKey {
        case itemId, title, start, merchant, ticketURL = "ticketUrl", priceCents, quantity, booked, intentState, confirmation
    }
}

extension CheckoutPlanItem {
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        itemId = try c.decode(String.self, forKey: .itemId)
        title = try c.decode(String.self, forKey: .title)
        start = try c.decode(Date.self, forKey: .start)
        merchant = try c.decodeIfPresent(String.self, forKey: .merchant) ?? ""
        ticketURL = try c.decodeIfPresent(URL.self, forKey: .ticketURL)
        priceCents = try c.decodeIfPresent(Int.self, forKey: .priceCents)
        quantity = try c.decodeIfPresent(Int.self, forKey: .quantity) ?? 1
        booked = try c.decodeIfPresent(Bool.self, forKey: .booked) ?? false
        intentState = try c.decodeIfPresent(String.self, forKey: .intentState)
        confirmation = try c.decodeIfPresent(String.self, forKey: .confirmation)
    }
}

/// What agentic checkout would buy for a plan, and with what.
struct CheckoutPlan: Codable, Hashable {
    var itineraryId: String
    /// The server can run agentic checkout (Stripe and the merchant are set up).
    var available: Bool
    /// The user switched agentic checkout on (Account › Payments).
    var agenticCheckout: Bool
    var items: [CheckoutPlanItem]
    /// Tickets before fees.
    var estimateCents: Int
    var defaultBudgetCents: Int
    var suggestedBudgetCents: Int
    var paymentMethodId: String?
    var cardBrand: String?
    var cardLast4: String?
    /// A run already going for this plan: follow it instead of starting another.
    var activeRunId: String?

    /// Stops that still need tickets.
    var buyable: [CheckoutPlanItem] { items.filter { !$0.booked } }
    /// Worth asking "Let Muse get your tickets?"
    var shouldPrompt: Bool { available && !buyable.isEmpty && paymentMethodId != nil }
}

extension CheckoutPlan {
    enum CodingKeys: String, CodingKey {
        case itineraryId, available, agenticCheckout, items, estimateCents, defaultBudgetCents, suggestedBudgetCents
        case paymentMethodId, cardBrand, cardLast4, activeRunId
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        itineraryId = try c.decode(String.self, forKey: .itineraryId)
        available = try c.decodeIfPresent(Bool.self, forKey: .available) ?? false
        agenticCheckout = try c.decodeIfPresent(Bool.self, forKey: .agenticCheckout) ?? false
        items = try c.decodeIfPresent([CheckoutPlanItem].self, forKey: .items) ?? []
        estimateCents = try c.decodeIfPresent(Int.self, forKey: .estimateCents) ?? 0
        defaultBudgetCents = try c.decodeIfPresent(Int.self, forKey: .defaultBudgetCents) ?? 5000
        suggestedBudgetCents = try c.decodeIfPresent(Int.self, forKey: .suggestedBudgetCents) ?? defaultBudgetCents
        paymentMethodId = try c.decodeIfPresent(String.self, forKey: .paymentMethodId)
        cardBrand = try c.decodeIfPresent(String.self, forKey: .cardBrand)
        cardLast4 = try c.decodeIfPresent(String.self, forKey: .cardLast4)
        activeRunId = try c.decodeIfPresent(String.self, forKey: .activeRunId)
    }
}

/// `POST /itineraries/{id}/checkout-runs`: the one approval covering every listed item.
struct CreateCheckoutRun: Codable, Hashable {
    struct Item: Codable, Hashable {
        var itemId: String
        var quantity: Int
    }
    var budgetCents: Int
    var items: [Item]
    /// nil = the default card.
    var paymentMethodId: String?
}

/// Unknown states read as `running`, so nothing looks finished by mistake.
enum CheckoutRunState: String, Codable {
    case running, done, cancelled

    init(from decoder: Decoder) throws {
        self = CheckoutRunState(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .running
    }
}

/// An agentic checkout: the budget, what was spent, and each item's intent.
struct CheckoutRun: Codable, Identifiable, Hashable {
    let id: String
    var itineraryId: String
    var state: CheckoutRunState
    var budgetCents: Int
    var spentCents: Int
    var currency: String
    var cardBrand: String
    var cardLast4: String
    /// "muse", or "fallback" when Muse couldn't finish and the server did.
    var agent: String?
    /// Muse's note for the user, written from what actually happened.
    var summary: String?
    var intents: [CheckoutIntent]
    var createdAt: Date
    var finishedAt: Date?

    var isRunning: Bool { state == .running }
    var booked: [CheckoutIntent] { intents.filter { $0.state == .booked } }
    var notBooked: [CheckoutIntent] { intents.filter { $0.state == .failed || $0.state == .cancelled } }
}

extension CheckoutRun {
    enum CodingKeys: String, CodingKey {
        case id, itineraryId, state, budgetCents, spentCents, currency, cardBrand, cardLast4, agent, summary, intents, createdAt, finishedAt
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        itineraryId = try c.decode(String.self, forKey: .itineraryId)
        state = try c.decode(CheckoutRunState.self, forKey: .state)
        budgetCents = try c.decodeIfPresent(Int.self, forKey: .budgetCents) ?? 0
        spentCents = try c.decodeIfPresent(Int.self, forKey: .spentCents) ?? 0
        currency = try c.decodeIfPresent(String.self, forKey: .currency) ?? "usd"
        cardBrand = try c.decodeIfPresent(String.self, forKey: .cardBrand) ?? ""
        cardLast4 = try c.decodeIfPresent(String.self, forKey: .cardLast4) ?? ""
        agent = try c.decodeIfPresent(String.self, forKey: .agent)
        summary = try c.decodeIfPresent(String.self, forKey: .summary)
        intents = try c.decodeIfPresent([CheckoutIntent].self, forKey: .intents) ?? []
        createdAt = try c.decode(Date.self, forKey: .createdAt)
        finishedAt = try c.decodeIfPresent(Date.self, forKey: .finishedAt)
    }
}

extension CheckoutIntent {
    /// Why an agentic purchase didn't go through, for the results sheet.
    var outcomeLine: String {
        if let failureReason, !failureReason.isEmpty { return failureReason }
        switch failureCode {
        case "sold_out": return "Sold out."
        case "price_changed": return "The price changed."
        case "over_budget": return "Over your budget."
        case "declined": return "The payment limit stopped a charge above the quote."
        case "card_declined": return "Your card was declined."
        case "skipped": return "Skipped."
        case "cancelled": return "Cancelled."
        default: return state == .cancelled ? "Cancelled." : "Couldn't buy it."
        }
    }
}
