import SwiftUI

/// The Event sheet (GUI_PLAN.md §7.6): tap any block. Kind chip, title, time and place,
/// description, who's in (groups), getting there (not for busy blocks), Website / Get tickets,
/// notes, and "Rate it after". Presented by `HomeView` with `sqSheet(isPresented:style: .fixed(660))`.
///
/// Loading: opened from a timeline it shows the block's copy at once; otherwise a skeleton of the
/// sheet until the details arrive. "Getting there" loads on its own (in parallel when the kind is
/// known) with its own skeleton. Notes autosave with a small "Saving…" → "Saved" status; the rating
/// shows right away and confirms the same way (rolled back if it fails).
struct HomeEventSheet: View {
    let route: HomeEventRoute
    let close: () -> Void
    /// Something on Home may have changed (a booking).
    let didChange: () -> Void

    @Environment(AppEnvironment.self) private var env
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var detail: Loadable<ItineraryItem>
    @State private var transit: Loadable<[TransitOption]> = .loading
    @State private var transitRequested = false
    @State private var mode: TravelMode?
    @State private var notes: String
    @State private var savedNotes: String
    @State private var notesError: String?
    @State private var notesStatus: HomeSaveState = .idle
    @State private var rating: Int
    @State private var isRating = false
    @State private var ratingError: String?
    @State private var ratingStatus: HomeSaveState = .idle
    @State private var showsBrowser = false
    @State private var showsCheckout = false
    @State private var openedCheckout = false
    @FocusState private var notesFocused: Bool
    /// The selected travel mode's tint and ring slide between the cards.
    @Namespace private var modeSelection

    init(route: HomeEventRoute, close: @escaping () -> Void, didChange: @escaping () -> Void) {
        self.route = route
        self.close = close
        self.didChange = didChange
        _detail = State(initialValue: route.seed.map { .loaded($0) } ?? .loading)
        _notes = State(initialValue: route.seed?.notes ?? "")
        _savedNotes = State(initialValue: route.seed?.notes ?? "")
        _rating = State(initialValue: route.seed?.rating?.stars ?? 0)
    }

    var body: some View {
        SheetScaffold {
            header(kind: detail.value?.kind)
            ZStack(alignment: .topLeading) {
                switch detail {
                case .loading:
                    HomeEventSkeleton()
                        .transition(.opacity)
                case .failed(let message):
                    ErrorStateView(message: message, minHeight: 420) { Task { await load() } }
                        .transition(.opacity)
                case .loaded(let item):
                    VStack(alignment: .leading, spacing: 14) { content(item) }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .transition(.opacity)
                }
            }
            .animation(reduceMotion ? Motion.reduced : Motion.standard, value: detail.phase)
        }
        .task { await load() }
        .task { await loadTransitEarly() }
        .task(id: notes) { await saveNotesAfterPause() }
        .task(id: notesStatus) {
            // "Saved" shows for a moment, then the header is back to just "NOTES".
            guard notesStatus == .saved else { return }
            try? await Task.sleep(for: .seconds(2))
            guard !Task.isCancelled else { return }
            withMotion { notesStatus = .idle }
        }
        .task(id: ratingStatus) {
            guard ratingStatus == .saved else { return }
            try? await Task.sleep(for: .seconds(2))
            guard !Task.isCancelled else { return }
            withMotion { ratingStatus = .idle }
        }
        .onDisappear(perform: saveNotesOnClose)
        .sheet(isPresented: $showsBrowser) {
            if let url = detail.value?.websiteURL {
                SafariView(url: url).ignoresSafeArea()
            }
        }
        .sqSheet(isPresented: $showsCheckout, style: SQSheetStyle(dim: 0.45)) {
            if let item = detail.value {
                HomeCheckoutSheet(item: item, close: { showsCheckout = false }, booked: didChange)
            }
        }
    }

    // MARK: Content

