import Foundation

/// Home › Past › "What you like": what your best-rated sidequests have in common, computed by the
/// server from your ratings and history (`GET /me/insights`).
struct PastInsights: Codable, Hashable {
    /// One sentence ("You like evening sidequests with friends.").
    var headline: String
    /// 2–4 facts, e.g. "Favorite time · Evenings · 2 of your 3 favorites".
    var highlights: [PastInsight] = []
    /// Tags from your 4–5 star ratings, most common first.
    var topTags: [String] = []
    /// Rated sidequests this is based on (0 = not enough ratings yet; show `headline` as a nudge).
    var basedOn: Int = 0
}

struct PastInsight: Codable, Hashable, Identifiable {
    let id: String
    /// "Favorite time"
    var title: String
    /// "Evenings"
    var value: String
    /// "2 of your 3 favorites"
    var detail: String?
    /// SF Symbol for the tile ("moon.stars", "person.2", "mappin.and.ellipse", "star").
    var symbol: String?
}

/// Home › search: everything matching a query (`GET /search?q=`).
struct SearchResults: Codable, Hashable {
    var sidequests: [Itinerary] = []
    var people: [UserSearchResult] = []
    var places: [Place] = []
    var posts: [ForumPost] = []

    var isEmpty: Bool { sidequests.isEmpty && people.isEmpty && places.isEmpty && posts.isEmpty }
}

extension PastInsights {
    enum CodingKeys: String, CodingKey {
        case headline, highlights, topTags, basedOn
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        headline = try c.decodeIfPresent(String.self, forKey: .headline) ?? ""
        highlights = try c.decodeIfPresent([PastInsight].self, forKey: .highlights) ?? []
        topTags = try c.decodeIfPresent([String].self, forKey: .topTags) ?? []
        basedOn = try c.decodeIfPresent(Int.self, forKey: .basedOn) ?? 0
    }
}

extension SearchResults {
    enum CodingKeys: String, CodingKey {
        case sidequests, people, places, posts
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        sidequests = try c.decodeIfPresent([Itinerary].self, forKey: .sidequests) ?? []
        people = try c.decodeIfPresent([UserSearchResult].self, forKey: .people) ?? []
        places = try c.decodeIfPresent([Place].self, forKey: .places) ?? []
        posts = try c.decodeIfPresent([ForumPost].self, forKey: .posts) ?? []
    }
}
