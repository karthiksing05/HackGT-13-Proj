import CoreGraphics
import Foundation

/// The demo "server" behind Review › hold a stop › Swap: a small catalog of Atlanta spots by kind,
/// and the demo's idea of "similar" (same kind, not already in the plan, nearest first). The real
/// server and the ML stack decide similarity for real.
enum MockAlternatives {
    enum Kind: CaseIterable {
        case views, art, food, park, games, books

        /// "Also …" in the reason line.
        var shared: String {
            switch self {
            case .views: "great views"
            case .art: "art to see"
            case .food: "good food"
            case .park: "time outside"
            case .games: "games"
            case .books: "books to browse"
            }
        }

        fileprivate var keywords: [String] {
            switch self {
            case .views: ["view", "rooftop", "ferris", "skyline", "overlook"]
            case .art: ["art", "mural", "galler", "museum"]
            case .food: ["food", "taco", "dinner", "market", "sandwich", "bbq"]
            case .park: ["park", "walk", "garden", "trail", "loop"]
            case .games: ["game", "arcade", "golf"]
            case .books: ["book"]
            }
        }
    }

    struct Spot {
        let title: String
        let subtitle: String
        let kind: Kind
        let minutes: Int
        let coordinate: Coordinate
    }

    static let catalog: [Spot] = [
        Spot(title: "Jackson Street Bridge", subtitle: "Skyline views · Free", kind: .views, minutes: 30,
             coordinate: Coordinate(lat: 33.7596, lng: -84.3721)),
        Spot(title: "Sun Dial rooftop", subtitle: "Views + drinks · $$", kind: .views, minutes: 60,
             coordinate: Coordinate(lat: 33.7596, lng: -84.3880)),
        Spot(title: "Bellwood Quarry overlook", subtitle: "Views + trails · Free", kind: .views, minutes: 70,
             coordinate: Coordinate(lat: 33.7890, lng: -84.4480)),
        Spot(title: "Freedom Park bridge", subtitle: "Skyline peek · Free", kind: .views, minutes: 25,
             coordinate: Coordinate(lat: 33.7700, lng: -84.3530)),
        Spot(title: "Cabbagetown murals", subtitle: "Street art · Free", kind: .art, minutes: 45,
             coordinate: Coordinate(lat: 33.7507, lng: -84.3610)),
        Spot(title: "Atlanta Contemporary", subtitle: "Art gallery · Free", kind: .art, minutes: 60,
             coordinate: Coordinate(lat: 33.7839, lng: -84.4153)),
        Spot(title: "High Museum of Art", subtitle: "Museum · $$", kind: .art, minutes: 90,
             coordinate: Coordinate(lat: 33.7901, lng: -84.3856)),
        Spot(title: "Castleberry Hill galleries", subtitle: "Galleries · Free", kind: .art, minutes: 50,
             coordinate: Coordinate(lat: 33.7487, lng: -84.4007)),
        Spot(title: "Sweet Auburn Curb Market", subtitle: "Food market · $", kind: .food, minutes: 40,
             coordinate: Coordinate(lat: 33.7545, lng: -84.3810)),
        Spot(title: "Politan Row", subtitle: "Food hall · $", kind: .food, minutes: 40,
             coordinate: Coordinate(lat: 33.7870, lng: -84.3839)),
        Spot(title: "Victory Sandwich Bar", subtitle: "Sandwiches · $", kind: .food, minutes: 35,
             coordinate: Coordinate(lat: 33.7530, lng: -84.3515)),
        Spot(title: "Fox Bros BBQ", subtitle: "BBQ · $$", kind: .food, minutes: 50,
             coordinate: Coordinate(lat: 33.7641, lng: -84.3491)),
        Spot(title: "Freedom Park trail", subtitle: "Walk · Free", kind: .park, minutes: 50,
             coordinate: Coordinate(lat: 33.7690, lng: -84.3540)),
        Spot(title: "Grant Park loop", subtitle: "Walk · Free", kind: .park, minutes: 60,
             coordinate: Coordinate(lat: 33.7370, lng: -84.3700)),
        Spot(title: "BeltLine Eastside Trail", subtitle: "Walk · Free", kind: .park, minutes: 45,
             coordinate: Coordinate(lat: 33.7650, lng: -84.3615)),
        Spot(title: "Historic Fourth Ward Park", subtitle: "Park · Free", kind: .park, minutes: 45,
             coordinate: Coordinate(lat: 33.7667, lng: -84.3639)),
        Spot(title: "The Painted Duck", subtitle: "Games + bar · $$", kind: .games, minutes: 70,
             coordinate: Coordinate(lat: 33.7870, lng: -84.4115)),
        Spot(title: "Joystick Gamebar", subtitle: "Arcade · $", kind: .games, minutes: 60,
             coordinate: Coordinate(lat: 33.7540, lng: -84.3730)),
        Spot(title: "Puttshack", subtitle: "Mini golf · $$", kind: .games, minutes: 60,
             coordinate: Coordinate(lat: 33.7852, lng: -84.4121)),
        Spot(title: "Board game café", subtitle: "Games · $", kind: .games, minutes: 60,
             coordinate: Coordinate(lat: 33.7770, lng: -84.3880)),
        Spot(title: "A Cappella Books", subtitle: "Books · Free", kind: .books, minutes: 40,
             coordinate: Coordinate(lat: 33.7640, lng: -84.3490)),
        Spot(title: "Little Shop of Stories", subtitle: "Books · Free", kind: .books, minutes: 35,
             coordinate: Coordinate(lat: 33.7740, lng: -84.2960)),
    ]

