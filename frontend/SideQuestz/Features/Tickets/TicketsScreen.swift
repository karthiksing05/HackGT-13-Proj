import SwiftUI

/// Account › Your tickets is showing (`Router.tickets`), opened on one ticket when `ticketId` is set
/// (the `tickets/<ticketId>` demo link).
struct TicketsRoute: Identifiable, Equatable {
    let id = UUID()
    var ticketId: String?
}

/// Where Your tickets shows: in an overlay of the Account tab, sliding in from the side like a pushed
/// screen ("‹ Account" goes back; the tab bar stays). It also keeps the list fresh while the app
/// runs, so the Account row's count follows bookings as they land.
struct TicketsScreenHost: View {
    let model: TicketsModel
    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        ZStack {
            // Keeps the host (and its task) in place without covering Account: zero-sized, so
            // nothing over the tab's content takes taps or reads as covering it in UI tests.
            Color.clear
                .frame(width: 0, height: 0)
                .accessibilityHidden(true)
            if let route = router.tickets {
                TicketsScreen(model: model, route: route)
                    .transition(reduceMotion ? .opacity : .move(edge: .trailing))
            }
        }
        .task { await model.listen(env) }
    }
}

/// Account › Your tickets: every ticket you've bought, yourself or through Muse. Upcoming first
/// (soonest first), then Past (latest first), each a ticket-shaped card; tap one for the ticket
/// itself (`TicketDetailSheet`), whose "Go to sidequest" takes you to its stop on Home.
///
/// Loading follows the app's policy: ticket-shaped skeletons at once, the logo after a slow moment,
/// one line and "Try again" when the first load fails. Reloads (pull to refresh, a booking landing)
/// keep the list on screen and swap the new one in.
struct TicketsScreen: View {
    let model: TicketsModel
    let route: TicketsRoute

    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var detail: MyTicket?
    /// "Go to sidequest" was tapped: Home takes over once the sheet is gone.
    @State private var goingTo: MyTicket?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                header
                content
                    .padding(.horizontal, Metrics.side)
                    .padding(.bottom, 28)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .scrollIndicators(.hidden)
        .sqPullToRefresh()
        .background(Theme.cream.ignoresSafeArea())
        .sqReloadable("tickets") { await model.load(env) }
        .task(id: route.id) { await open(route) }
        .sqSheet(item: $detail, style: TicketDetailSheet.style, onDismiss: detailClosed) { ticket in
            TicketDetailSheet(ticket: ticket, goToSidequest: canGo(to: ticket) ? { goTo(ticket) } : nil) { detail = nil }
        }
    }

    // MARK: Header

    private var header: some View {
        VStack(alignment: .leading, spacing: 6) {
            BackButton(title: "Account") { router.closeTickets() }
                .accessibilityIdentifier("tickets.back")
                .padding(.horizontal, 12)
                .frame(height: 44)
            Text("Your tickets")
                .largeTitleStyle()
                .accessibilityAddTraits(.isHeader)
                .padding(.horizontal, Metrics.side)
        }
        .designTopPadding(50, minimum: 0)
        .padding(.bottom, 4)
    }

    // MARK: Content

    private var content: some View {
        ZStack(alignment: .top) {
            switch model.tickets {
            case .loading:
                TicketCardSkeleton()
                    .sqShimmer(active: router.tab == .account)
                    .sqSlowLoading(router.tab == .account, lines: ["Getting your tickets…"])
                    .padding(.top, 18)
                    .transition(.opacity)
            case .failed(let message):
                ErrorStateView(message: message, minHeight: 260) { Task { await model.retry(env) } }
                    .transition(.opacity)
            case .loaded:
                if let sections = model.sections(now: env.clock.now), !sections.isEmpty {
                    list(sections)
                        .transition(.opacity)
                } else {
                    emptyState
                        .padding(.top, 18)
                        .transition(.opacity)
                }
            }
        }
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: model.tickets.phase)
    }

    private func list(_ sections: TicketSections) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            if !sections.upcoming.isEmpty {
                section("Upcoming", sections.upcoming, isPast: false)
            }
            if !sections.past.isEmpty {
                section("Past", sections.past, isPast: true)
            }
        }
    }

    private func section(_ title: String, _ tickets: [MyTicket], isPast: Bool) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            SectionHeader(title: title)
                .authLineHeight(1.35, size: 19, mono: true)
            ForEach(Array(tickets.enumerated()), id: \.element.id) { index, ticket in
                TicketCard(ticket: ticket, isPast: isPast) { detail = ticket }
                    .sqAppear(index)
                    .sqTransition(.rise)
            }
        }
        .padding(.top, 18)
    }

    /// Nothing bought yet.
    private var emptyState: some View {
        VStack(spacing: 10) {
            Image(systemName: "ticket")
                .font(.system(size: 22, weight: .medium))
                .foregroundStyle(Theme.sageInk)
                .frame(width: 52, height: 52)
                .background(Theme.sageTint, in: Circle())
                .accessibilityHidden(true)
            Text("No tickets yet")
                .sqFont(17, .semibold)
                .foregroundStyle(Theme.ink)
            Text("Tickets you buy, or that Muse buys for you, show up here.")
                .sqFont(15)
                .foregroundStyle(Theme.text3)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.vertical, 28)
        .padding(.horizontal, 24)
        .frame(maxWidth: .infinity)
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
        .accessibilityElement(children: .combine)
    }

    // MARK: Actions

    /// Loads (or refreshes) the list; a route with a ticket opens it once it's there.
    private func open(_ route: TicketsRoute) async {
        await model.load(env)
        guard let id = route.ticketId, let ticket = model.ticket(id: id) else { return }
        try? await Task.sleep(for: .milliseconds(350))
        detail = ticket
    }

    /// "Go to sidequest" takes you to a stop that's still ahead on a plan that's still yours.
    private func canGo(to ticket: MyTicket) -> Bool {
        ticket.itineraryTitle != nil && !ticket.itineraryId.isEmpty && ticket.isUpcoming(at: env.clock.now)
    }

    private func goTo(_ ticket: MyTicket) {
        goingTo = ticket
        detail = nil
    }

    /// Home presents the stop's sheet, so it waits until this one is gone.
    private func detailClosed() {
        guard let ticket = goingTo else { return }
        goingTo = nil
        router.goToStop(ticket.itemId, itineraryId: ticket.itineraryId)
    }
}
