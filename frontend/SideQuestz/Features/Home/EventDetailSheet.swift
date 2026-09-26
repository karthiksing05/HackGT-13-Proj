import SwiftUI

/// The Event sheet (GUI_PLAN.md §7.6): tap any block. Kind chip, title, time and place (with
/// "Running N min late" while `transit.delay` says so), description, who's in (groups), getting
/// there (not for busy blocks), Website / Get tickets (or the booked ticket), notes, and "Rate it
/// after". Presented by `HomeView` with `sqSheet(isPresented:style: .fixed(660))`.
///
/// Everything here saves to the server: the travel choice (`selectTransit`, shown at once and rolled
/// back with a message if it fails), notes and who sees them (`updateItemNotes` with its scope;
/// notes still unsaved when the sheet closes go to Home, which retries and says so if it can't), the
/// rating, and tickets through agent checkout. The checkout lives in `HomeStore`, so closing either
/// sheet while it pays doesn't lose it ("Get tickets" reads "Booking" until it lands).
///
/// Loading: opened from a timeline it shows the block's copy at once; otherwise a skeleton of the
/// sheet until the details arrive. "Getting there" loads on its own (in parallel when the kind is
/// known) with its own skeleton. Saves show a small "Saving…" → "Saved" status.
struct HomeEventSheet: View {
    let route: HomeEventRoute
    let store: HomeStore
    let close: () -> Void
    /// Something on Home may have changed (a booking).
    let didChange: () -> Void

    @Environment(AppEnvironment.self) private var env
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var detail: Loadable<ItineraryItem>
    @State private var transit: Loadable<[TransitOption]> = .loading
    @State private var transitRequested = false
    /// The travel mode shown (picked, or the saved one) and the last one the server confirmed.
    @State private var mode: TravelMode?
    @State private var savedMode: TravelMode?
    /// Picked here, so a later load doesn't overwrite the choice.
    @State private var transitTouched = false
    /// Picks are numbered: only the latest one sets the status or rolls back.
    @State private var transitPicks = 0
    @State private var transitConfirmed = 0
    @State private var transitStatus: HomeSaveState = .idle
    @State private var transitError: String?
    @State private var notes: String
    @State private var savedNotes: String
    /// Who sees the notes: what's shown (nil = the server hasn't said; the block's kind decides)
    /// and what the server has.
    @State private var scope: NotesScope?
    @State private var savedScope: NotesScope?
    /// Saves are numbered: only the latest one sets the status.
    @State private var notesSaves = 0
    @State private var notesConfirmed = 0
    @State private var notesError: HomeNotesError?
    @State private var notesStatus: HomeSaveState = .idle
    @State private var rating: Int
    @State private var isRating = false
    @State private var ratingError: String?
    @State private var ratingStatus: HomeSaveState = .idle
    @State private var browserLink: HomeBrowserLink?
    @State private var showsCheckout = false
    /// The checkout in the Checkout sheet (kept while the sheet animates away).
    @State private var shownCheckout: HomeCheckoutSession?
    @State private var openedCheckout = false
    @FocusState private var notesFocused: Bool
    /// The selected travel mode's tint and ring slide between the cards.
    @Namespace private var modeSelection

    init(route: HomeEventRoute, store: HomeStore, close: @escaping () -> Void, didChange: @escaping () -> Void) {
        self.route = route
        self.store = store
        self.close = close
        self.didChange = didChange
        // Notes this block's last sheet couldn't save come back (and save again).
        let pending = store.pendingNotes[route.id]
        _detail = State(initialValue: route.seed.map { .loaded($0) } ?? .loading)
        _notes = State(initialValue: pending?.text ?? route.seed?.notes ?? "")
        _savedNotes = State(initialValue: route.seed?.notes ?? "")
        _scope = State(initialValue: pending?.scope ?? route.seed?.notesScope)
        _savedScope = State(initialValue: route.seed?.notesScope)
        _mode = State(initialValue: route.seed?.transitMode)
        _savedMode = State(initialValue: route.seed?.transitMode)
        _rating = State(initialValue: route.seed?.rating?.stars ?? 0)
    }