    /// What a stop is, from its subtitle ("Games + views · $" → views, games), else its title
    /// ("Open group dinner (Forum)" → food).
    static func kinds(of stop: PlanStop) -> [Kind] {
        let fromSubtitle = matches(stop.subtitle.lowercased())
        return fromSubtitle.isEmpty ? matches(stop.title.lowercased()) : fromSubtitle
    }

    private static func matches(_ text: String) -> [Kind] {
        Kind.allCases
            .compactMap { kind in kind.keywords.compactMap { text.range(of: $0)?.lowerBound }.min().map { (kind, $0) } }
            .sorted { $0.1 < $1.1 }
            .map(\.0)
    }

    /// Up to `limit` spots like `stop`, nearest first, leaving out anything already in the plan.
    static func alternatives(for stop: PlanStop, excluding titles: Set<String>, limit: Int = 4) -> [PlanAlternative] {
        let kinds = kinds(of: stop)
        let from = MockRouteEngine.mapPoint(for: stop.place)
        return catalog
            .filter { kinds.contains($0.kind) && !titles.contains($0.title) && $0.title != stop.title }
            .map { spot -> (spot: Spot, miles: Double) in
                let to = MockRouteEngine.mapPoint(for: Place(name: spot.title, coordinate: spot.coordinate))
                return (spot, hypot(from.x - to.x, from.y - to.y) * MockRouteEngine.milesPerPoint)
            }
            .sorted { $0.miles < $1.miles }
            .prefix(limit)
            .map { spot, miles in
                PlanAlternative(
                    stop: PlanStop(id: id(for: spot.title), title: spot.title, subtitle: spot.subtitle,
                                   place: Place(name: spot.title, coordinate: spot.coordinate), durationMinutes: spot.minutes),
                    reason: "Also \(spot.kind.shared) · \(String(format: "%.1f", max(miles, 0.1))) mi away"
                )
            }
    }

    /// Stable ids, so the same spot is the same stop wherever it's suggested.
    static func id(for title: String) -> String {
        "alt-" + title.lowercased().map { $0.isLetter || $0.isNumber ? String($0) : "-" }.joined()
    }
}
