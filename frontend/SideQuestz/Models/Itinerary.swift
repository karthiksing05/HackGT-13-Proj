import Foundation

enum BlockKind: String, Codable, CaseIterable {
    case busy, sidequest, transit, group
}

/// Who can see a plan (Create › Vibe › "Who's coming").
enum Visibility: String, Codable, CaseIterable, Identifiable {
    case justMe = "just_me", friends, open
    var id: String { rawValue }
    var label: String {
        switch self {
        case .justMe: "Just me"
        case .friends: "Friends only"
        case .open: "Open to all"
        }
    }
    var note: String {
        switch self {
        case .justMe: "Private. Nobody else sees this plan."
        case .friends: "Posted to the Forum, but only your friends can see and join it."
        case .open: "Posted to the Forum for anyone nearby to request to join."
        }
    }
}

struct Rating: Codable, Hashable {
    var stars: Int
    var tags: [String]
    var note: String?

    /// "Would go again · Great people · “note”"
    var tagLine: String {
        (tags + [note.flatMap { $0.isEmpty ? nil : "“\($0)”" }].compactMap { $0 }).joined(separator: " · ")
    }

    static let starWords = ["Tap a star", "Not great", "Meh", "It was fine", "Really good", "Loved it"]
    static let tagOptions = ["Would go again", "Great people", "Good value", "Too crowded", "Too pricey", "Hard to get to"]
}

/// One block on a timeline (and the model behind the Event sheet).
struct ItineraryItem: Codable, Identifiable, Hashable {
    let id: String
    var kind: BlockKind
    var title: String
    /// Display place ("Ponce City Market, rooftop", "Tech Square → Ponce City Market").
    var place: Place?
    var start: Date
    var end: Date
    var description: String?
    var websiteURL: URL?
    var bookable: Bool = false
    /// nil = unknown real price → show the "$[price]" placeholder, never invent one.
    var priceCents: Int?
    var people: [PersonRef] = []
    var interested: [PersonRef] = []
    /// People going beyond `people` (e.g. "8 going" with 3 avatars shown).
    var extraGoing: Int = 0
    var notes: String?
    /// Who sees `notes`: only you, or everyone on the plan.
    var notesScope: NotesScope?
    var rating: Rating?
    /// Your saved "Getting there" choice.
    var transitMode: TravelMode?
    /// Set once it's booked (agent checkout): "Get tickets" becomes the ticket.
    var ticket: Ticket?

    var goingCount: Int { people.count + extraGoing }
    var hasPeople: Bool { !people.isEmpty || !interested.isEmpty }
    /// "3 going · 1 interested"
    var peopleLine: String { "\(goingCount) going · \(interested.count) interested" }
}

enum NotesScope: String, Codable, Hashable {
    case personal = "private", shared
}

/// A booked ticket for an item.
struct Ticket: Codable, Hashable {
    var id: String
    var quantity: Int = 1
    var totalCents: Int?
    /// "SQ-4F7K2"
    var confirmation: String?
    /// The ticket itself (PDF / pass / web page).
    var url: URL?
}

struct Itinerary: Codable, Identifiable, Hashable {
    let id: String
    var title: String
    var date: Date
    var start: Date
    var backBy: Date
    var startPlace: Place
    var endPlace: Place
    var visibility: Visibility
    var lockAt: Date?
    var maxGroupSize: Int?
    var items: [ItineraryItem]
    /// People on the plan including you; 1 = "Solo".
    var goingCount: Int = 1
    /// You made this plan, so you can edit or delete it. People who joined can only leave.
    var isHost: Bool = true

    /// Sidequest + group blocks ("3 stops").
    var stopCount: Int { items.filter { $0.kind == .sidequest || $0.kind == .group }.count }
    var peopleLabel: String { goingCount > 1 ? "\(goingCount) going" : "Solo" }
}

/// `PATCH /itineraries/{id}` (Home › Edit sidequest). Only the fields that are set are sent and
/// changed. A new date, time window or stop order makes the server re-time the route; the response
/// is the updated itinerary.
struct ItineraryUpdate: Codable, Hashable {
    var title: String?
    var date: Date?
    var start: Date?
    var backBy: Date?
    var visibility: Visibility?
    /// The stop item ids (sidequest and group blocks) in their new order. Stops left out are removed.
    var stopOrder: [String]?

    var isEmpty: Bool {
        title == nil && date == nil && start == nil && backBy == nil && visibility == nil && stopOrder == nil
    }
}

/// Walk / MARTA / Rideshare choices in the Event sheet › "Getting there".
struct TransitOption: Codable, Hashable, Identifiable {
    var mode: TravelMode
    var minutes: Int
    /// nil = unknown fare → "[fare]"; 0 = "Free".
    var costCents: Int?
    var id: String { mode.rawValue }
}

// MARK: - Calendar (Home › Calendar, Create › When dropdown)

struct CalendarItem: Codable, Identifiable, Hashable {
    let id: String
    var kind: BlockKind
    var title: String
    var start: Date
    var end: Date
    var people: [PersonRef] = []
    var interested: [PersonRef] = []
    /// The plan this block belongs to (nil for calendar-only events), so its notes and transit go
    /// to `/itineraries/{id}/items/…`.
    var itineraryId: String?
}

struct CalendarDay: Codable, Identifiable, Hashable {
    /// "2026-09-25"
    let id: String
    var date: Date
    var items: [CalendarItem]
}

// MARK: - Past events + ratings

struct PastEvent: Codable, Identifiable, Hashable {
    let id: String
    var title: String
    /// "Tech Square"
    var place: String
    /// "with 3 others" / "solo"
    var company: String
    var date: Date
    var kind: BlockKind
    var rating: Rating?

    /// "Tech Square · with 3 others" (Home › Past rows, Rate sheet caption).
    var subtitle: String { "\(place) · \(company)" }
}

// MARK: - Checkout (Visa agent)

/// The agent works in the background: `preparing` (finding the tickets, building the quote) →
/// `awaitingApproval` → `processing` (paying) → `booked`, or `failed` / `cancelled`. Unknown states
/// read as `processing`, so nothing ever looks finished (or failed) by mistake.
enum CheckoutState: String, Codable {
    case preparing, awaitingApproval = "awaiting_approval", processing, booked, cancelled, failed

    init(from decoder: Decoder) throws {
        self = CheckoutState(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .processing
    }

    /// Still moving: keep listening for `checkout.status` / polling.
    var isInProgress: Bool { self == .preparing || self == .processing }
}

struct CheckoutStep: Codable, Hashable {
    var text: String
    var done: Bool
}

struct CheckoutIntent: Codable, Identifiable, Hashable {
    let id: String
    var itemId: String
    var itemTitle: String
    var steps: [CheckoutStep]
    /// nil = unknown → "$[price]" / "[fees]" / "[total]" placeholders.
    var subtotalCents: Int?
    var feesCents: Int?
    var totalCents: Int?
    var cardBrand: String
    var cardLast4: String
    var state: CheckoutState
    var quantity: Int = 1
    /// The saved card the agent pays with (change it with `updateCheckoutIntent`).
    var paymentMethodId: String?
    /// Why it failed, in a sentence ("The show sold out.").
    var failureReason: String?
    /// Instant checkout: the server skipped approval (it goes straight to `processing`).
    var instant: Bool = false
}
