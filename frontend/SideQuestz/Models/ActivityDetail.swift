import Foundation

/// Everything the user's catalog knows about one event or place (`GET /activities/{id}`, the id is a
/// stop's `activityId`): what Create › Review's stop pane shows beyond the stop itself. The server
/// writes the labels; the app shows them as they come.
struct ActivityDetail: Codable, Identifiable, Hashable {
    let id: String
    var title: String
    var kind: PlanStopKind
    /// The catalog's category ("live_music"); the pane shows `categoryLabel`.
    var category: String = ""
    /// The category the way stop subtitles say it: "Live music", "Games + views".
    var categoryLabel: String = ""
    var summary: String? = nil
    var description: String? = nil
    var venueName: String? = nil
    /// One line: "675 Ponce De Leon Ave NE, Atlanta, GA 30308".
    var address: String? = nil
    /// Where it is: a place under its own name, an event under its venue's.
    var place: Place
    /// Events only: when it starts, and ends when known.
    var start: Date? = nil
    var end: Date? = nil
    /// The opening hours on the plan's day ("Open 10 AM–6 PM", "Closed that day"); nil when unknown.
    var hoursLine: String? = nil
    /// The cheapest known price; nil when it isn't known.
    var priceCents: Int? = nil
    /// "Free", "$18", "$6–$12", "$$" (only the tier is known) or "Price unknown".
    var priceLabel: String = ""
    /// Out of 5, with how many ratings it's from.
    var rating: Double? = nil
    var ratingCount: Int? = nil
    var websiteURL: URL? = nil
    var ticketURL: URL? = nil
    var imageURL: URL? = nil
    var tags: [String] = []
}

extension ActivityDetail {
    enum CodingKeys: String, CodingKey {
        case id, title, kind, category, categoryLabel, summary, description, venueName, address, place, start, end
        case hoursLine, priceCents, priceLabel, rating, ratingCount, tags
        case websiteURL = "url", ticketURL = "ticketUrl", imageURL = "imageUrl"
    }

    /// Only `id` and `title` are required. A missing or unknown `kind` reads as an event when there's
    /// a start, else as a place; a missing `place` is the venue (else the title) without a
    /// coordinate; blank text is nil; a link the in-app browser can't open (not http or https) is
    /// dropped rather than failing the whole detail.
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        func text(_ key: CodingKeys) throws -> String? {
            let value = try c.decodeIfPresent(String.self, forKey: key)?.trimmingCharacters(in: .whitespacesAndNewlines)
            return value?.isEmpty == false ? value : nil
        }
        id = try c.decode(String.self, forKey: .id)
        title = try c.decode(String.self, forKey: .title)
        start = try c.decodeIfPresent(Date.self, forKey: .start)
        end = try c.decodeIfPresent(Date.self, forKey: .end)
        kind = try c.decodeIfPresent(String.self, forKey: .kind).flatMap(PlanStopKind.init(rawValue:)) ?? (start == nil ? .place : .event)
        category = try c.decodeIfPresent(String.self, forKey: .category) ?? ""
        categoryLabel = try text(.categoryLabel) ?? ""
        summary = try text(.summary)
        description = try text(.description)
        venueName = try text(.venueName)
        address = try text(.address)
        place = try c.decodeIfPresent(Place.self, forKey: .place) ?? Place(name: venueName ?? title)
        hoursLine = try text(.hoursLine)
        priceCents = try c.decodeIfPresent(Int.self, forKey: .priceCents)
        priceLabel = try text(.priceLabel) ?? ""
        rating = try c.decodeIfPresent(Double.self, forKey: .rating)
        ratingCount = try c.decodeIfPresent(Int.self, forKey: .ratingCount)
        websiteURL = Self.webLink(try text(.websiteURL))
        ticketURL = Self.webLink(try text(.ticketURL))
        imageURL = Self.webLink(try text(.imageURL))
        tags = try c.decodeIfPresent([String].self, forKey: .tags) ?? []
    }

    /// An http(s) link, or nil.
    static func webLink(_ text: String?) -> URL? {
        guard let text, let url = URL(string: text), let scheme = url.scheme?.lowercased(),
              scheme == "http" || scheme == "https", url.host?.isEmpty == false else { return nil }
        return url
    }

    /// "4.6 ★ (1,204)", "4.6 ★" without a count; nil without a rating.
    var ratingLine: String? {
        guard let rating else { return nil }
        let stars = String(format: "%.1f ★", rating)
        guard let ratingCount, ratingCount > 0 else { return stars }
        return "\(stars) (\(ratingCount.formatted(.number.locale(Locale(identifier: "en_US")))))"
    }

    /// The summary, then the description when it says more (the description often starts with it).
    var about: String? {
        switch (summary, description) {
        case let (summary?, description?): description.hasPrefix(summary) ? description : "\(summary)\n\n\(description)"
        case let (summary, description): description ?? summary
        }
    }
}
