import Observation
import SwiftUI

/// Your tickets (`GET /me/tickets`): the list behind Account › Your tickets and the row's upcoming
/// count. Owned by Account (`AccountMeModel.tickets`), so the row and the screen share one list.
///
/// A reload keeps what's on screen and swaps the fresh list in; a failed reload keeps the old one.
/// A booking that lands while the app is open (`checkout.status` booked, a Muse run finishing)
/// reloads it, the same moment Home reloads its plans.
@Observable
final class TicketsModel {
    private(set) var tickets: Loadable<[MyTicket]> = .loading

    func load(_ env: AppEnvironment) async {
        let result = await Loadable.run { try await env.api.myTickets() }
        guard result.value != nil || tickets.value == nil else { return }
        withMotion(Motion.arrive) { tickets = result }
    }

    /// "Try again" after a failed first load: the skeleton comes back while it loads.
    func retry(_ env: AppEnvironment) async {
        withMotion { tickets = .loading }
        await load(env)
    }

    /// Upcoming and past, against the app's clock.
    func sections(now: Date) -> TicketSections? {
        tickets.value.map { TicketSections($0, now: now) }
    }

    /// How many are still ahead (nil until the list is in).
    func upcomingCount(now: Date) -> Int? {
        tickets.value.map { list in list.filter { $0.isUpcoming(at: now) }.count }
    }

    func ticket(id: String) -> MyTicket? {
        tickets.value?.first { $0.id == id }
    }

    /// Follows bookings while the app runs (`WS /ws`).
    func listen(_ env: AppEnvironment) async {
        for await event in env.realtime.subscribe() {
            switch event {
            case .checkoutStatus(_, let state) where state == .booked:
                Task { await load(env) }
            case .checkoutRun(_, let state, _) where state != .running:
                Task { await load(env) }
            default:
                break
            }
        }
    }
}
