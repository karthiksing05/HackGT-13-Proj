import Foundation

/// The demo server's `GET /activities/{id}` (Review › tap a stop): details for every activity a demo
/// plan, swap or must-see pick can put on Review (`MockActivities`: the demo's stops, the swap
/// catalog, Friday's events) and for the Forum dinner.
///
/// Simulated: the words, addresses, hours and ratings are written for the demo and are the same on
/// every day. The demo planner doesn't read opening hours, so only places that are open whenever a
/// demo plan could visit them list any. Ticket links use the sandbox merchant's address, like the
/// demo's saved stops (the merchant itself sells Saltlight Harbor's events).
enum MockActivityDetails {
    /// What the demo catalog says about one activity, by title.
    struct Info {
        var summary: String
        var description: String? = nil
        var address: String
        var hours: String? = nil
        var rating: Double? = nil
        var ratingCount: Int? = nil
        var website: String? = nil
        /// Sold on the demo merchant (`events.sidequestz.tech/<slug>/tickets`).
        var sellsTickets = false
    }

    /// The Forum's open dinner: a stop of the demo's options that isn't a place to pick.
    static let forumDinner = "Open group dinner (Forum)"

    /// The details for a stop's `activityId`, or nil for an id the demo catalog doesn't have.
    static func detail(id: String) -> ActivityDetail? {
        if let activity = MockActivities.activity(id: id) { return detail(for: activity) }
        return id == MockActivities.id(for: forumDinner) ? dinner : nil
    }

    static func detail(for activity: MockActivity) -> ActivityDetail {
        let info = infos[activity.title]
        let slug = activity.id.dropFirst("act-".count)
        return ActivityDetail(
            id: activity.id, title: activity.title, kind: activity.kind, category: activity.category,
            categoryLabel: activity.label, summary: info?.summary, description: info?.description,
            venueName: activity.kind == .event ? activity.venue.name : nil, address: info?.address, place: activity.venue,
            start: activity.start, end: activity.end, hoursLine: activity.kind == .place ? info?.hours : nil,
            priceCents: activity.priceCents, priceLabel: priceLabel(activity),
            rating: info?.rating, ratingCount: info?.ratingCount, websiteURL: info?.website.flatMap(URL.init(string:)),
            ticketURL: info?.sellsTickets == true ? URL(string: "https://events.sidequestz.tech/\(slug)/tickets") : nil,
            tags: activity.tags
        )
    }

    /// Like the server: a known price in dollars ("Free", "$5", "$18"), else the tier ("$", "$$").
    private static func priceLabel(_ activity: MockActivity) -> String {
        guard let cents = activity.priceCents else { return activity.priceLabel }
        return cents == 0 ? "Free" : Money.compact(cents)
    }

    private static let dinner = ActivityDetail(
        id: MockActivities.id(for: forumDinner), title: forumDinner, kind: .event, category: "community_event",
        categoryLabel: "Group dinner",
        summary: "Maya's open plan from the Forum: dinner at Krog Street Market with whoever joins. 3 people are going so far.",
        venueName: "Krog Street Market", address: "99 Krog St NE, Atlanta, GA 30307",
        place: Place(name: "Krog Street Market", coordinate: MockPlaces.stops[forumDinner]?.coordinate),
        priceLabel: "$", tags: ["food", "social", "group"]
    )

    // MARK: The demo catalog

