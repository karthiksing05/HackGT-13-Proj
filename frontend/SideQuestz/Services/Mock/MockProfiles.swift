import Foundation

/// People › profile for the offline demo (`GET /users/{id}/profile`). What each person is like
/// (handle, school, status, likes, taste match with Jordan, their own friends, sidequests done) is
/// seeded here; how they relate to you, their status line and your friends in common come from
/// `MockAPIClient`'s live friends and requests, and their open plans are their Forum plan posts.
/// Likes and match reasons follow the server's rules (`pkg/api/social/profile.go`).
enum MockProfiles {
    struct Seed {
        var handle: String
        var school: String?
        var city: String? = "atlanta"
        var status: PresenceStatus = .open
        /// Most liked first.
        var likes: [TripType]
        /// Taste match with Jordan (People for you lists the same number).
        var match: Int?
        /// Their friends among the demo's people, besides Jordan.
        var friends: [PersonRef]
        var sidequestsDone: Int
    }

    private static let p = MockPeople.self

    static let seeds: [String: Seed] = [
        p.maya.id: Seed(handle: "maya", school: "Georgia Tech", likes: [.liveMusic, .food, .nightlife], match: 86,
                        friends: [p.dev, p.ava, p.sam, p.chris], sidequestsDone: 14),
        p.dev.id: Seed(handle: "dev", school: "Georgia Tech", likes: [.outdoors, .sports, .food], match: 79,
                       friends: [p.maya, p.ava], sidequestsDone: 9),
        p.ava.id: Seed(handle: "ava", school: "Emory", status: .busy, likes: [.outdoors, .earlyMornings, .liveMusic], match: 72,
                       friends: [p.maya, p.dev, p.sam, p.chris, p.gtOutdoors], sidequestsDone: 11),
        p.sam.id: Seed(handle: "sam", school: "Georgia State", status: .friendsOnly, likes: [.shopping, .food, .museums], match: 64,
                       friends: [p.ava, p.maya], sidequestsDone: 21),
        p.chris.id: Seed(handle: "chris", school: "Georgia Tech", likes: [.outdoors, .sports, .earlyMornings], match: 74,
                         friends: [p.ava, p.maya, p.gtOutdoors], sidequestsDone: 6),
        p.priya.id: Seed(handle: "priya", school: nil, city: "Chicago", likes: [.food, .outdoors, .museums], match: 88,
                         friends: [p.ava, p.chris], sidequestsDone: 3),
        p.gtOutdoors.id: Seed(handle: "gtoutdoors", school: "Georgia Tech", likes: [.outdoors, .earlyMornings, .longWalks], match: 63,
                              friends: [p.chris, p.ava, p.priya], sidequestsDone: 48),
    ]

    /// Jordan's own count of sidequests done.
    static let ownSidequestsDone = 12

    /// `person`'s profile for Jordan now. `relation`, `requestId` and `statusLine` are the mock's
    /// current ones, `friends` Jordan's friends, `ratings` Jordan's Setup ratings and `posts` the
    /// Forum as Jordan sees it.
    static func profile(of person: PersonRef, relation: ProfileRelation, requestId: String?, statusLine: String?,
                        friends: [PersonRef], ratings: [TripType: Int], posts: [ForumPost]) -> PublicProfile {
        let isFriend = relation == .friend
        let plans = posts.filter { $0.type == .plan && $0.author.id == person.id && (isFriend || !$0.friendsOnly) }
        guard let seed = seeds[person.id] else {
            return PublicProfile(person: person, relation: relation, requestId: requestId, openPlans: plans)
        }
        var ref = person
        ref.username = seed.handle
        let friendIds = Set(friends.map(\.id))
        let mutual = seed.friends.filter { friendIds.contains($0.id) }.sorted { $0.name.lowercased() < $1.name.lowercased() }
        return PublicProfile(
            person: ref, school: seed.school, city: seed.city, status: seed.status, statusLine: isFriend ? statusLine : nil,
            relation: relation, requestId: requestId,
            compatibility: MockData.suggestedPeople.first { $0.person.id == person.id }?.compatibility ?? seed.match,
            matchReasons: reasons(mine: liked(ratings), theirs: seed.likes), likes: seed.likes.map(\.label),
            mutualFriends: MutualFriends(count: mutual.count, people: Array(mutual.prefix(3))), openPlans: plans,
            sidequestsDone: seed.sidequestsDone)
    }

    /// Jordan's own profile: what others see, without a match or anything about how you relate.
    static func own(_ user: User, ratings: [TripType: Int]) -> PublicProfile {
        var me = MockPeople.me
        me.name = user.name
        me.initials = Initials.from(user.name)
        me.photoURL = user.photoURL
        me.username = user.username
        return PublicProfile(person: me, school: user.school, city: user.city, status: user.status, relation: .you,
                             likes: liked(ratings).map(\.label), sidequestsDone: ownSidequestsDone)
    }

    /// Trip types rated 4 or 5, most liked first (ties in Setup's order): with no ratings of past
    /// stops to go on, the server's rule comes down to this.
    static func liked(_ ratings: [TripType: Int]) -> [TripType] {
        TripType.allCases.enumerated()
            .filter { (ratings[$0.element] ?? 0) >= 4 }
            .sorted { (ratings[$0.element] ?? 0, -$0.offset) > (ratings[$1.element] ?? 0, -$1.offset) }
            .map(\.element)
    }

    /// Up to three reasons: what you both like, in the order you like it.
    static func reasons(mine: [TripType], theirs: [TripType]) -> [String] {
        mine.filter(theirs.contains).prefix(3).map { reason($0) }
    }

    /// The server's sentence for a trip type both people like.
    static func reason(_ type: TripType) -> String {
        switch type {
        case .outdoors: "You both love the outdoors"
        case .food: "You're both foodies"
        case .museums: "You both love museums and art"
        case .liveMusic: "You both love live music"
        case .nightlife: "You both love a night out"
        case .sports: "You're both into sports and games"
        case .shopping: "You both love markets and shopping"
        case .bigCrowds: "You both like a big crowd"
        case .earlyMornings: "You're both early risers"
        case .longWalks: "You both love long walks"
        }
    }
}
