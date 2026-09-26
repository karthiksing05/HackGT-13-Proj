import SwiftUI

/// The Event sheet (GUI_PLAN.md §7.6): tap any block. Kind chip, title, time and place,
/// description, who's in (groups), getting there (not for busy blocks), Website / Get tickets,
/// notes, and "Rate it after". Presented by `HomeView` with `sqSheet(isPresented:style: .fixed(660))`.
struct HomeEventSheet: View {
    let route: HomeEventRoute
    let close: () -> Void
    /// Something on Home may have changed (a booking).
    let didChange: () -> Void

    @Environment(AppEnvironment.self) private var env
    @State private var detail: Loadable<ItineraryItem>
    @State private var transit: Loadable<[TransitOption]> = .loading
    @State private var mode: TravelMode?
    @State private var notes: String
    @State private var savedNotes: String
    @State private var notesError: String?
    @State private var rating: Int
    @State private var isRating = false
    @State private var ratingError: String?
    @State private var showsBrowser = false
    @State private var showsCheckout = false
    @State private var openedCheckout = false
    @FocusState private var notesFocused: Bool

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
            switch detail {
            case .loading:
                header(kind: nil)
                LoadingStateView(minHeight: 420)
            case .failed(let message):
                header(kind: nil)
                ErrorStateView(message: message, minHeight: 420) { Task { await load() } }
            case .loaded(let item):
                content(item)
            }
        }
        .task { await load() }
        .task(id: notes) { await saveNotesAfterPause() }
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
        header(kind: item.kind)
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

    private func header(kind: BlockKind?) -> some View {
        HStack(spacing: 8) {
            if let kind {
                // The kind chip in the prototype's line box (13pt × 1.35 + 4pt padding).
                TagLabel(text: kind.palette.label, fill: kind.palette.background, foreground: kind.palette.text,
                         fontSize: 13, horizontalPadding: 10, verticalPadding: 5, radius: 8)
            }
            Spacer(minLength: 0)
            CloseCircleButton(action: close)
                .padding(-6)
        }
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
        switch transit {
        case .loading:
            LoadingStateView(minHeight: 84)
        case .failed(let message):
            ErrorStateView(message: message, minHeight: 84) { Task { await loadTransit() } }
        case .loaded(let options):
            if options.isEmpty {
                Text("No routes to show yet.")
                    .sqFont(15)
                    .foregroundStyle(Theme.text3)
            } else {
                HStack(spacing: 8) {
                    ForEach(options) { transitCard($0, isSelected: $0.mode == (mode ?? options.first?.mode)) }
                }
                .sensoryFeedback(.selection, trigger: mode)
            }
        }
    }

    private func transitCard(_ option: TransitOption, isSelected: Bool) -> some View {
        let cost = Self.cost(option)
        let shape = RoundedRectangle(cornerRadius: 14, style: .continuous)
        return Button {
            mode = option.mode
        } label: {
            VStack(spacing: 3) {
                HomeIcon(glyph: Self.glyph(option.mode), size: 22)
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
            .background(isSelected ? Theme.sageTint : .white, in: shape)
            .overlay {
                // box-shadow ring outside the card: 2pt sage when selected, 1pt `line` otherwise.
                let width: CGFloat = isSelected ? 2 : 1
                RoundedRectangle(cornerRadius: 14 + width, style: .continuous)
                    .strokeBorder(isSelected ? Theme.sage : Theme.line, lineWidth: width)
                    .padding(-width)
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
                    Text("Tunes your future picks")
                        .sqFont(12, relativeTo: .caption)
                        .foregroundStyle(Theme.text3)
                        .homeLine(12)
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
            }
        }
    }

    // MARK: Loading + saving

    private func load() async {
        if detail.value == nil { detail = .loading }
        let result = await Loadable.run { try await env.api.eventDetail(id: route.id) }
        switch result {
        case .loaded(let item):
            detail = .loaded(item)
            if notes == savedNotes {
                notes = item.notes ?? ""
                savedNotes = notes
            }
            if !isRating { rating = item.rating?.stars ?? 0 }
        case .failed(let message):
            // Keep showing the timeline's copy of the block if we have one.
            if detail.value == nil { detail = .failed(message) }
        case .loading:
            break
        }
        guard let item = detail.value else { return }
        if item.kind != .busy { await loadTransit() }
        if route.opensCheckout, item.bookable, !openedCheckout {
            openedCheckout = true
            // Let the sheet finish sliding up before Checkout goes on top.
            try? await Task.sleep(for: .milliseconds(450))
            showsCheckout = true
        }
    }

    private func loadTransit() async {
        transit = .loading
        transit = await Loadable.run { try await env.api.transitOptions(itineraryId: route.itineraryId, itemId: route.id) }
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
        do {
            try await env.api.updateItemNotes(itineraryId: route.itineraryId, itemId: route.id, notes: text)
            savedNotes = text
            notesError = nil
        } catch {
            notesError = "Couldn't save your notes."
        }
    }

    /// Closing mid-typing still saves what was written.
    private func saveNotesOnClose() {
        guard detail.value != nil, notes != savedNotes else { return }
        let api = env.api, text = notes, itineraryId = route.itineraryId, itemId = route.id
        Task { try? await api.updateItemNotes(itineraryId: itineraryId, itemId: itemId, notes: text) }
    }

    private func rate(_ stars: Int) {
        guard let item = detail.value, !isRating, stars != rating else { return }
        let previous = rating
        rating = stars
        isRating = true
        ratingError = nil
        Task {
            do {
                try await env.api.rate(itemId: item.id, rating: Rating(stars: stars, tags: item.rating?.tags ?? [], note: item.rating?.note))
            } catch {
                rating = previous
                ratingError = (error as? LocalizedError)?.errorDescription ?? "Couldn't save your rating."
            }
            isRating = false
        }
    }
}