    static let infos: [String: Info] = [
        // The demo's options.
        "Skyline Park rooftop": Info(
            summary: "Mini golf, carnival games and skyline views on the roof of Ponce City Market.",
            description: "Take the freight elevator to the top of Ponce City Market for nine holes of mini golf, skee-ball and "
                + "boardwalk-style games, with the Midtown and downtown skylines on every side. Games are paid by the round; "
                + "walking around the roof is free. It gets busy after 5 on Fridays, so an afternoon visit means shorter lines.",
            address: "675 Ponce De Leon Ave NE, Atlanta, GA 30308", hours: "Open 11 AM–10 PM",
            rating: 4.5, ratingCount: 2318, website: "https://poncecitymarket.com", sellsTickets: true),
        "Krog Street Tunnel murals": Info(
            summary: "A tunnel under the railroad covered wall to wall in murals and tags that change every week.",
            description: "The tunnel under the tracks between Inman Park and Cabbagetown is Atlanta's best-known graffiti wall: "
                + "every surface is painted and repainted, so no two visits look the same. It's a short walk from the Eastside Trail.",
            address: "Krog St NE, Atlanta, GA 30307", hours: "Open 24 hours",
            rating: 4.4, ratingCount: 1540, website: "https://atlantabeltline.org"),
        "Krog Street Market": Info(
            summary: "A food hall in a 1920s warehouse: dumplings, tacos, burgers and ice cream around a bar in the middle.",
            address: "99 Krog St NE, Atlanta, GA 30307", hours: "Open 7 AM–10 PM",
            rating: 4.6, ratingCount: 3902, website: "https://krogstreetmarket.com"),
        "Piedmont Park loop": Info(
            summary: "A loop around Atlanta's big central park: Lake Clara Meer, the Active Oval and skyline views over the Meadow.",
            address: "400 Park Dr NE, Atlanta, GA 30306", hours: "Open 6 AM–11 PM",
            rating: 4.8, ratingCount: 21450, website: "https://piedmontpark.org"),
        "Board game café": Info(
            summary: "Hundreds of board games to borrow by the table, with coffee, snacks and staff who'll teach you the rules.",
            address: "Tech Square, Atlanta, GA 30308", hours: "Open 11 AM–11 PM", rating: 4.7, ratingCount: 412),
        "Ponce City Market food hall": Info(
            summary: "The Central Food Hall in the old Sears building: dozens of counters, from ramen to fried chicken.",
            address: "675 Ponce De Leon Ave NE, Atlanta, GA 30308", hours: "Open 10 AM–10 PM",
            rating: 4.6, ratingCount: 18021, website: "https://poncecitymarket.com"),
        "Centennial Olympic Park": Info(
            summary: "Downtown's park from the 1996 Games, with the Fountain of Rings and lawns to spread out on.",
            address: "265 Park Ave W NW, Atlanta, GA 30313", hours: "Open 7 AM–11 PM",
            rating: 4.6, ratingCount: 26104, website: "https://gwcca.org"),
        "SkyView Ferris wheel": Info(
            summary: "A 200-foot Ferris wheel by Centennial Olympic Park; each ride is a few turns in a climate-controlled gondola.",
            address: "168 Luckie St NW, Atlanta, GA 30303", rating: 4.3, ratingCount: 8207,
            website: "https://skyviewatlanta.com", sellsTickets: true),
        "Bookstore browse": Info(
            summary: "An independent bookstore with new and used shelves, staff picks and a reading corner.",
            address: "Midtown, Atlanta, GA 30308", hours: "Open 10 AM–9 PM", rating: 4.8, ratingCount: 260),
        "Historic Fourth Ward Park": Info(
            summary: "A park built around a stormwater pond, with a skatepark, a playground and the BeltLine along one side.",
            address: "680 Dallas St NE, Atlanta, GA 30308", hours: "Open 6 AM–11 PM",
            rating: 4.7, ratingCount: 5811, website: "https://h4wpc.org"),
        "Taco stand on the BeltLine": Info(
            summary: "A walk-up window on the Eastside Trail: tacos, elote and aguas frescas to eat at the picnic tables.",
            address: "Eastside Trail, Atlanta, GA 30307", hours: "Open 11 AM–10 PM", rating: 4.5, ratingCount: 640),
        "Atlanta Botanical Garden": Info(
            summary: "Gardens next to Piedmont Park, with the Canopy Walk through the treetops and a conservatory of orchids.",
            address: "1345 Piedmont Ave NE, Atlanta, GA 30309", rating: 4.8, ratingCount: 14032,
            website: "https://atlantabg.org", sellsTickets: true),

        // The swap catalog.
        "Jackson Street Bridge": Info(
            summary: "The bridge over Freedom Parkway with the classic view of the downtown skyline, best around sunset.",
            address: "Jackson St NE, Atlanta, GA 30312", hours: "Open 24 hours", rating: 4.7, ratingCount: 3120),
        "Sun Dial rooftop": Info(
            summary: "A revolving restaurant and bar on the 72nd floor of the Westin Peachtree Plaza.",
            address: "210 Peachtree St NW, Atlanta, GA 30303", rating: 4.3, ratingCount: 2716),
        "Bellwood Quarry overlook": Info(
            summary: "The overlook above the flooded granite quarry in Westside Park, the city's largest park.",
            address: "1660 Johnson Rd NW, Atlanta, GA 30318", hours: "Open 6 AM–11 PM", rating: 4.7, ratingCount: 1904),
        "Freedom Park bridge": Info(
            summary: "A footbridge on the Freedom Park Trail with a peek at the skyline between the trees.",
            address: "Freedom Park, Atlanta, GA 30307", hours: "Open 6 AM–11 PM", rating: 4.5, ratingCount: 322),
        "Cabbagetown murals": Info(
            summary: "The long painted wall on Wylie Street and the side streets around it, by local and visiting artists.",
            address: "Wylie St SE, Atlanta, GA 30316", hours: "Open 24 hours", rating: 4.6, ratingCount: 871),
        "Atlanta Contemporary": Info(
            summary: "A free contemporary art center with changing exhibitions and open artist studios.",
            address: "535 Means St NW, Atlanta, GA 30318", rating: 4.6, ratingCount: 512),
        "High Museum of Art": Info(
            summary: "The Southeast's leading art museum, in the white Richard Meier building on Peachtree Street.",
            address: "1280 Peachtree St NE, Atlanta, GA 30309", rating: 4.7, ratingCount: 9411,
            website: "https://high.org", sellsTickets: true),
        "Castleberry Hill galleries": Info(
            summary: "Galleries and lofts in old warehouses south of downtown, all open for the monthly art stroll.",
            address: "Castleberry Hill, Atlanta, GA 30313", rating: 4.5, ratingCount: 409),
        "Sweet Auburn Curb Market": Info(
            summary: "Atlanta's public market since 1918: produce stalls and lunch counters under one roof.",
            address: "209 Edgewood Ave SE, Atlanta, GA 30303", rating: 4.5, ratingCount: 5220),
        "Politan Row": Info(
            summary: "A food hall of chef-run counters around a central bar.",
            address: "Colony Square, Atlanta, GA 30361", rating: 4.4, ratingCount: 1318),
        "Victory Sandwich Bar": Info(
            summary: "Cheap, good sandwiches and a patio with bocce in Inman Park.",
            address: "913 Bernina Ave NE, Atlanta, GA 30307", hours: "Open 11 AM–12 AM", rating: 4.5, ratingCount: 2104),
        "Fox Bros BBQ": Info(
            summary: "Texas-style brisket and ribs, with a big patio off DeKalb Avenue.",
            address: "1238 DeKalb Ave NE, Atlanta, GA 30307", hours: "Open 11 AM–10 PM", rating: 4.6, ratingCount: 6832),
        "Freedom Park trail": Info(
            summary: "A shaded paved trail through Freedom Park, between the Carter Center and Candler Park.",
            address: "Freedom Park, Atlanta, GA 30307", hours: "Open 6 AM–11 PM", rating: 4.6, ratingCount: 1107),
        "Grant Park loop": Info(
            summary: "The city's oldest park: a loop past big oaks, the zoo's edge and Victorian houses.",
            address: "840 Cherokee Ave SE, Atlanta, GA 30315", hours: "Open 6 AM–11 PM", rating: 4.6, ratingCount: 4307),
        "BeltLine Eastside Trail": Info(
            summary: "The busiest stretch of the BeltLine: murals, patios and people-watching from Piedmont Park to Inman Park.",
            address: "Eastside Trail, Atlanta, GA 30308", hours: "Open 5 AM–11 PM",
            rating: 4.8, ratingCount: 7604, website: "https://beltline.org"),
        "The Painted Duck": Info(
            summary: "Duckpin bowling, bocce and games in a Westside warehouse, with a bar and a kitchen.",
            address: "976 Brady Ave NW, Atlanta, GA 30318", rating: 4.3, ratingCount: 3011),
        "Joystick Gamebar": Info(
            summary: "A bar full of classic arcade cabinets and pinball on Edgewood Avenue.",
            address: "427 Edgewood Ave SE, Atlanta, GA 30312", rating: 4.5, ratingCount: 1206),
        "Puttshack": Info(
            summary: "Mini golf with balls that keep score for you, plus food and drinks.",
            address: "1115 Howell Mill Rd NW, Atlanta, GA 30318", hours: "Open 11 AM–12 AM", rating: 4.5, ratingCount: 2598),
        "A Cappella Books": Info(
            summary: "An independent bookstore in Little Five Points with new, used and signed books.",
            address: "208 Haralson Ave NE, Atlanta, GA 30307", rating: 4.8, ratingCount: 561),
        "Little Shop of Stories": Info(
            summary: "A children's and family bookstore on the Decatur square, with story times on weekends.",
            address: "133A E Court Square, Decatur, GA 30030", rating: 4.9, ratingCount: 702),

        // Friday's events (and Saturday's yoga).
        "Rooftop trivia": Info(
            summary: "Five rounds of general-knowledge trivia on the roof. Teams of up to six; prizes for the top three.",
            address: "675 Ponce De Leon Ave NE, Atlanta, GA 30308", sellsTickets: true),
        "Gallery talk at the High": Info(
            summary: "A curator walks through the new exhibition in 45 minutes; museum admission is included.",
            address: "1280 Peachtree St NE, Atlanta, GA 30309", website: "https://high.org", sellsTickets: true),
        "Food truck Friday": Info(
            summary: "A dozen food trucks line up by the park's 12th Street gate every Friday afternoon.",
            address: "400 Park Dr NE, Atlanta, GA 30306"),
        "Pickup soccer": Info(
            summary: "Casual pickup games on the lawn. All levels; bring water and shoes you can run in.",
            address: "680 Dallas St NE, Atlanta, GA 30308"),
        "Sunset jazz on the Eastside Trail": Info(
            summary: "A jazz trio plays by the trail as the sun goes down. Bring a blanket.",
            address: "Eastside Trail, Atlanta, GA 30308"),
        "Late show at the Plaza Theatre": Info(
            summary: "A late screening at the city's oldest cinema, a 1939 movie palace on Ponce.",
            address: "1049 Ponce De Leon Ave NE, Atlanta, GA 30306", website: "https://plazaatlanta.com", sellsTickets: true),
        "Morning farmers market": Info(
            summary: "Local produce, bread and coffee in Grant Park on Friday mornings.",
            address: "840 Cherokee Ave SE, Atlanta, GA 30315"),
        "Sunrise yoga in Piedmont Park": Info(
            summary: "An hour of all-levels yoga on the Meadow as the sun comes up. Bring a mat.",
            address: "400 Park Dr NE, Atlanta, GA 30306"),
    ]
}
