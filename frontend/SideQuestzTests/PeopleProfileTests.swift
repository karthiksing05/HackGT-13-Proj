import Foundation
import Testing
@testable import SideQuestz

/// People › profile: the server's profile decodes (leniently), each relation gets its buttons, the
/// lines read right, and the demo backend's profiles follow its friends, requests and posts.
@MainActor
struct PeopleProfileTests {
    private let decoder = APICoding.decoder(timeZone: AppClock.demo.timeZone)

    private func decode(_ json: String) throws -> PublicProfile {
        try decoder.decode(PublicProfile.self, from: Data(json.utf8))
    }

    // MARK: Decoding

    @Test func decodesTheServersProfile() throws {
        let profile = try decode("""
        {"person":{"id":"665f1c","name":"Maya Rivera","initials":"MR","color_hex":"#1D4ED8","username":"maya"},
         "school":"Georgia Tech","city":"atlanta","status":"friends_only","status_line":"Free until 8 PM",
         "relation":"friend","compatibility":86,"match_reasons":["You both love live music","You're both foodies"],
         "likes":["Live music","Food & drinks","Nightlife"],
         "mutual_friends":{"count":4,"people":[{"id":"u1","name":"Ana B."},{"id":"u2","name":"Ben C."},{"id":"u3","name":"Cal D."}]},
         "open_plans":[{"id":"it-1","type":"plan","author":{"id":"665f1c","name":"Maya Rivera"},"title":"Sunset walk",
                        "meta":"Hosting · 0.6 mi away","join_status":"requested","compatibility":80}],
         "sidequests_done":14}
        """)
        #expect(profile.id == "665f1c" && profile.person.username == "maya" && profile.school == "Georgia Tech")
        #expect(profile.city == "atlanta" && profile.status == .friendsOnly && profile.statusLine == "Free until 8 PM")
        #expect(profile.relation == .friend && profile.requestId == nil && profile.compatibility == 86)
        #expect(profile.matchReasons == ["You both love live music", "You're both foodies"])
        #expect(profile.likes == ["Live music", "Food & drinks", "Nightlife"])
        #expect(profile.mutualFriends.count == 4 && profile.mutualFriends.people.map(\.initials) == ["AB", "BC", "CD"])
        let plan = try #require(profile.openPlans.first)
        #expect(plan.type == .plan && plan.title == "Sunset walk" && plan.joinStatus == .requested && plan.compatibility == 80)
        #expect(profile.sidequestsDone == 14)
    }

