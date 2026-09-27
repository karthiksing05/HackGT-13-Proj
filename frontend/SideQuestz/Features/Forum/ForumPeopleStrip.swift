import Observation
import SwiftUI

/// Forum › Friends › People for you (`GET /people/suggested`): taste matches, best first, then people
/// active lately. Loaded the first time the Friends tab shows and kept for the session; pull to
/// refresh and a changed profile load it again. Adding and accepting are optimistic, like
/// Account › Friends.
@Observable
final class ForumPeopleModel {
    /// nil until the Friends tab first shows.
    private(set) var suggestions: Loadable<[PersonSuggestion]>?
    /// Requests on their way (person ids): the card reads "Requested", faint.
    private(set) var sending: Set<String> = []
    /// Requests being accepted (person ids): the card reads "Friends", faint.
    private(set) var accepting: Set<String> = []
    private(set) var error: String?

    /// Loads the first time, or again with `force`; a failed reload keeps the cards on screen.
    func load(_ env: AppEnvironment, force: Bool = false) async {
        guard force || suggestions == nil || suggestions?.phase == .failed else { return }
        if suggestions?.value == nil { withMotion { suggestions = .loading } }
        let result = await Loadable.run { try await env.api.suggestedPeople() }
        withMotion(Motion.arrive) {
            if result.value != nil || suggestions?.value == nil { suggestions = result }
        }
    }

    func add(_ person: PersonRef, env: AppEnvironment) async {
        withMotion(Motion.quick) {
            _ = sending.insert(person.id)
            error = nil
        }
        do {
            let request = try await env.api.sendFriendRequest(userId: person.id)
            withMotion(Motion.arrive) {
                sending.remove(person.id)
                update(person.id) {
                    $0.relation = .outgoing
                    $0.requestId = request.id
                }
            }
        } catch {
            withMotion {
                sending.remove(person.id)
                self.error = authMessage(for: error, fallback: "Couldn't send the request. Try again.")
            }
        }
    }

    /// Accepts their request (Chris asked you): they're a friend now.
    func accept(_ suggestion: PersonSuggestion, env: AppEnvironment) async {
        guard let requestId = suggestion.requestId else { return }
        let personId = suggestion.person.id
        withMotion(Motion.quick) {
            _ = accepting.insert(personId)
            error = nil
        }
        do {
            try await env.api.acceptFriendRequest(id: requestId)
            withMotion(Motion.arrive) {
                accepting.remove(personId)
                update(personId) {
                    $0.relation = .friend
                    $0.requestId = nil
                }
            }
        } catch {
            withMotion {
                accepting.remove(personId)
                self.error = authMessage(for: error, fallback: "Couldn't accept the request. Try again.")
            }
        }
    }

    private func update(_ personId: String, _ change: (inout PersonSuggestion) -> Void) {
        guard case .loaded(var list)? = suggestions, let index = list.firstIndex(where: { $0.person.id == personId }) else { return }
        change(&list[index])
        suggestions = .loaded(list)
    }
}

/// The top of Forum › Friends: what the tab is for, then People for you as a row of cards (avatar,
/// name, match, Add). A card opens that person's profile. First load: shimmering cards (the S past
/// the slow-load threshold); a failure is one line with "Try again"; with no one to suggest, a line
/// pointing to Account › Friends.
struct ForumFriendsIntro: View {
    let model: ForumPeopleModel
    let openProfile: (PersonRef) -> Void

    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router

