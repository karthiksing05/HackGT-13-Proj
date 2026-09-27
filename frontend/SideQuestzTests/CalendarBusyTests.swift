import Foundation
import Testing
@testable import SideQuestz

/// Busy blocks from people's calendars (the server's `calendar_events`), as the live server sends them:
/// on the calendar's days, behind a tapped block, and as the reason Review shows when they fill a window.
@MainActor
struct CalendarBusyTests {
    private let decoder = APICoding.decoder(timeZone: TimeZone(identifier: "America/New_York")!)

    @Test func calendarFullHasItsOwnSentence() throws {
        let reason = PlanEmptyReason(rawValue: "calendar_full")
        #expect(reason == .calendarFull)
        #expect(reason.rawValue == "calendar_full")
        #expect(reason.message == "Your calendar is full then. Try another time or day.")
        let batch = try decoder.decode(PlanBatch.self, from: Data(#"{"planner":"dag","run_id":"run_1","options":[],"done":true,"reason":"calendar_full"}"#.utf8))
        #expect(batch.options.isEmpty && batch.reason == .calendarFull)
    }

    /// GET /calendar/days: a busy block has its calendar_events id and no plan, next to a plan's stop.
    @Test func liveCalendarDaysCarryBusyBlocks() throws {
        let json = """
        [{"id":"2026-09-28","date":"2026-09-28T04:00:00Z","items":[
          {"id":"019a2b3c-cal","kind":"busy","title":"MATH 3012","start":"2026-09-28T13:30:00Z","end":"2026-09-28T14:45:00Z","people":[],"interested":[]},
          {"id":"019a2b3c-stop","kind":"sidequest","title":"Tacos","start":"2026-09-28T15:15:00Z","end":"2026-09-28T16:15:00Z","people":[],"interested":[],"itinerary_id":"019a2b3c-plan"},
          {"id":"019a2b3c-cal2","kind":"busy","title":"CS 3510 lecture","start":"2026-09-28T17:00:00Z","end":"2026-09-28T17:50:00Z","people":[],"interested":[]}]}]
        """
        let days = try decoder.decode([CalendarDay].self, from: Data(json.utf8))
        let items = try #require(days.first).items
        #expect(items.map(\.kind) == [.busy, .sidequest, .busy])
        #expect(items[0].itineraryId == nil && items[0].people.isEmpty && items[2].title == "CS 3510 lecture")
        #expect(items[1].itineraryId == "019a2b3c-plan")
        #expect(items[0].kind.palette.label == "Calendar")
    }

    /// GET /events/{id} for a tapped busy block: what it is, where, and the viewer's own note.
    @Test func liveBusyBlockDetail() throws {
        let json = """
        {"id":"019a2b3c-cal","kind":"busy","title":"MATH 3012","place":{"name":"Klaus Advanced Computing Building"},
         "start":"2026-09-28T13:30:00Z","end":"2026-09-28T14:45:00Z","description":"From your calendar. SideQuests plans around it.",
         "bookable":false,"people":[],"interested":[],"extra_going":0,"notes":"Bring the problem set","notes_scope":"private"}
        """
        let item = try decoder.decode(ItineraryItem.self, from: Data(json.utf8))
        #expect(item.kind == .busy && item.place?.name == "Klaus Advanced Computing Building")
        #expect(item.notes == "Bring the problem set" && item.notesScope == .personal)
        #expect(item.description == "From your calendar. SideQuests plans around it." && !item.bookable && !item.hasPeople)
    }
}
