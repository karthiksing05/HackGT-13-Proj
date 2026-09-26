import SwiftUI

/// Home's search bar, in the design system's search field look (white, 42 tall, radius 12, a
/// magnifying glass), with a clear button once there's text.
struct HomeSearchBar: View {
    @Binding var text: String
    var focused: FocusState<Bool>.Binding

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(Theme.text3)
                .accessibilityHidden(true)
            TextField("", text: $text, prompt: Text("Search sidequests, people, places").foregroundStyle(Theme.text3))
                .sqFont(15)
                .foregroundStyle(Theme.ink)
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
                .submitLabel(.search)
                .onSubmit { focused.wrappedValue = false }
                .focused(focused)
                .accessibilityLabel("Search")
            if !text.isEmpty {
                Button {
                    text = ""
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .font(.system(size: 15))
                        .foregroundStyle(Theme.mutedIcon)
                        .frame(width: 32, height: 42)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Clear search")
                .transition(.opacity)
            }
        }
        .padding(.leading, 12)
        .padding(.trailing, text.isEmpty ? 12 : 2)
        .frame(height: 42)
        .background(.white, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .contentShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
        .onTapGesture { focused.wrappedValue = true }
        .animation(Motion.quick, value: text.isEmpty)
    }
}

/// Home › search: `GET /search?q=` for the query typed last (the view debounces typing). The first
/// search shows a skeleton; a newer query keeps the last results on screen, dimmed, until its own
/// arrive.
@Observable
final class HomeSearchModel {
    /// nil until something has been searched.
    private(set) var results: Loadable<SearchResults>?
    /// The query `results` answer.
    private(set) var resultsQuery = ""
    /// A newer query is loading over the results on screen.
    private(set) var isRefreshing = false
    /// A chat with this person is being opened.
    private(set) var openingPersonId: String?
    private(set) var personError: String?
    @ObservationIgnored private var requestedQuery = ""

    func search(_ query: String, env: AppEnvironment) async {
        requestedQuery = query
        let showing = results?.value != nil
        withMotion {
            if showing { isRefreshing = true } else { results = .loading }
            personError = nil
        }
        let result = await Loadable.run { try await env.api.search(query: query) }
        // Typing again cancelled this one; the newer query shows instead.
        guard !Task.isCancelled else { return }
        withMotion(showing ? Motion.gentle : Motion.standard) {
            isRefreshing = false
            // A failed refresh of the same query keeps what's there.
            let keep: Bool = {
                if case .failed = result { return showing && query == resultsQuery }
                return false
            }()
            if !keep {
                results = result
                resultsQuery = query
            }
        }
    }

    /// Pull to refresh: the last query again, keeping the results on screen meanwhile.
    func reload(env: AppEnvironment) async {
        guard !requestedQuery.isEmpty else { return }
        await search(requestedQuery, env: env)
    }

    /// Search closed or cleared.
    func reset() {
        requestedQuery = ""
        results = nil
        resultsQuery = ""
        isRefreshing = false
        openingPersonId = nil
        personError = nil
    }

    /// A person in the results: open (or start) your chat with them.
    func openChat(with person: PersonRef, env: AppEnvironment, open: @escaping (ChatThread) -> Void) {
        guard openingPersonId == nil else { return }
        withMotion(Motion.quick) {
            openingPersonId = person.id
            personError = nil
        }
        Task {
            do {
                let thread = try await env.api.startDM(userId: person.id)
                withMotion(Motion.quick) { openingPersonId = nil }
                open(thread)
            } catch {
                withMotion(Motion.arrive) {
                    openingPersonId = nil
                    personError = HomeCheckoutSession.message(error, "Couldn't open a chat. Try again.")
                }
            }
        }
    }
}

/// Search results in sections (Sidequests, People, Places, Posts) of white rows. A sidequest opens
/// on the Sidequests segment, a person opens your chat, a place starts a plan that ends there, and a
/// post opens the Forum.
struct HomeSearchResultsView: View {
    let model: HomeSearchModel
    let retry: () -> Void
    let openSidequest: (Itinerary) -> Void
    let openPerson: (PersonRef) -> Void
    let openPlace: (Place) -> Void
    let openPost: (ForumPost) -> Void

    @Environment(AppEnvironment.self) private var env

    var body: some View {
        HomeLoadable(state: model.results ?? .loading, minHeight: 200, slowLines: ["Searching…"], retry: retry) {
            HomeSearchSkeleton()
        } content: { results in
            if results.isEmpty {
                EmptyStateView(message: "Nothing matches “\(model.resultsQuery)”. Try a place, a friend or a plan.")
                    .transition(.opacity)
            } else {
                sections(results)
            }
        }
        // A newer query keeps these on screen, dimmed, until its results swap in.
        .sqRefreshing(model.isRefreshing)
        .padding(.bottom, 24)
    }

    @ViewBuilder
    private func sections(_ results: SearchResults) -> some View {
        if !results.sidequests.isEmpty {
            section("SIDEQUESTS") {
                rows(results.sidequests) { sidequestRow($0) }
            }
        }
        if !results.people.isEmpty {
            section("PEOPLE") {
                rows(results.people) { personRow($0) }
            }
            if let error = model.personError {
                Text(error)
                    .sqFont(13)
                    .foregroundStyle(Theme.danger)
                    .padding(.horizontal, Metrics.side)
                    .padding(.top, 8)
                    .sqTransition(.rise)
            }
        }
        if !results.places.isEmpty {
            section("PLACES") {
                rows(results.places.enumerated().map { HomeSearchPlace(index: $0.offset, place: $0.element) }) { placeRow($0.place) }
            }
        }
        if !results.posts.isEmpty {
            section("POSTS") {
                rows(results.posts) { postRow($0) }
            }
        }
    }

