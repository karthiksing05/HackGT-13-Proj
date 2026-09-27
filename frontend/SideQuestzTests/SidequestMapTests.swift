import CoreLocation
import Foundation
import MapKit
import Testing
@testable import SideQuestz

/// Home › Sidequests: where a sidequest stands at a given time and place (the progress strip), how
/// distances read, the route its map draws, and each sidequest keeping its own Timeline | Map.
/// The demo: "Free Friday afternoon" 1–8 PM from Tech Square to Home (Skyline Park rooftop
/// 2:30–4:00, Krog Street Tunnel murals 4:20–5:30, group dinner 6:00–7:30) and "Saturday reset",
/// a round trip from Home whose last stop has no location.
@MainActor
struct SidequestMapTests {
    private let clock = AppClock.demo
    private let friday = MockData.itineraries()[0]
    private let saturday = MockData.itineraries()[1]
    private let techSquare = MockPlaces.techSquare.coordinate
    private let home = MockPlaces.home.coordinate
    private let skyline = MockPlaces.stops["Skyline Park rooftop"]!.coordinate
    private let murals = MockPlaces.stops["Krog Street Tunnel murals"]!.coordinate

    private func at(_ hour: Int, _ minute: Int = 0, day: Int = 25) -> Date {
        clock.date(2026, 9, day, hour, minute)
    }

    private func progress(_ itinerary: Itinerary, _ now: Date, here: Coordinate? = nil) -> SidequestProgress {
        SidequestProgress(itinerary: itinerary, now: now, calendar: clock.calendar, here: here)
    }

    /// The strip's line, seen from `here`.
    private func line(_ itinerary: Itinerary, _ now: Date, here: Coordinate?) -> SidequestProgressLine {
        SidequestProgressLine(progress(itinerary, now, here: here), format: TimeFormat(clock: clock), now: now)
    }

    private func item(_ id: String, in itinerary: Itinerary? = nil) -> ItineraryItem {
        (itinerary ?? friday).items.first { $0.id == id }!
    }

    /// `meters` due north of `c` (a degree of latitude is about 111 km).
    private func north(of c: Coordinate, meters: Double) -> Coordinate {
        Coordinate(lat: c.lat + meters / 111_000, lng: c.lng)
    }

    // MARK: Progress

    @Test func phasesThroughTheDay() {
        let a3 = item("a3"), a5 = item("a5"), a6 = item("a6")
        #expect(progress(friday, at(12)).phase == .startsSoon(start: at(13)))
        // In the lecture (a calendar block) and in transit, you're on the way to the first stop.
        #expect(progress(friday, at(13, 20)).phase == .onTheWay(next: a3, at: at(14, 30)))
        #expect(progress(friday, at(14, 10)).phase == .onTheWay(next: a3, at: at(14, 30)))
        #expect(progress(friday, at(15, 15)).phase == .atStop(a3, until: at(16)))
        #expect(progress(friday, at(16, 10)).phase == .onTheWay(next: a5, at: at(16, 20)))
        #expect(progress(friday, at(18, 45)).phase == .atStop(a6, until: at(19, 30)))
        #expect(progress(friday, at(19, 30)).phase == .done)
        // Another day: Friday seen from Thursday, Saturday seen from Friday.
        #expect(progress(friday, at(17, day: 24)).phase == .later(start: at(13)))
        #expect(progress(saturday, at(14, 10)).phase == .later(start: at(13, day: 26)))
    }

    @Test func headingBackAfterTheLastStop() {
        var itinerary = friday
        itinerary.items.append(ItineraryItem(id: "a7", kind: .transit, title: "Transit home", place: Place(name: "Krog Street Market → Home"),
                                             start: at(19, 30), end: at(19, 55)))
        let evening = progress(itinerary, at(19, 40))
        #expect(evening.phase == .headingBack(until: at(19, 55)))
        #expect(evening.completed == 3)
        #expect(evening.target == nil)
        #expect(progress(itinerary, at(19, 55)).phase == .done)
    }