    /// Suggestions to show: never you, and friends drop out once a reload says so.
    private var people: [PersonSuggestion]? {
        model.suggestions?.value?.filter { $0.person.id != env.user?.id }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("Posts from your friends: when they're free or open a plan")
                .socialText(13)
                .foregroundStyle(Theme.text3)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.horizontal, Metrics.side)
            SocialSectionLabel(text: "PEOPLE FOR YOU")
                .padding(.horizontal, Metrics.side)
                .padding(.top, 12)
                .padding(.bottom, 8)
            content
            if let error = model.error {
                Text(error)
                    .socialText(12)
                    .foregroundStyle(Theme.dangerText)
                    .padding(.horizontal, Metrics.side)
                    .padding(.top, 6)
                    .sqTransition(.rise)
            }
        }
        .animation(Motion.standard, value: model.suggestions?.phase)
        .animation(Motion.standard, value: model.error)
    }

    @ViewBuilder private var content: some View {
        switch model.suggestions {
        case .failed(let message)?:
            HStack(spacing: 6) {
                Text(message).socialText(12).foregroundStyle(Theme.dangerText)
                Button("Try again") { Task { await model.load(env, force: true) } }
                    .buttonStyle(.sqLink(size: 12))
                    .frame(height: 20)
            }
            .padding(.horizontal, Metrics.side)
            .transition(.opacity)
        case .loaded?:
            if let people, !people.isEmpty {
                cards(people)
                    .transition(.opacity)
                if people.allSatisfy({ $0.compatibility == nil }) {
                    Text("Rate a few places to see who shares your taste.")
                        .socialText(12)
                        .foregroundStyle(Theme.text3)
                        .padding(.horizontal, Metrics.side)
                        .padding(.top, 6)
                }
            } else {
                HStack(spacing: 6) {
                    Text("No one to suggest yet.").socialText(13).foregroundStyle(Theme.text3)
                    Button("Find friends") {
                        router.accountSegment = .friends
                        router.select(.account)
                    }
                    .buttonStyle(.sqLink(size: 13))
                    .frame(height: 22)
                }
                .padding(.horizontal, Metrics.side)
                .transition(.opacity)
            }
        default:
            ForumPeopleSkeleton()
                .sqSlowLoading(lines: ["Finding people for you…"], logoSize: 28)
                .transition(.opacity)
        }
    }

    private func cards(_ people: [PersonSuggestion]) -> some View {
        ScrollView(.horizontal) {
            HStack(spacing: 10) {
                ForEach(people) { suggestion in
                    card(suggestion)
                        .transition(.opacity)
                }
            }
            .padding(.horizontal, Metrics.side)
        }
        .scrollIndicators(.hidden)
        .scrollBounceBehavior(.basedOnSize, axes: .horizontal)
    }

    /// Avatar, name and match open the profile; the button under them adds (or accepts) at once.
    private func card(_ suggestion: PersonSuggestion) -> some View {
        let person = suggestion.person
        return VStack(spacing: 10) {
            Button { openProfile(person) } label: {
                VStack(spacing: 6) {
                    Avatar(person: person, size: 52, fontSize: 18)
                        .accessibilityHidden(true)
                    Text(person.name)
                        .socialText(14, .semibold)
                        .foregroundStyle(Theme.ink)
                        .lineLimit(1)
                    Group {
                        if let match = suggestion.compatibility {
                            MatchPill(percent: match)
                        } else {
                            Text("Active lately")
                                .sqFont(11, .semibold)
                                .foregroundStyle(Theme.text3)
                        }
                    }
                    .frame(height: 20)
                }
                .frame(maxWidth: .infinity)
                .contentShape(Rectangle())
            }
            .buttonStyle(.sqPressable)
            .accessibilityElement(children: .combine)
            .accessibilityHint("Opens their profile")
            relationButton(suggestion)
        }
        .padding(12)
        .frame(width: 136)
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
    }

    /// "Add" → "Requested" (faint until the server has it); "Accept" for someone who asked you;
    /// "Friends" once you are. Requested and Friends open the profile (withdraw or remove there).
    @ViewBuilder private func relationButton(_ suggestion: PersonSuggestion) -> some View {
        let person = suggestion.person
        let sending = model.sending.contains(person.id)
        let accepting = model.accepting.contains(person.id)
        ZStack {
            if sending || suggestion.relation == .outgoing {
                pill("Requested", check: !sending, fill: Theme.successBg, text: Theme.success) { openProfile(person) }
                    .opacity(sending ? 0.6 : 1)
                    .disabled(sending)
                    .accessibilityValue(sending ? "Sending" : "")
                    .accessibilityHint(sending ? "" : "Opens their profile")
            } else if accepting || suggestion.relation == .friend {
                pill("Friends", check: !accepting, fill: Theme.sageTint, text: Theme.sageInk) { openProfile(person) }
                    .opacity(accepting ? 0.6 : 1)
                    .disabled(accepting)
                    .accessibilityHint(accepting ? "" : "Opens their profile")
            } else if suggestion.relation == .incoming {
                pill("Accept", check: false, fill: Theme.sage, text: Theme.ink) {
                    Task { await model.accept(suggestion, env: env) }
                }
                .accessibilityLabel("Accept \(person.name)")
            } else {
                pill("Add", check: false, fill: Theme.sage, text: Theme.ink) {
                    Task { await model.add(person, env: env) }
                }
                .accessibilityLabel("Add \(person.name)")
            }
        }
        .animation(Motion.quick, value: suggestion.relation)
        .animation(Motion.quick, value: sending || accepting)
    }

    private func pill(_ title: String, check: Bool, fill: Color, text: Color, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 5) {
                if check {
                    AnimatedCheck(lineWidth: 2.8, delay: 0.1)
                        .frame(width: 11, height: 11)
                        .transition(.opacity)
                }
                Text(title)
            }
        }
        .buttonStyle(.sq(fill: fill, foreground: text, height: 32, radius: 16, fontSize: 13))
        .transition(.opacity)
    }
}

/// People for you while it first loads: card-shaped placeholders running off the edge like the
/// cards do, shimmering. Reads "Loading".
private struct ForumPeopleSkeleton: View {
    private let nameWidths: [CGFloat] = [72, 88, 64]

    var body: some View {
        ScrollView(.horizontal) {
            HStack(spacing: 10) {
                ForEach(nameWidths.indices, id: \.self) { index in
                    VStack(spacing: 8) {
                        Circle().fill(Theme.skeleton).frame(width: 52, height: 52)
                        SkeletonBlock(width: nameWidths[index], height: 12)
                        SkeletonBlock(width: 62, height: 16, radius: 8)
                        SkeletonBlock(height: 32, radius: 16)
                    }
                    .padding(12)
                    .frame(width: 136)
                    .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
                }
            }
            .padding(.horizontal, Metrics.side)
        }
        .scrollDisabled(true)
        .scrollIndicators(.hidden)
        .sqShimmer()
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading")
    }
}
