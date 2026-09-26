import Foundation

// Facebook (Graph API) as a source of taste context.
//
// The backend runs everything that touches Facebook: the Login dialog and its callback (it holds the
// app secret), the user token, the Graph API reads, storing what it read, and turning that into
// suggested ratings. The app starts the flow, shows what was found and saves the ratings the person
// accepts with `PUT /me/preferences`. It never posts to Facebook. See API_CONTRACT.md › "Facebook".

/// `GET /integrations/facebook`
struct FacebookConnection: Codable, Hashable {
    var connected: Bool
    /// Facebook stopped accepting the stored token (expired, password changed, app removed there):
    /// the person has to connect again before the next import.
    var needsReconnect: Bool = false
    /// The name on the Facebook account ("Connected as …").
    var name: String?
    /// Permissions the person turned off in Facebook's dialog (`user_likes`, `user_location`,
    /// `user_friends`). Connecting with `rerequest` asks for them again.
    var declinedScopes: [String] = []
    /// The latest import, when there is one.
    var lastImport: FacebookImport?
}

/// What an import read from Facebook and what it suggests (`POST /integrations/facebook/import`).
struct FacebookImport: Codable, Hashable {
    var importedAt: Date
    /// How many liked Pages the suggestions come from.
    var likedPages: Int
    /// Suggested 1–5 ratings for "What do you enjoy?" (only the trip types there was evidence for).
    var suggestedRatings: [TripType: Int] = [:]
    /// Interests in plain words, for showing what was found ("Hiking", "Indie rock").
    var interests: [String] = []
    /// Current city, if shared ("Atlanta, Georgia").
    var homeArea: String?
    /// Facebook friends who also use SideQuests, with how you're connected here.
    var friendsOnApp: [UserSearchResult] = []
}

extension FacebookConnection {
    enum CodingKeys: String, CodingKey {
        case connected, needsReconnect, name, declinedScopes, lastImport
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        connected = try c.decodeIfPresent(Bool.self, forKey: .connected) ?? false
        needsReconnect = try c.decodeIfPresent(Bool.self, forKey: .needsReconnect) ?? false
        name = try c.decodeIfPresent(String.self, forKey: .name)
        declinedScopes = try c.decodeIfPresent([String].self, forKey: .declinedScopes) ?? []
        lastImport = try c.decodeIfPresent(FacebookImport.self, forKey: .lastImport)
    }

    /// Declined permissions in words, for "You didn't share …" ("Pages you like", "your city").
    var declinedLabels: [String] {
        declinedScopes.compactMap { scope in
            switch scope {
            case "user_likes": "Pages you like"
            case "user_location": "your city"
            case "user_friends": "friends who use SideQuests"
            default: nil
            }
        }
    }
}

extension FacebookImport {
    enum CodingKeys: String, CodingKey {
        case importedAt, likedPages, suggestedRatings, interests, homeArea, friendsOnApp
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        importedAt = try c.decode(Date.self, forKey: .importedAt)
        likedPages = try c.decodeIfPresent(Int.self, forKey: .likedPages) ?? 0
        // Unknown trip types and out-of-range values are dropped rather than failing the import.
        // (String-keyed dictionaries skip the snake_case key conversion, so "live_music" is mapped here.)
        let raw = try c.decodeIfPresent([String: Int].self, forKey: .suggestedRatings) ?? [:]
        suggestedRatings = raw.reduce(into: [:]) { result, entry in
            if let type = TripType(wireKey: entry.key), (1...5).contains(entry.value) { result[type] = entry.value }
        }
        interests = try c.decodeIfPresent([String].self, forKey: .interests) ?? []
        homeArea = try c.decodeIfPresent(String.self, forKey: .homeArea)
        friendsOnApp = try c.decodeIfPresent([UserSearchResult].self, forKey: .friendsOnApp) ?? []
    }
}

extension TripType {
    /// "live_music" (the wire) or "liveMusic".
    init?(wireKey: String) {
        let parts = wireKey.split(separator: "_").map(String.init)
        let camel = (parts.first ?? "") + parts.dropFirst().map { $0.prefix(1).uppercased() + $0.dropFirst() }.joined()
        self.init(rawValue: camel)
    }
}

extension Preferences {
    /// These preferences with suggested ratings merged in, and which trip types changed.
    /// `overwrite: false` fills only unrated types (Setup: never replaces a rating you picked);
    /// `true` replaces them too (Account, after you've seen each change).
    func merging(_ suggestions: [TripType: Int], overwrite: Bool) -> (preferences: Preferences, changed: Set<TripType>) {
        var merged = self
        var changed = Set<TripType>()
        for (type, value) in suggestions where (1...5).contains(value) {
            let current = ratings[type]
            guard current != value, overwrite || current == nil else { continue }
            merged.ratings[type] = value
            changed.insert(type)
        }
        return (merged, changed)
    }
}