    private func section<Rows: View>(_ title: String, @ViewBuilder rows: () -> Rows) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(title)
                .sqFont(13, .semibold, relativeTo: .footnote)
                .tracking(0.4)
                .foregroundStyle(Theme.text3)
                .homeLine(13)
                .padding(.horizontal, Metrics.side)
                .padding(.top, 16)
                .padding(.bottom, 8)
                .accessibilityAddTraits(.isHeader)
            VStack(spacing: 0) { rows() }
                .sqGroupedCard()
                .padding(.horizontal, Metrics.side)
        }
    }

    /// Rows separated by cream rules; results for a new query rise in.
    private func rows<Item: Identifiable, Row: View>(_ items: [Item], @ViewBuilder row: @escaping (Item) -> Row) -> some View {
        ForEach(Array(items.enumerated()), id: \.element.id) { index, item in
            VStack(spacing: 0) {
                if index > 0 { RowDivider(color: Theme.cream) }
                row(item)
            }
            .sqTransition(.rise)
        }
    }

    // MARK: Rows

    private func sidequestRow(_ itinerary: Itinerary) -> some View {
        let stops = itinerary.stopCount
        let when = "\(env.format.relativeDay(itinerary.date)) · \(env.format.compactRange(itinerary.start, itinerary.backBy))"
        let subtitle = "\(when) · \(stops) \(stops == 1 ? "stop" : "stops")"
        return resultRow(title: itinerary.title, subtitle: subtitle, hint: "Shows it on Home") {
            openSidequest(itinerary)
        } leading: {
            RouteMarker(figure: .diamond, size: 15)
                .frame(width: 36, height: 36)
                .background(Theme.sageTint, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                .accessibilityHidden(true)
        } trailing: {
            chevron
        }
    }

    private func personRow(_ result: UserSearchResult) -> some View {
        let person = result.person
        let opening = model.openingPersonId == person.id
        let subtitle = [person.username.map { "@\($0)" }, Self.relation(result.relation)].compactMap { $0 }.joined(separator: " · ")
        return resultRow(title: person.name, subtitle: subtitle.isEmpty ? nil : subtitle, hint: "Opens your chat") {
            openPerson(person)
        } leading: {
            Avatar(person: person, size: 36, fontSize: 13)
                .accessibilityHidden(true)
        } trailing: {
            ZStack(alignment: .trailing) {
                if opening {
                    LoadingDots(color: Theme.sageInk, dotSize: 5)
                        .transition(.opacity)
                } else {
                    Text("Message")
                        .sqFont(13, .semibold)
                        .foregroundStyle(Theme.sageInk)
                        .transition(.opacity)
                }
            }
            .animation(Motion.quick, value: opening)
        }
        .disabled(model.openingPersonId != nil)
        .accessibilityValue(opening ? "Opening chat" : "")
    }

    private func placeRow(_ place: Place) -> some View {
        resultRow(title: place.name, subtitle: "Plan a sidequest that ends here", hint: "Starts a new plan") {
            openPlace(place)
        } leading: {
            HomeIcon(glyph: .pin, size: 20)
                .foregroundStyle(Theme.text2)
                .frame(width: 36, height: 36)
                .background(Theme.cream, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
        } trailing: {
            Text("Plan")
                .sqFont(13, .semibold)
                .foregroundStyle(Theme.sageInk)
        }
    }

    private func postRow(_ post: ForumPost) -> some View {
        resultRow(title: post.title ?? post.text ?? "Forum post", subtitle: [post.author.firstName, post.meta].joined(separator: " · "),
                  hint: "Opens the Forum") {
            openPost(post)
        } leading: {
            Avatar(person: post.author, size: 36, fontSize: 13)
                .accessibilityHidden(true)
        } trailing: {
            chevron
        }
    }

    private var chevron: some View {
        Image(systemName: "chevron.right")
            .font(.system(size: 12, weight: .semibold))
            .foregroundStyle(Theme.mutedIcon)
            .accessibilityHidden(true)
    }

    private func resultRow<Leading: View, Trailing: View>(title: String, subtitle: String?, hint: String,
                                                          action: @escaping () -> Void,
                                                          @ViewBuilder leading: () -> Leading,
                                                          @ViewBuilder trailing: () -> Trailing) -> some View {
        Button(action: action) {
            HStack(spacing: 12) {
                leading()
                VStack(alignment: .leading, spacing: 0) {
                    Text(title)
                        .sqFont(15, .semibold)
                        .foregroundStyle(Theme.ink)
                        .homeLine(15)
                        .lineLimit(2)
                    if let subtitle {
                        Text(subtitle)
                            .sqFont(13)
                            .foregroundStyle(Theme.text3)
                            .homeLine(13)
                            .lineLimit(1)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                trailing()
            }
            .padding(.vertical, 12)
            .padding(.horizontal, 14)
            .frame(minHeight: 60)
            .contentShape(Rectangle())
        }
        .buttonStyle(.sqPressable)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([title, subtitle].compactMap { $0 }.joined(separator: ", "))
        .accessibilityHint(hint)
        .accessibilityAddTraits(.isButton)
    }

    private static func relation(_ relation: FriendRelation) -> String? {
        switch relation {
        case .friend: "Friend"
        case .outgoing: "Request sent"
        case .incoming: "Wants to be friends"
        case .none: nil
        }
    }
}

/// Places have no id of their own; their position in the results stands in.
private struct HomeSearchPlace: Identifiable {
    let index: Int
    let place: Place
    var id: String { "\(index)-\(place.name)" }
}