    @Test func missingFieldsHaveDefaults() throws {
        let bare = try decode(#"{"person":{"id":"u9","name":"Sam T."}}"#)
        #expect(bare.relation == .none && bare.status == .open && bare.statusLine == nil && bare.compatibility == nil)
        #expect(bare.matchReasons.isEmpty && bare.likes.isEmpty && bare.openPlans.isEmpty && bare.sidequestsDone == 0)
        #expect(bare.mutualFriends == MutualFriends())
        // Your own profile, a relation this app doesn't know, a match out of range, a short count.
        #expect(try decode(#"{"person":{"id":"u1","name":"Jordan Lee"},"relation":"self"}"#).relation == .you)
        let odd = try decode(#"{"person":{"id":"u2","name":"Ava K."},"relation":"blocked","compatibility":140,"sidequests_done":-3,"mutual_friends":{"count":0,"people":[{"id":"u3","name":"Dev P."}]}}"#)
        #expect(odd.relation == .none && odd.compatibility == 100 && odd.sidequestsDone == 0 && odd.mutualFriends.count == 1)
        #expect(try decode(#"{"person":{"id":"u4","name":"Chris N."},"relation":"incoming","request_id":"fr-9","compatibility":-5}"#).requestId == "fr-9")
        // Sent back with the server's words ("self", snake_case keys).
        let sent = String(decoding: try APICoding.encoder().encode(PublicProfile(person: MockPeople.me, relation: .you, sidequestsDone: 2)), as: UTF8.self)
        #expect(sent.contains(#""relation":"self""#) && sent.contains(#""sidequests_done":2"#) && sent.contains(#""mutual_friends":{"#))
    }

    // MARK: Buttons

    @Test func eachRelationGetsItsButtons() {
        #expect(PersonProfileAction.actions(for: .you).isEmpty)
        #expect(PersonProfileAction.actions(for: .none) == [.add, .message])
        #expect(PersonProfileAction.actions(for: .outgoing) == [.requested, .message])
        #expect(PersonProfileAction.actions(for: .incoming) == [.accept, .decline, .message])
        #expect(PersonProfileAction.actions(for: .friend) == [.friends, .message])
        #expect([PersonProfileAction.add, .requested, .accept, .decline, .friends, .message].map(\.title)
                == ["Add friend", "Requested", "Accept", "Decline", "Friends", "Message"])
        // A Friends row's relation carries over.
        #expect([FriendRelation.none, .friend, .outgoing, .incoming].map { ProfileRelation($0) } == [.none, .friend, .outgoing, .incoming])
    }

    // MARK: Lines

    @Test func handleAndPresenceLines() {
        var profile = PublicProfile(person: PersonRef(id: "u1", name: "Maya R.", initials: "MR", colorHex: "#1D4ED8", username: "maya"))
        profile.school = "Georgia Tech"
        profile.city = "atlanta"
        #expect(profile.handleLine == "@maya · Georgia Tech")
        profile.school = "  "
        #expect(profile.handleLine == "@maya · Atlanta")
        profile.city = "Chicago, IL"
        #expect(profile.handleLine == "@maya · Chicago, IL")
        profile.person.username = nil
        #expect(profile.handleLine == "Chicago, IL")
        profile.city = nil
        #expect(profile.handleLine == nil)

        // Someone else: what their status lets you know, or a friend's own line.
        #expect(profile.presenceLine == "Open to plans")
        profile.status = .friendsOnly
        #expect(profile.presenceLine == "Plans with friends only")
        profile.status = .busy
        #expect(profile.presenceLine == "Busy right now")
        profile.relation = .friend
        profile.statusLine = "On a sidequest · Thrift crawl"
        #expect(profile.presenceLine == "On a sidequest · Thrift crawl")
        // Yours reads as Account's status does.
        profile.relation = .you
        #expect(profile.presenceLine == "Busy")

        #expect([0, 1, 14].map { count -> String in
            profile.sidequestsDone = count
            return profile.sidequestsLine
        } == ["No sidequests yet", "1 sidequest done", "14 sidequests done"])
    }

    @Test func friendsInCommonRead() {
        func line(_ count: Int, _ names: [String]) -> (title: String, line: String?) {
            let people = names.enumerated().map { PersonRef(id: "u\($0.offset)", name: $0.element, initials: "", colorHex: "#000000") }
            let profile = PublicProfile(person: MockPeople.maya, mutualFriends: MutualFriends(count: count, people: people))
            return (profile.mutualTitle, profile.mutualLine)
        }
        #expect(line(0, []).line == nil)
        #expect(line(1, ["Ava K."]) == ("1 FRIEND IN COMMON", "Ava"))
        #expect(line(2, ["Ava K.", "Dev P."]).line == "Ava and Dev")
        #expect(line(3, ["Ava K.", "Dev P.", "Sam T."]) == ("3 FRIENDS IN COMMON", "Ava, Dev and Sam"))
        #expect(line(5, ["Ava K.", "Dev P.", "Sam T."]).line == "Ava, Dev, Sam and 2 more")
        #expect(line(2, []).line == "2 friends in common")
    }

    // MARK: The demo backend

    @Test func demoProfilesFollowFriendsRequestsAndPosts() async throws {
        let api = MockAPIClient(latencyScale: 0)

        // A friend: their status line, friends in common by name, their plan, the People for you
        // numbers' sibling (a taste match) and why.
        let maya = try await api.profile(userId: MockPeople.maya.id)
        #expect(maya.relation == .friend && maya.statusLine == "Free until 8 PM" && maya.compatibility == 86)
        #expect(maya.person.username == "maya" && maya.handleLine == "@maya · Georgia Tech")
        #expect(maya.mutualFriends.count == 3 && maya.mutualFriends.people.map(\.firstName) == ["Ava", "Dev", "Sam"])
        #expect(maya.openPlans.map(\.id) == ["p1"] && maya.matchReasons == ["You're both foodies"])
        #expect(maya.likes == ["Live music", "Food & drinks", "Nightlife"])

        // A stranger has People for you's match, and no status line.
        let priya = try await api.profile(userId: MockPeople.priya.id)
        #expect(priya.relation == .none && priya.statusLine == nil && priya.openPlans.isEmpty)
        #expect(priya.compatibility == MockData.suggestedPeople.first { $0.person.id == MockPeople.priya.id }?.compatibility)
        #expect(priya.matchReasons == ["You both love the outdoors", "You're both foodies"])
        #expect(priya.mutualFriends.people.map(\.firstName) == ["Ava"])

        // Add → Requested (with the request to withdraw), everywhere the demo tracks it; withdraw.
        let request = try await api.sendFriendRequest(userId: MockPeople.priya.id)
        let asked = try await api.profile(userId: MockPeople.priya.id)
        #expect(asked.relation == .outgoing && asked.requestId == request.id)
        #expect(try await api.suggestedPeople().first { $0.person.id == MockPeople.priya.id }?.relation == .outgoing)
        #expect(try await api.searchUsers(query: "priya").first?.relation == .outgoing)
        try await api.cancelFriendRequest(id: request.id)
        #expect(try await api.profile(userId: MockPeople.priya.id).relation == .none)

        // Chris asked you: accept, and he's a friend who was just added.
        let chris = try await api.profile(userId: MockPeople.chris.id)
        #expect(chris.relation == .incoming && chris.requestId == "fr-chris")
        try await api.acceptFriendRequest(id: "fr-chris")
        let friend = try await api.profile(userId: MockPeople.chris.id)
        #expect(friend.relation == .friend && friend.requestId == nil && friend.statusLine == "Just added")

        // Removing Sam takes his friends-only plan off his profile and out of the Forum.
        #expect(try await api.profile(userId: MockPeople.sam.id).openPlans.map(\.id) == ["p5"])
        try await api.removeFriend(id: MockPeople.sam.id)
        let sam = try await api.profile(userId: MockPeople.sam.id)
        #expect(sam.relation == .none && sam.statusLine == nil && sam.openPlans.isEmpty)
        var friendsOnly = ForumQuery()
        friendsOnly.scope = .friends
        let friendsFeed = try await api.forumPosts(friendsOnly)
        #expect(friendsFeed.map(\.id).sorted() == ["p1", "p2"] && friendsFeed.allSatisfy(\.isFriend))
        #expect(try await api.forumPosts(ForumQuery()).first { $0.id == "p3" }?.isFriend == false)

        // You, and someone who isn't there.
        let me = try await api.profile(userId: MockPeople.me.id)
        #expect(me.relation == .you && me.compatibility == nil && me.matchReasons.isEmpty && me.mutualFriends.count == 0)
        #expect(me.likes == ["Outdoors & parks", "Food & drinks"] && me.handleLine == "@jordanlee · Georgia Tech")
        await #expect(throws: APIError.notFound) { try await api.profile(userId: "u-nobody") }
    }

    /// The demo's likes and reasons follow the server's rules and sentences (profile.go).
    @Test func demoLikesAndReasons() {
        #expect(MockProfiles.liked([.food: 4, .outdoors: 5, .museums: 3, .liveMusic: 4]) == [.outdoors, .food, .liveMusic])
        #expect(MockProfiles.liked([.nightlife: 2]).isEmpty)
        #expect(MockProfiles.reasons(mine: [.outdoors, .food, .liveMusic, .nightlife], theirs: [.nightlife, .liveMusic, .food, .outdoors])
                == ["You both love the outdoors", "You're both foodies", "You both love live music"])
        #expect(MockProfiles.reasons(mine: [.outdoors], theirs: [.shopping]).isEmpty)
        #expect(TripType.allCases.map { MockProfiles.reason($0) }.allSatisfy { $0.hasPrefix("You") })
        #expect(MockProfiles.reason(.earlyMornings) == "You're both early risers")
    }
}