    var body: some View {
        SheetScaffold {
            header(kind: detail.value?.kind)
            ZStack(alignment: .topLeading) {
                switch detail {
                case .loading:
                    HomeEventSkeleton()
                        .sqSlowLoading()
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
        .task(id: HomeNotesDraft(text: notes, loaded: detail.value != nil)) { await saveNotesAfterPause() }
        .task(id: notesStatus) {
            // "Saved" shows for a moment, then the header is back to just "NOTES".
            guard notesStatus == .saved else { return }
            try? await Task.sleep(for: .seconds(2))
            guard !Task.isCancelled else { return }
            withMotion { notesStatus = .idle }
        }
        .task(id: transitStatus) {
            guard transitStatus == .saved else { return }
            try? await Task.sleep(for: .seconds(2))
            guard !Task.isCancelled else { return }
            withMotion { transitStatus = .idle }
        }
        .task(id: ratingStatus) {
            guard ratingStatus == .saved else { return }
            try? await Task.sleep(for: .seconds(2))
            guard !Task.isCancelled else { return }
            withMotion { ratingStatus = .idle }
        }
        .onAppear { store.adoptPendingNotes(route.id) }
        .onDisappear(perform: saveNotesOnClose)
        // A purchase made here landed: its ticket replaces "Get tickets" (and Home reloads on close).
        .onChange(of: store.latestCheckout(for: route.id)?.bookedItem?.ticket) { _, ticket in ticketArrived(ticket) }
        .onChange(of: store.latestCheckout(for: route.id)?.state == .booked) { _, booked in
            if booked { didChange() }
        }
        .sheet(item: $browserLink) { link in
            SafariView(url: link.url).ignoresSafeArea()
        }
        .sqSheet(isPresented: $showsCheckout, style: SQSheetStyle(dim: 0.45), onDismiss: checkoutClosed) {
            HomeCheckoutSheetHost(session: $shownCheckout, isShared: sharesWithGroup, close: { showsCheckout = false })
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
            timeRow(item)
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
        notesBox(item)
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

    /// "2:30–4:00 PM", plus "· Running 8 min late" while the server says the block is late.
    private func timeRow(_ item: ItineraryItem) -> some View {
        let time = env.format.range(item.start, item.end)
        let late = store.delay(for: item.id)
        return HStack(spacing: 8) {
            HomeIcon(glyph: .clock, size: 18)
            Group {
                if let late {
                    Text(time) + Text(" · Running \(late) min late").fontWeight(.semibold).foregroundStyle(Theme.danger)
                } else {
                    Text(time)
                }
            }
            .sqFont(15)
            .homeLine(15)
        }
        .foregroundStyle(Theme.text2)
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: late)
        .accessibilityElement(children: .combine)
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
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text("GETTING THERE")
                .sqFont(13, .semibold, relativeTo: .footnote)
                .tracking(0.4)
                .foregroundStyle(Theme.text3)
                .homeLine(13)
                .accessibilityAddTraits(.isHeader)
            if transitStatus != .idle {
                HomeSaveStatusLabel(state: transitStatus)
                    .transition(.opacity)
            }
        }
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: transitStatus)
        ZStack(alignment: .topLeading) {
            switch transit {
            case .loading:
                HomeTransitSkeleton()
                    .sqSlowLoading()
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
                        let selected = selectedMode(in: options)
                        HStack(spacing: 8) {
                            ForEach(Array(options.enumerated()), id: \.element.id) { index, option in
                                transitCard(option, isSelected: option.mode == selected) { pick(option, in: options) }
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
        if let transitError {
            Text(transitError)
                .sqFont(13)
                .foregroundStyle(Theme.danger)
                .sqTransition(.rise)
        }
    }

    /// The saved (or just picked) mode; the first option until there is one.
    private func selectedMode(in options: [TransitOption]) -> TravelMode? {
        if let mode, options.contains(where: { $0.mode == mode }) { return mode }
        return options.first?.mode
    }

    /// A travel mode card. Picking one springs the sage tint and ring over from the last pick.
    private func transitCard(_ option: TransitOption, isSelected: Bool, action: @escaping () -> Void) -> some View {
        let cost = Self.cost(option)
        let shape = RoundedRectangle(cornerRadius: 14, style: .continuous)
        return Button(action: action) {
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
        let ticket = ticketInfo(item)
        let sellsTickets = item.bookable && ticket == nil
        if item.websiteURL != nil || sellsTickets {
            HStack(spacing: 10) {
                if let url = item.websiteURL {
                    Button {
                        browserLink = HomeBrowserLink(url: url)
                    } label: {
                        HStack(spacing: 6) {
                            HomeIcon(glyph: .globe, size: 18)
                            Text("Website")
                        }
                    }
                    .buttonStyle(.sq(fill: Theme.cream, foreground: Theme.ink, height: 48, fontSize: 15))
                    .accessibilityHint("Opens the event's website in the app")
                }
                if sellsTickets {
                    ticketsButton(item)
                        .transition(.opacity)
                }
            }
        } else if ticket == nil {
            // The prototype keeps an empty buttons row here, which adds one more gap.
            Color.clear.frame(height: 0).accessibilityHidden(true)
        }
        if let ticket {
            ticketStub(ticket)
                .sqTransition(.rise)
        }
    }

    /// The booked ticket: the item's own, or (until the server's copy of the item has it) the
    /// checkout that just booked here, so "Get tickets" never comes back for something bought.
    private func ticketInfo(_ item: ItineraryItem) -> HomeTicketInfo? {
        if let ticket = item.ticket {
            return HomeTicketInfo(quantity: ticket.quantity, confirmation: ticket.confirmation, url: ticket.url)
        }
        guard let session = store.latestCheckout(for: item.id), session.state == .booked else { return nil }
        let ticket = session.bookedItem?.ticket
        return HomeTicketInfo(quantity: ticket?.quantity ?? session.intent.value?.quantity ?? 1,
                              confirmation: ticket?.confirmation, url: ticket?.url)
    }

    /// "Get tickets", or "Buy instantly" when instant checkout covers the known price. While a
    /// purchase is paying it reads "Booking" and reopens the checkout to watch it land.
    private func ticketsButton(_ item: ItineraryItem) -> some View {
        let paying = store.checkout(for: item.id)?.isPaying == true
        let instant = buysInstantly(item)
        let label = instant ? "Buy instantly" : "Get tickets"
        return Button {
            openCheckout(item)
        } label: {
            ZStack {
                Text(label)
                    .opacity(paying ? 0 : 1)
                if paying {
                    HStack(spacing: 8) {
                        Text("Booking")
                        LoadingDots(color: Theme.ink, dotSize: 5)
                    }
                    .transition(.opacity)
                }
            }
        }
        .buttonStyle(.sq(fill: Theme.sage, foreground: Theme.ink, height: 48, fontSize: 15))
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: paying)
        .accessibilityLabel(paying ? "Booking your tickets" : label)
        .accessibilityHint(paying ? "Shows how the purchase is going"
                           : instant ? "The agent buys them right away with your card"
                           : "The agent gets them ready for you to approve")
    }

    /// Instant checkout is on and the known price is within its limit (the server has the last
    /// word: over the limit it asks for approval as usual).
    private func buysInstantly(_ item: ItineraryItem) -> Bool {
        guard let preferences = env.preferences, preferences.instantCheckout, let price = item.priceCents else { return false }
        return price <= preferences.instantCheckoutLimitCents
    }

    /// The ticket in place of "Get tickets": how many, the confirmation code (press and hold to
    /// copy it) and "View" when the ticket is a page.
    private func ticketStub(_ ticket: HomeTicketInfo) -> some View {
        HStack(spacing: 12) {
            Image(systemName: "ticket")
                .font(.system(size: 17, weight: .medium))
                .foregroundStyle(Theme.sageInk)
                .frame(width: 36, height: 36)
                .background(.white, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 0) {
                Text(ticket.quantity == 1 ? "Ticket booked" : "\(ticket.quantity) tickets booked")
                    .sqFont(15, .semibold)
                    .foregroundStyle(Theme.ink)
                    .homeLine(15)
                if let code = ticket.confirmation {
                    Text("Confirmation \(code)")
                        .sqFont(13)
                        .foregroundStyle(Theme.text2)
                        .homeLine(13)
                        .textSelection(.enabled)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityElement(children: .combine)
            if let url = ticket.url {
                Button("View") { browserLink = HomeBrowserLink(url: url) }
                    .buttonStyle(.sqPill(fill: Theme.sage, foreground: Theme.ink, height: 32, fontSize: 13, horizontalPadding: 14))
                    .accessibilityLabel("View ticket")
                    .accessibilityHint("Opens your ticket in the app")
            }
        }
        .padding(.vertical, 10)
        .padding(.leading, 12)
        .padding(.trailing, 14)
        .background(Theme.sageTint, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
    }

    // MARK: Notes

    private func notesBox(_ item: ItineraryItem) -> some View {
        let shown = shownScope(item)
        return VStack(alignment: .leading, spacing: 4) {
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
                scopeControl(item, shown: shown)
            }
            .homeLine(12)
            TextField("", text: $notes, prompt: Text("What to bring, where to meet, reminders…").foregroundStyle(Theme.text3), axis: .vertical)
                .lineLimit(3...10)
                .sqFont(15)
                .foregroundStyle(Theme.ink)
                .homeLine(15)
                .focused($notesFocused)
                .frame(minHeight: 3 * 15 * 1.35, alignment: .topLeading)
                .accessibilityLabel("Notes, \(Self.scopeLabel(shown))")
            if let notesError {
                HStack(spacing: 8) {
                    Text(notesError.message).sqFont(13).foregroundStyle(Theme.danger)
                    if notesError.canRetry {
                        Button("Try again") { Task { await saveNotes() } }
                            .buttonStyle(.sqLink(size: 13))
                    }
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

    /// Who sees the notes. When there are others to share them with, the label is a small menu.
    @ViewBuilder
    private func scopeControl(_ item: ItineraryItem, shown: NotesScope) -> some View {
        let label = Text(Self.scopeLabel(shown))
            .sqFont(11, relativeTo: .caption2)
            .foregroundStyle(Theme.text3)
        if notesCanBeShared(item) {
            Menu {
                Picker("Who sees these notes", selection: Binding(get: { shown }, set: { changeScope(to: $0, item: item) })) {
                    Label("Only you", systemImage: "lock").tag(NotesScope.personal)
                    Label("Shared with the group", systemImage: "person.2").tag(NotesScope.shared)
                }
            } label: {
                HStack(spacing: 3) {
                    label
                    Image(systemName: "chevron.up.chevron.down")
                        .font(.system(size: 8, weight: .semibold))
                        .foregroundStyle(Theme.text3)
                }
                .contentShape(Rectangle().inset(by: -12))
            }
            .menuOrder(.fixed)
            .accessibilityLabel("Who sees these notes")
            .accessibilityValue(Self.scopeLabel(shown))
        } else {
            label
        }
    }

    /// The server's scope, or (until it says) the kind's: group plans share, the rest are yours.
    private func shownScope(_ item: ItineraryItem) -> NotesScope {
        scope ?? (item.kind == .group ? .shared : .personal)
    }

    /// There's a group to share notes with: a group block, or a plan others have joined.
    private func notesCanBeShared(_ item: ItineraryItem) -> Bool {
        item.kind == .group || route.planIsShared
    }

    /// Others see this block's plan (the Checkout sheet says the group sees the ticket too).
    private var sharesWithGroup: Bool {
        route.planIsShared || detail.value?.kind == .group
    }

    /// The scope was changed here and the server doesn't have it yet.
    private var scopeChanged: Bool {
        scope != nil && scope != savedScope
    }

    private static func scopeLabel(_ scope: NotesScope) -> String {
        switch scope {
        case .personal: "Only you"
        case .shared: "Shared with the group"
        }
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

    // MARK: Loading

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
                if scope == savedScope {
                    scope = item.notesScope
                    savedScope = item.notesScope
                }
                if !transitTouched {
                    mode = item.transitMode
                    savedMode = item.transitMode
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
        if route.opensCheckout, item.bookable, ticketInfo(item) == nil, !openedCheckout {
            openedCheckout = true
            // Let the sheet finish sliding up before Checkout goes on top.
            try? await Task.sleep(for: .milliseconds(450))
            openCheckout(item)
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

    // MARK: Saving

    /// Shows the new choice at once and saves it (`selectTransit`); if the server says no, the last
    /// saved choice comes back with a short message.
    private func pick(_ option: TransitOption, in options: [TransitOption]) {
        guard option.mode != selectedMode(in: options) else { return }
        transitTouched = true
        transitPicks += 1
        let pick = transitPicks
        withMotion(Motion.arrive) {
            mode = option.mode
            transitError = nil
            transitStatus = .saving
        }
        Task {
            do {
                try await env.api.selectTransit(itineraryId: route.itineraryId, itemId: route.id, mode: option.mode)
                if pick > transitConfirmed {
                    transitConfirmed = pick
                    savedMode = option.mode
                    store.updateItem(route.id) { $0.transitMode = option.mode }
                }
                guard pick == transitPicks else { return }
                withMotion { transitStatus = .saved }
            } catch {
                guard pick == transitPicks else { return }
                withMotion(Motion.arrive) {
                    mode = savedMode
                    transitStatus = .idle
                    transitError = "Couldn't save your choice. Try again."
                }
            }
        }
    }

    /// A new audience for the notes saves right away (with the text as it is). If the server says
    /// no, the old one comes back.
    private func changeScope(to newScope: NotesScope, item: ItineraryItem) {
        guard newScope != shownScope(item) else { return }
        withMotion(Motion.quick) { scope = newScope }
        Task { await saveNotes() }
    }

    /// Saves notes once typing pauses (the task restarts on every keystroke, and once the details
    /// are in, so notes carried over from a failed save go out too).
    private func saveNotesAfterPause() async {
        guard detail.value != nil, notes != savedNotes || scopeChanged else { return }
        try? await Task.sleep(for: .milliseconds(800))
        guard !Task.isCancelled else { return }
        await saveNotes()
    }

    private func saveNotes() async {
        guard let item = detail.value else { return }
        let text = notes, sending = shownScope(item), changingScope = scopeChanged
        notesSaves += 1
        let save = notesSaves
        withMotion {
            notesStatus = .saving
            notesError = nil
        }
        do {
            try await env.api.updateItemNotes(itineraryId: route.itineraryId, itemId: route.id, notes: text, scope: sending)
            if save > notesConfirmed {
                notesConfirmed = save
                savedNotes = text
                savedScope = sending
                store.notesConfirmed(itemId: route.id, text: text, scope: sending)
            }
            guard save == notesSaves else { return }
            // Typed more meanwhile: the next pause saves that, so it's still "Saving…".
            withMotion { notesStatus = notes == text ? .saved : .saving }
        } catch {
            // Typing again cancelled this save; the next pause saves the newer text.
            guard !Task.isCancelled, save == notesSaves else { return }
            withMotion(Motion.arrive) {
                notesStatus = .idle
                if changingScope, scope == sending {
                    scope = savedScope
                    notesError = HomeNotesError(message: "Couldn't change who sees your notes.", canRetry: false)
                } else {
                    notesError = HomeNotesError(message: "Couldn't save your notes.", canRetry: true)
                }
            }
        }
    }

    /// Closing mid-typing (or before a save came back) still saves what was written: Home takes
    /// over, retries, and shows a banner if it still can't.
    private func saveNotesOnClose() {
        guard let item = detail.value, notes != savedNotes || scopeChanged else { return }
        store.saveNotesLater(HomePendingNotes(itemId: route.id, itineraryId: route.itineraryId, title: item.title,
                                              text: notes, scope: shownScope(item)), env: env)
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

    // MARK: Checkout

    private func openCheckout(_ item: ItineraryItem) {
        shownCheckout = store.startCheckout(for: item, env: env)
        showsCheckout = true
    }

    /// Before paying, closing drops the purchase; while it pays, the store keeps following it.
    private func checkoutClosed() {
        shownCheckout?.sheetClosed()
        shownCheckout = nil
    }

    private func ticketArrived(_ ticket: Ticket?) {
        guard let ticket, var item = detail.value, item.ticket != ticket else { return }
        item.ticket = ticket
        withMotion(Motion.arrive) { detail = .loaded(item) }
    }
}

/// The Checkout sheet for the checkout being shown (read through the binding, so it's never stale).
private struct HomeCheckoutSheetHost: View {
    @Binding var session: HomeCheckoutSession?
    let isShared: Bool
    let close: () -> Void

    var body: some View {
        if let session {
            HomeCheckoutSheet(session: session, isShared: isShared, close: close)
        }
    }
}

/// What the notes autosave waits on: the text, and whether the details are in.
private struct HomeNotesDraft: Equatable {
    let text: String
    let loaded: Bool
}

private struct HomeNotesError: Equatable {
    let message: String
    /// A text save can be tried again; a scope change is simply picked again.
    let canRetry: Bool
}

/// A booked ticket as the Event sheet shows it.
private struct HomeTicketInfo {
    let quantity: Int
    let confirmation: String?
    let url: URL?
}
