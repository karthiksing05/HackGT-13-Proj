import Foundation

/// Trip types rated 1–5 in Setup › "What do you enjoy?".
enum TripType: String, Codable, CaseIterable, Identifiable, CodingKeyRepresentable {
    // Used as dictionary keys: keep camelCase raw values — the snake_case JSON strategy converts
    // dictionary keys both ways ("live_music" on the wire).
    case outdoors, food, museums, liveMusic, nightlife, sports, shopping, bigCrowds, earlyMornings, longWalks

    var id: String { rawValue }

    var label: String {
        switch self {
        case .outdoors: "Outdoors & parks"
        case .food: "Food & drinks"
        case .museums: "Museums & art"
        case .liveMusic: "Live music"
        case .nightlife: "Nightlife"
        case .sports: "Sports & games"
        case .shopping: "Shopping & markets"
        case .bigCrowds: "Big crowds"
        case .earlyMornings: "Early mornings"
        case .longWalks: "Long walks"
        }
    }

    /// Word for each value on the 1–5 scale.
    static func scaleLabel(_ value: Int) -> String {
        ["", "Not for me", "Rather not", "Neutral", "Like it", "Love it"][max(0, min(5, value))]
    }
}

enum Company: String, Codable, CaseIterable, Identifiable {
    case solo
    case smallGroup = "small_group", bigGroup = "big_group"
    var id: String { rawValue }
    var label: String {
        switch self {
        case .solo: "Solo"
        case .smallGroup: "Small group"
        case .bigGroup: "Big group"
        }
    }
}

enum Pace: String, Codable, CaseIterable, Identifiable {
    case relaxed, balanced, packed
    var id: String { rawValue }
    /// Setup › "Your usual pace".
    var setupLabel: String {
        switch self {
        case .relaxed: "Chill"
        case .balanced: "Balanced"
        case .packed: "Packed"
        }
    }
    /// Create › More options › "Pace".
    var optionLabel: String {
        switch self {
        case .relaxed: "Relaxed"
        case .balanced: "Balanced"
        case .packed: "Packed"
        }
    }
}

/// Setup › "Typical spend per sidequest". Becomes the default budget in Create.
enum SpendTier: String, Codable, CaseIterable, Identifiable {
    case freeOnly = "free_only", under15 = "under_15", from15to40 = "15_to_40", over40 = "over_40"
    var id: String { rawValue }
    var label: String {
        switch self {
        case .freeOnly: "Free only"
        case .under15: "Under $15"
        case .from15to40: "$15–40"
        case .over40: "$40+"
        }
    }
    /// Index into Create › Budget (Free / $ / $$ / $$$).
    var defaultBudget: Int {
        switch self {
        case .freeOnly: 0
        case .under15: 1
        case .from15to40: 2
        case .over40: 3
        }
    }
}

enum Flexibility: String, Codable, CaseIterable, Identifiable {
    case stickToBudget = "stick_to_budget", bitOverOK = "bit_over_ok"
    var id: String { rawValue }
    var label: String {
        switch self {
        case .stickToBudget: "Stick to my budget"
        case .bitOverOK: "A bit over is OK"
        }
    }
}

enum SplitStyle: String, Codable, CaseIterable, Identifiable {
    case equally
    case payOwn = "pay_own", takeTurns = "take_turns"
    var id: String { rawValue }
    var label: String {
        switch self {
        case .equally: "Split equally"
        case .payOwn: "Pay my own"
        case .takeTurns: "Take turns"
        }
    }
}

struct Preferences: Codable, Hashable {
    var ratings: [TripType: Int] = [:]
    var company: Company = .smallGroup
    var pace: Pace = .balanced
    var spend: SpendTier = .under15
    var flexibility: Flexibility = .bitOverOK
    var splitStyle: SplitStyle = .equally
    var preferFree: Bool = true
    /// Open-ended answers from Setup step 5, keyed "perfectAfternoon", "neverDo", "planAround".
    var answers: [String: String] = [:]
}

struct TasteBar: Codable, Hashable, Identifiable {
    var id: String { label }
    var label: String
    /// 0…1
    var value: Double
}

struct TasteProfile: Codable, Hashable {
    var bars: [TasteBar]
}

enum CalendarProvider: String, Codable, CaseIterable, Identifiable {
    case google, outlook
    var id: String { rawValue }
    var name: String {
        switch self {
        case .google: "Google Calendar"
        case .outlook: "Outlook Calendar"
        }
    }
}

struct Integration: Codable, Hashable, Identifiable {
    var provider: CalendarProvider
    var connected: Bool
    var id: String { provider.rawValue }
}

struct PaymentMethod: Codable, Hashable, Identifiable {
    let id: String
    var brand: String
    var last4: String
    var isDefault: Bool
}