    @ViewBuilder
    private func content(_ item: ItineraryItem) -> some View {
        Text(item.title)
            .sqFont(26, .bold, relativeTo: .title)
            .foregroundStyle(Theme.ink)
            .homeLine(26, 1.15)
            .fixedSize(horizontal: false, vertical: true)
            .accessibilityAddTraits(.isHeader)
        VStack(alignment: .leading, spacing: 6) {
            iconRow(.clock, env.format.range(item.start, item.end))
            if let place = item.place?.name, !place.isEmpty {
                iconRow(.pin, place)
            }
        }
        if let description = item.description, !description.isEmpty {
            Text(description)
                .sqFont(15)
                .foregroundStyle(Theme.ink)
                .homeLine(15, 1.45)
                .fixedSize(horizontal: false, vertical: true)
        }
        if item.kind == .group && item.hasPeople {
            whosIn(item)
        }
        if item.kind != .busy {
            gettingThere
        }
        actions(item)
        notesBox(scope: item.kind == .group ? "Shared with the group" : "Only you")
        if item.kind == .sidequest || item.kind == .group {
            ratingRow
        }
    }

    /// Kind chip + ×. It stays put while the details load (a placeholder chip shimmers) and the
    /// real chip pops in.
    private func header(kind: BlockKind?) -> some View {
        HStack(spacing: 8) {
            if let kind {
                // The kind chip in the prototype's line box (13pt × 1.35 + 4pt padding).
                TagLabel(text: kind.palette.label, fill: kind.palette.background, foreground: kind.palette.text,
                         fontSize: 13, horizontalPadding: 10, verticalPadding: 5, radius: 8)
                    .sqTransition(.pop)
            } else if detail.isLoading {
                RoundedRectangle(cornerRadius: 8, style: .continuous)
                    .fill(Theme.skeleton)
                    .frame(width: 84, height: 26)
                    .sqShimmer()
                    .accessibilityHidden(true)
                    .transition(.opacity)
            }
            Spacer(minLength: 0)
            CloseCircleButton(action: close)
                .padding(-6)
        }
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: kind)
    }

    private func iconRow(_ glyph: HomeIcon.Glyph, _ text: String) -> some View {
        HStack(spacing: 8) {
            HomeIcon(glyph: glyph, size: 18)
            Text(text)
                .sqFont(15)
                .homeLine(15)
        }
        .foregroundStyle(Theme.text2)
        .accessibilityElement(children: .combine)
    }

    // MARK: Who's in

    private func whosIn(_ item: ItineraryItem) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("WHO'S IN")
                .sqFont(12, .bold, relativeTo: .caption)
                .tracking(0.4)
                .foregroundStyle(Theme.text3)
                .homeLine(12)
                .accessibilityAddTraits(.isHeader)
            ScrollView(.horizontal) {
                HStack(alignment: .top, spacing: 12) {
                    ForEach(item.people) { person($0, isGoing: true) }
                    ForEach(item.interested) { person($0, isGoing: false) }
                }
            }
            .scrollIndicators(.hidden)
            .scrollBounceBehavior(.basedOnSize, axes: .horizontal)
        }
        .padding(.vertical, 12)
        .padding(.horizontal, 14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.cream, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
    }

    private func person(_ person: PersonRef, isGoing: Bool) -> some View {
        let name = person.id == env.user?.id ? "You" : person.firstName
        let status = isGoing ? "Going" : "Interested"
        return VStack(spacing: 4) {
            Avatar(person: person, size: 40, fontSize: 13, ring: isGoing ? Theme.sage : Theme.lineStrong, ringWidth: 2)
            Text(name)
                .sqFont(11, relativeTo: .caption2)
                .foregroundStyle(Theme.text2)
                .homeLine(11)
                .lineLimit(1)
                .fixedSize()
            Text(status)
                .sqFont(10, relativeTo: .caption2)
                .foregroundStyle(Theme.text3)
                .homeLine(10)
                .lineLimit(1)
                .fixedSize()
        }
        .frame(width: 52)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(name), \(status)")
    }

    // MARK: Getting there

    @ViewBuilder
    private var gettingThere: some View {
        Text("GETTING THERE")
            .sqFont(13, .semibold, relativeTo: .footnote)
            .tracking(0.4)
            .foregroundStyle(Theme.text3)
            .homeLine(13)
            .accessibilityAddTraits(.isHeader)
        ZStack(alignment: .topLeading) {
            switch transit {
            case .loading:
                HomeTransitSkeleton()
                    .transition(.opacity)
            case .failed(let message):
                ErrorStateView(message: message, minHeight: 84) { Task { await loadTransit() } }
                    .transition(.opacity)
            case .loaded(let options):
                Group {
                    if options.isEmpty {
                        Text("No routes to show yet.")
                            .sqFont(15)
                            .foregroundStyle(Theme.text3)
                    } else {
                        HStack(spacing: 8) {
                            ForEach(Array(options.enumerated()), id: \.element.id) { index, option in
                                transitCard(option, isSelected: option.mode == (mode ?? options.first?.mode))
                                    .sqAppear(index)
                            }
                        }
                        .sensoryFeedback(.selection, trigger: mode)
                    }
                }
                .transition(.opacity)
            }
        }
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: transit.phase)
    }

    /// A travel mode card. Picking one springs the sage tint and ring over from the last pick.
    private func transitCard(_ option: TransitOption, isSelected: Bool) -> some View {
        let cost = Self.cost(option)
        let shape = RoundedRectangle(cornerRadius: 14, style: .continuous)
        return Button {
            withMotion(Motion.arrive) { mode = option.mode }
        } label: {
            VStack(spacing: 3) {
                HomeIcon(glyph: Self.glyph(option.mode), size: 22)
                    .sqBounce(when: isSelected, scale: 1.2)
                Text(option.mode.label)
                    .sqFont(14, .semibold)
                    .homeLine(14)
                Text("\(option.minutes) min · \(cost)")
                    .sqFont(12, relativeTo: .caption)
                    .foregroundStyle(Theme.text2)
                    .homeLine(12)
            }
            .lineLimit(1)
            .minimumScaleFactor(0.8)
            .foregroundStyle(Theme.ink)
            .frame(maxWidth: .infinity)
            .padding(.vertical, 10)
            .padding(.horizontal, 6)
            .background {
                ZStack {
                    shape.fill(.white)
                    if isSelected {
                        shape.fill(Theme.sageTint)
                            .matchedGeometryEffect(id: "mode.tint", in: modeSelection)
                    }
                }
            }
            .overlay {
                // box-shadow ring outside the card: 2pt sage when selected, 1pt `line` otherwise.
                ZStack {
                    RoundedRectangle(cornerRadius: 15, style: .continuous)
                        .strokeBorder(Theme.line, lineWidth: 1)
                        .padding(-1)
                        .opacity(isSelected ? 0 : 1)
                    if isSelected {
                        RoundedRectangle(cornerRadius: 16, style: .continuous)
                            .strokeBorder(Theme.sage, lineWidth: 2)
                            .padding(-2)
                            .matchedGeometryEffect(id: "mode.ring", in: modeSelection)
                    }
                }
            }
            .contentShape(shape)
        }
        .buttonStyle(.sqPressable)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(option.mode.label), \(option.minutes) minutes, \(cost)")
        .accessibilityAddTraits(isSelected ? [.isButton, .isSelected] : .isButton)
    }

    private static func glyph(_ mode: TravelMode) -> HomeIcon.Glyph {
        switch mode {
        case .walk: .walk
        case .marta: .train
        case .rideshare, .drive, .uber: .car
        }
    }

    /// 0 → "Free", unknown → "[fare]" (never invent a price).
    private static func cost(_ option: TransitOption) -> String {
        guard let cents = option.costCents else { return "[fare]" }
        return cents == 0 ? "Free" : Money.format(cents)
    }

    // MARK: Website + tickets

    @ViewBuilder
    private func actions(_ item: ItineraryItem) -> some View {
        if item.websiteURL != nil || item.bookable {
            HStack(spacing: 10) {
                if item.websiteURL != nil {
                    Button {
                        showsBrowser = true
                    } label: {
                        HStack(spacing: 6) {
                            HomeIcon(glyph: .globe, size: 18)
                            Text("Website")
                        }
                    }
                    .buttonStyle(.sq(fill: Theme.cream, foreground: Theme.ink, height: 48, fontSize: 15))
                    .accessibilityHint("Opens the event's website in the app")
                }
                if item.bookable {
                    Button("Get tickets") { showsCheckout = true }
                        .buttonStyle(.sq(fill: Theme.sage, foreground: Theme.ink, height: 48, fontSize: 15))
                }
            }
        } else {
            // The prototype keeps an empty buttons row here, which adds one more gap.
            Color.clear.frame(height: 0).accessibilityHidden(true)
        }
    }

    // MARK: Notes

    private func notesBox(scope: String) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline) {
                Text("NOTES")
                    .sqFont(12, .bold, relativeTo: .caption)
                    .tracking(0.4)
                    .foregroundStyle(Theme.text3)
                if notesStatus != .idle {
                    HomeSaveStatusLabel(state: notesStatus)
                        .transition(.opacity)
                }
                Spacer(minLength: 8)
                Text(scope)
                    .sqFont(11, relativeTo: .caption2)
                    .foregroundStyle(Theme.text3)
            }
            .homeLine(12)
            TextField("", text: $notes, prompt: Text("What to bring, where to meet, reminders…").foregroundStyle(Theme.text3), axis: .vertical)
                .lineLimit(3...10)
                .sqFont(15)
                .foregroundStyle(Theme.ink)
                .homeLine(15)
                .focused($notesFocused)
                .frame(minHeight: 3 * 15 * 1.35, alignment: .topLeading)
                .accessibilityLabel("Notes, \(scope)")
            if let notesError {
                HStack(spacing: 8) {
                    Text(notesError).sqFont(13).foregroundStyle(Theme.danger)
                    Button("Try again") { Task { await saveNotes() } }
                        .buttonStyle(.sqLink(size: 13))
                }
                .sqTransition(.rise)
            }
        }
        // 1pt border + the prototype's 12 / 14 padding.
        .padding(.vertical, 13)
        .padding(.horizontal, 15)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.field, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay { RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Theme.line, lineWidth: 1) }
        .contentShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        .onTapGesture { notesFocused = true }
    }

    // MARK: Rate it after

    private var ratingRow: some View {
        VStack(alignment: .leading, spacing: 0) {
            RowDivider()
            HStack(spacing: 8) {
                VStack(alignment: .leading, spacing: 0) {
                    Text("Rate it after")
                        .sqFont(15, .semibold)
                        .foregroundStyle(Theme.ink)
                        .homeLine(15)
                    // While the rating saves, "Saving…" → "Saved" stands in for the caption.
                    ZStack(alignment: .leading) {
                        Text("Tunes your future picks")
                            .sqFont(12, relativeTo: .caption)
                            .foregroundStyle(Theme.text3)
                            .homeLine(12)
                            .opacity(ratingStatus == .idle ? 1 : 0)
                        if ratingStatus != .idle {
                            HomeSaveStatusLabel(state: ratingStatus, size: 12)
                                .transition(.opacity)
                        }
                    }
                    .animation(reduceMotion ? Motion.reduced : Motion.standard, value: ratingStatus)
                }
                Spacer(minLength: 0)
                StarPicker(rating: Binding(get: { rating }, set: { rate($0) }), size: 26,
                           target: CGSize(width: 36, height: 44), spacing: 2, emptyStroke: Theme.mutedIcon, lineWidth: 1.6)
                    .disabled(isRating)
            }
            .padding(.top, 6)
            if let ratingError {
                Text(ratingError)
                    .sqFont(13)
                    .foregroundStyle(Theme.danger)
                    .padding(.top, 4)
                    .sqTransition(.rise)
            }
        }
    }

    // MARK: Loading + saving

    private func load() async {
        if detail.value == nil { withMotion { detail = .loading } }
        let result = await Loadable.run { try await env.api.eventDetail(id: route.id) }
        switch result {
        case .loaded(let item):
            withMotion {
                detail = .loaded(item)
                if notes == savedNotes {
                    notes = item.notes ?? ""
                    savedNotes = notes
                }
                if !isRating { rating = item.rating?.stars ?? 0 }
            }
        case .failed(let message):
            // Keep showing the timeline's copy of the block if we have one.
            if detail.value == nil { withMotion { detail = .failed(message) } }
        case .loading:
            break
        }
        guard let item = detail.value else { return }
        if item.kind != .busy, !transitRequested { await loadTransit() }
        if route.opensCheckout, item.bookable, !openedCheckout {
            openedCheckout = true
            // Let the sheet finish sliding up before Checkout goes on top.
            try? await Task.sleep(for: .milliseconds(450))
            showsCheckout = true
        }
    }

    /// Opened from a timeline, the kind is already known: load the routes alongside the details.
    private func loadTransitEarly() async {
        guard let seed = route.seed, seed.kind != .busy else { return }
        await loadTransit()
    }

    private func loadTransit() async {
        transitRequested = true
        if transit.value == nil { withMotion { transit = .loading } }
        let result = await Loadable.run { try await env.api.transitOptions(itineraryId: route.itineraryId, itemId: route.id) }
        if case .failed = result, transit.value != nil { return }
        withMotion(Motion.arrive) { transit = result }
    }

    /// Saves notes once typing pauses (the task restarts on every keystroke).
    private func saveNotesAfterPause() async {
        guard detail.value != nil, notes != savedNotes else { return }
        try? await Task.sleep(for: .milliseconds(800))
        guard !Task.isCancelled else { return }
        await saveNotes()
    }

    private func saveNotes() async {
        let text = notes
        withMotion {
            notesStatus = .saving
            notesError = nil
        }
        do {
            try await env.api.updateItemNotes(itineraryId: route.itineraryId, itemId: route.id, notes: text)
            savedNotes = text
            // Typed more meanwhile: the next pause saves that, so it's still "Saving…".
            withMotion { notesStatus = notes == text ? .saved : .saving }
        } catch {
            // Typing again cancelled this save; the next pause saves the newer text.
            guard !Task.isCancelled else { return }
            withMotion(Motion.arrive) {
                notesStatus = .idle
                notesError = "Couldn't save your notes."
            }
        }
    }

    /// Closing mid-typing still saves what was written.
    private func saveNotesOnClose() {
        guard detail.value != nil, notes != savedNotes else { return }
        let api = env.api, text = notes, itineraryId = route.itineraryId, itemId = route.id
        Task { try? await api.updateItemNotes(itineraryId: itineraryId, itemId: itemId, notes: text) }
    }

    /// Shows the stars right away ("Saving…"), confirms with "Saved", or rolls back and explains.
    private func rate(_ stars: Int) {
        guard let item = detail.value, !isRating, stars != rating else { return }
        let previous = rating
        rating = stars
        isRating = true
        withMotion {
            ratingError = nil
            ratingStatus = .saving
        }
        Task {
            do {
                try await env.api.rate(itemId: item.id, rating: Rating(stars: stars, tags: item.rating?.tags ?? [], note: item.rating?.note))
                withMotion { ratingStatus = .saved }
            } catch {
                withMotion(Motion.arrive) {
                    rating = previous
                    ratingStatus = .idle
                    ratingError = (error as? LocalizedError)?.errorDescription ?? "Couldn't save your rating."
                }
            }
            isRating = false
        }
    }
}