    @Test func fillsAndCompletedStops() {
        #expect(progress(friday, at(14, 10)).fills == [0, 0, 0])
        // 45 of Skyline Park's 90 minutes.
        let rooftop = progress(friday, at(15, 15))
        #expect(rooftop.fills == [0.5, 0, 0])
        #expect(rooftop.completed == 0)
        // 40 of the murals' 70 minutes, the rooftop behind you.
        let tunnel = progress(friday, at(17))
        #expect(tunnel.fills[0] == 1 && tunnel.fills[2] == 0)
        #expect(abs(tunnel.fills[1] - 40.0 / 70) < 1e-9)
        #expect(tunnel.completed == 1)
        let evening = progress(friday, at(19, 30))
        #expect(evening.fills == [1, 1, 1])
        #expect(evening.completed == 3)
        // Only sidequest and group blocks are stops, in the order you visit them.
        #expect(evening.stops.map(\.id) == ["a3", "a5", "a6"])
    }

    @Test func targetAndDistance() throws {
        let heading = progress(friday, at(14, 10), here: techSquare)
        #expect(heading.target?.id == "a3")
        let expected = CLLocation(latitude: techSquare.lat, longitude: techSquare.lng)
            .distance(from: CLLocation(latitude: skyline.lat, longitude: skyline.lng))
        #expect(abs(try #require(heading.distance) - expected) < 0.5)
        #expect(!heading.isHere && !heading.isFarAway)
        // Before it starts, the first stop; once the stops are behind you, none.
        #expect(progress(friday, at(12), here: techSquare).target?.id == "a3")
        #expect(progress(friday, at(19, 30), here: techSquare).target == nil)
        // No location, no distance.
        #expect(progress(friday, at(14, 10)).distance == nil)
    }

    @Test func youreHereWithin150Meters() {
        let near = north(of: skyline, meters: 140), past = north(of: skyline, meters: 160)
        #expect(SidequestProgress.meters(near, skyline) < SidequestProgress.hereRadius)
        #expect(SidequestProgress.meters(past, skyline) > SidequestProgress.hereRadius)
        #expect(progress(friday, at(15, 15), here: near).isHere)
        #expect(!progress(friday, at(15, 15), here: past).isHere)
        // Early at the next stop counts too; before the sidequest starts, it doesn't.
        #expect(progress(friday, at(16, 10), here: north(of: murals, meters: 100)).isHere)
        #expect(!progress(friday, at(12), here: skyline).isHere)
    }

    @Test func farAwayBeyond50Kilometers() throws {
        // Tech Square is the route's northernmost point, so straight north it's the nearest.
        let inTown = north(of: techSquare, meters: 49_000), outOfTown = north(of: techSquare, meters: 51_000)
        let near = progress(friday, at(14, 10), here: inTown)
        #expect(!near.isFarAway)
        #expect(abs(try #require(near.distance) - SidequestProgress.meters(inTown, skyline)) < 0.5)

        let far = progress(friday, at(14, 10), here: outOfTown)
        #expect(far.isFarAway)
        #expect(!far.isHere)
        // The distance is to the nearest point on the route, not to the next stop.
        let nearest = SidequestProgress.meters(outOfTown, techSquare)
        #expect(nearest > SidequestProgress.farAwayDistance)
        #expect(abs(try #require(far.distance) - nearest) < 0.5)
        #expect(line(friday, at(14, 10), here: outOfTown).note == "You're 32 mi away")
    }

    // MARK: Words

