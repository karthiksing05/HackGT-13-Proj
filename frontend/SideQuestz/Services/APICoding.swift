import Foundation

/// JSON settings shared by REST (`LiveAPIClient`) and realtime (`WebSocketService`): snake_case keys,
/// ISO 8601 dates.
enum APICoding {
    static func encoder() -> JSONEncoder {
        let e = JSONEncoder()
        e.keyEncodingStrategy = .convertToSnakeCase
        e.dateEncodingStrategy = .iso8601
        return e
    }

    /// Accepts ISO 8601 with or without fractional seconds (`…T19:00:00.123456+00:00`, which the
    /// iOS 17 `.iso8601` strategy rejects) and date-only days (`"2026-09-25"`, read as midnight in
    /// the app's time zone).
    static func decoder(timeZone: TimeZone) -> JSONDecoder {
        let d = JSONDecoder()
        d.keyDecodingStrategy = .convertFromSnakeCase
        d.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let string = try container.decode(String.self)
            if let date = parse(string, timeZone: timeZone) { return date }
            throw DecodingError.dataCorruptedError(in: container, debugDescription: "Bad ISO 8601 date: \(string)")
        }
        return d
    }

    static func parse(_ string: String, timeZone: TimeZone) -> Date? {
        let withFraction = ISO8601DateFormatter()
        withFraction.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = withFraction.date(from: string) ?? ISO8601DateFormatter().date(from: string) { return date }
        let day = ISO8601DateFormatter()
        day.formatOptions = [.withFullDate]
        day.timeZone = timeZone
        return day.date(from: string)
    }
}
