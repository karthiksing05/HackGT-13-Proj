import CoreGraphics
import Foundation

/// The mock "server" route engine (POST /plans/route in mock mode).
///
/// It reproduces the prototype's math so demo numbers match exactly: places sit on the prototype's
/// 350×220 map, 1 map point ≈ 0.0143 mi, and legs use the plan's rule — ≤ 0.8 mi walks, otherwise
/// Drive / Uber / MARTA depending on the ride answer.
enum MockRouteEngine {
    static let milesPerPoint = 0.0143

    /// Tech Square anchors the projection for dropped pins and searched places.
    private static let anchor = (coordinate: MockPlaces.techSquare.coordinate, point: MockPlaces.techSquare.mapPoint)
    private static let pointsPerDegreeLat = 4826.0
    private static let pointsPerDegreeLng = 4023.0

    static func mapPoint(for place: Place) -> CGPoint {
        if let known = MockPlaces.suggestions.first(where: { $0.label == place.name || $0.pillName == place.name }) {
            return known.mapPoint
        }
        if let stop = MockPlaces.stops[place.name] {
            return stop.point
        }
        if let c = place.coordinate {
            return CGPoint(
                x: anchor.point.x + (c.lng - anchor.coordinate.lng) * pointsPerDegreeLng,
                y: anchor.point.y - (c.lat - anchor.coordinate.lat) * pointsPerDegreeLat
            )
        }
        return CGPoint(x: 200, y: 100)
    }

    /// JavaScript's Math.round for positive numbers.
    private static func jsRound(_ x: Double) -> Int { Int((x + 0.5).rounded(.down)) }

    static func leg(from a: Place, to b: Place, ride: RideChoice) -> Leg {
        let p = mapPoint(for: a), q = mapPoint(for: b)
        let miles = hypot(p.x - q.x, p.y - q.y) * milesPerPoint
        if miles <= 0.8 { return Leg(mode: .walk, minutes: max(3, jsRound(miles * 20))) }
        switch ride {
        case .drive: return Leg(mode: .drive, minutes: jsRound(miles * 3 + 5))
        case .cover: return Leg(mode: .uber, minutes: jsRound(miles * 3 + 6))
        case .none: return Leg(mode: .marta, minutes: jsRound(miles * 4 + 8))
        }
    }

    /// Times every stop in order from `startTime` and flags lateness against `backBy`.
    static func route(stops: [PlanStop], start: Place, end: Place, startTime: Date, backBy: Date, ride: RideChoice) -> RouteResult {
        var legs: [Leg] = []
        var times: [DateInterval] = []
        var t = startTime
        var previous = start
        for stop in stops {
            let leg = leg(from: previous, to: stop.place, ride: ride)
            legs.append(leg)
            t = t.addingTimeInterval(TimeInterval(leg.minutes * 60))
            let stopEnd = t.addingTimeInterval(TimeInterval(stop.durationMinutes * 60))
            times.append(DateInterval(start: t, end: stopEnd))
            t = stopEnd
            previous = stop.place
        }
        let last = leg(from: previous, to: end, ride: ride)
        legs.append(last)
        t = t.addingTimeInterval(TimeInterval(last.minutes * 60))
        let late = Int((t.timeIntervalSince(backBy) / 60).rounded())
        // The demo's stops have no fixed starts, so no order ever breaks one (`brokenAt` stays -1).
        return RouteResult(legs: legs, stopTimes: times, arrival: t, minutesLate: max(0, late), brokenAt: -1)
    }
}