    @Test func stripLines() {
        let next = line(friday, at(14, 10), here: techSquare)
        #expect(next.display == "Next: Skyline Park rooftop at 2:30 PM · in 20 min · 1.4 mi")
        #expect(next.spoken == "Next: Skyline Park rooftop at 2:30 PM, in 20 minutes, 1.4 miles away, 0 of 3 stops done")
        #expect(line(friday, at(12), here: techSquare).display == "Starts at 1:00 PM · in 1 hr")
        #expect(line(friday, at(15, 15), here: skyline).display == "Now at Skyline Park rooftop · until 4:00 PM · You're here")
        #expect(line(friday, at(15, 15), here: techSquare).display == "Now at Skyline Park rooftop · until 4:00 PM · 1.4 mi away")
        #expect(line(friday, at(17), here: nil).display == "Now at Krog Street Tunnel murals · until 5:30 PM")
        #expect(line(saturday, at(14, 10), here: techSquare).display == "Starts Sat at 1:00 PM · 3 stops")
        #expect(line(saturday, at(14, 10), here: techSquare).spoken == "Starts Saturday, September 26 at 1:00 PM, 3 stops")
        #expect(line(friday, at(19, 30), here: techSquare).display == "Done · 3 of 3 stops")

        // Far from the route: how far instead of the distance to the stop (never once it's over).
        let newYork = Coordinate(lat: 40.7128, lng: -74.0060)
        func nearest(_ itinerary: Itinerary) -> String {
            DistanceFormat.short(SidequestRoute(itinerary: itinerary).path.map { SidequestProgress.meters(newYork, $0) }.min() ?? 0)
        }
        #expect(line(friday, at(14, 10), here: newYork).display
            == "Next: Skyline Park rooftop at 2:30 PM · in 20 min · You're \(nearest(friday)) away")
        #expect(line(saturday, at(14, 10), here: newYork).display == "Starts Sat at 1:00 PM · 3 stops · You're \(nearest(saturday)) away")
        #expect(line(friday, at(19, 30), here: newYork).note == nil)
    }

    @Test func distanceFormatting() {
        #expect(DistanceFormat.short(0) == "50 ft")
        #expect(DistanceFormat.short(10) == "50 ft")        // 33 ft
        #expect(DistanceFormat.short(100) == "350 ft")      // 328 ft
        #expect(DistanceFormat.short(150) == "500 ft")      // 492 ft
        #expect(DistanceFormat.short(161) == "0.1 mi")
        #expect(DistanceFormat.short(2235.5) == "1.4 mi")
        #expect(DistanceFormat.short(15_900) == "9.9 mi")
        #expect(DistanceFormat.short(16_050) == "10 mi")    // 9.97 mi: whole miles from here
        #expect(DistanceFormat.short(423_250) == "263 mi")
        #expect(DistanceFormat.short(2_000_000) == "1,243 mi")
        #expect(DistanceFormat.spoken(150) == "500 feet")
        #expect(DistanceFormat.spoken(2235.5) == "1.4 miles")
        #expect(DistanceFormat.spoken(1609.344) == "1 mile")
        #expect(DistanceFormat.spoken(423_250) == "263 miles")
    }

    @Test func countdownFormatting() {
        let format = TimeFormat(clock: clock)
        #expect(format.countdown(to: at(14, 30), from: at(14, 10)) == "in 20 min")
        #expect(format.countdown(to: at(15, 10), from: at(14, 10)) == "in 1 hr")
        #expect(format.countdown(to: at(15, 15), from: at(14, 10)) == "in 1 hr 5 min")
        // A started minute counts.
        #expect(format.countdown(to: at(14, 10).addingTimeInterval(20), from: at(14, 10)) == "in 1 min")
        #expect(format.countdown(to: at(15, 15), from: at(14, 10), spoken: true) == "in 1 hour 5 minutes")
        #expect(format.countdown(to: at(14, 11), from: at(14, 10), spoken: true) == "in 1 minute")
    }

    // MARK: Route

    @Test func routeRunsInVisitOrder() {
        let route = SidequestRoute(itinerary: friday)
        #expect(!route.isRoundTrip)
        #expect(route.start == techSquare)
        #expect(route.end == home)
        #expect(route.stops.map(\.item.id) == ["a3", "a5", "a6"])
        #expect(route.stops.map(\.number) == [1, 2, 3])
        #expect(route.path == [techSquare, skyline, murals, MockPlaces.krogMarket.coordinate, home])
    }

    @Test func roundTripHasOneMarkerAndSkipsStopsWithoutALocation() {
        let route = SidequestRoute(itinerary: saturday)
        #expect(route.isRoundTrip)
        #expect(route.start == home)
        #expect(route.end == nil)
        // The sunset at Jackson Street Bridge has no location: stop 3 is left off the map, and
        // the others keep their numbers.
        #expect(route.stops.map(\.item.id) == ["b1", "b3"])
        #expect(route.stops.map(\.number) == [1, 2])
        #expect(route.path == [home, MockPlaces.stops["Atlanta Botanical Garden"]!.coordinate, MockPlaces.piedmont.coordinate, home])
    }

    @Test func routeSplitsWhereYouAre() {
        let route = SidequestRoute(itinerary: friday)
        let before = route.split(at: progress(friday, at(12)))
        #expect(before.travelled.isEmpty)
        #expect(before.ahead == route.path)
        // On the way to stop 1: solid up to it.
        let heading = route.split(at: progress(friday, at(14, 10)))
        #expect(heading.travelled == [techSquare, skyline])
        #expect(heading.ahead == Array(route.path[1...]))
        // At stop 2.
        #expect(route.split(at: progress(friday, at(17))).travelled == Array(route.path[...2]))
        let over = route.split(at: progress(friday, at(19, 30)))
        #expect(over.travelled == route.path)
        #expect(over.ahead.isEmpty)
        // Saturday evening, heading to the sunset (no location): solid through the last stop on the map.
        let round = SidequestRoute(itinerary: saturday)
        let sunset = round.split(at: progress(saturday, at(18, 30, day: 26)))
        #expect(sunset.travelled == Array(round.path[...2]))
        #expect(sunset.ahead == Array(round.path[2...]))
    }

    @Test func framingShowsTheWholeRoute() throws {
        let path = SidequestRoute(itinerary: friday).path
        let rect = try #require(SidequestRoute.frame(path))
        for c in path {
            let point = MKMapPoint(CLLocationCoordinate2D(latitude: c.lat, longitude: c.lng))
            // Inside, with room to spare.
            #expect(rect.insetBy(dx: rect.width * 0.1, dy: rect.height * 0.1).contains(point))
        }
        // A single point still gets a sane span.
        let one = try #require(SidequestRoute.frame([techSquare]))
        #expect(one.width / MKMapPointsPerMeterAtLatitude(techSquare.lat) >= 800)
        #expect(SidequestRoute.frame([]) == nil)
    }

    // MARK: Timeline | Map

    @Test func eachSidequestKeepsItsOwnMode() async throws {
        let env = AppEnvironment.preview()
        let store = HomeStore()
        await store.loadItineraries(env)
        // Timeline by default.
        #expect(!store.showsMap("itin-fri") && !store.showsMap("itin-sat"))
        store.setShowsMap(true, for: "itin-fri")
        #expect(store.showsMap("itin-fri") && !store.showsMap("itin-sat"))
        store.setShowsMap(true, for: "itin-sat")
        store.setShowsMap(false, for: "itin-fri")
        #expect(!store.showsMap("itin-fri") && store.showsMap("itin-sat"))

        // A sidequest taken off Home (deleted or left) drops its choice, even if it comes back.
        let removed = try #require(store.remove("itin-sat"))
        #expect(store.mapShown.isEmpty)
        store.restore(removed.itinerary, at: removed.index)
        #expect(!store.showsMap("itin-sat"))

        // So does one the server no longer lists; the others keep theirs.
        store.setShowsMap(true, for: "itin-fri")
        store.setShowsMap(true, for: "itin-sat")
        try await env.api.deleteItinerary(id: "itin-fri")
        await store.loadItineraries(env)
        #expect(store.mapShown == ["itin-sat"])
    }

    /// The offline demo stands at Tech Square with location allowed; it never asks.
    @Test func demoLocationIsTechSquare() {
        let feed = LocationFeed(isMock: true)
        #expect(feed.coordinate == techSquare)
        #expect(feed.isAllowed && !feed.canAsk)
        feed.start()
        feed.stop()
        #expect(feed.coordinate == techSquare)
    }
}
