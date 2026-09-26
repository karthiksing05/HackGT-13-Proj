import SwiftUI

extension Color {
    /// `Color(hex: 0x899E88)`, or with alpha: `Color(hex: 0x899E88, opacity: 0.2)`.
    init(hex: UInt32, opacity: Double = 1) {
        self.init(
            .sRGB,
            red: Double((hex >> 16) & 0xFF) / 255,
            green: Double((hex >> 8) & 0xFF) / 255,
            blue: Double(hex & 0xFF) / 255,
            opacity: opacity
        )
    }

    /// Parses "#RRGGBB" or "#RRGGBBAA" (as used by the prototype and the API for avatar colors).
    init?(hexString: String) {
        var s = hexString.trimmingCharacters(in: .whitespaces)
        if s.hasPrefix("#") { s.removeFirst() }
        guard s.count == 6 || s.count == 8, let value = UInt64(s, radix: 16) else { return nil }
        if s.count == 8 {
            self.init(hex: UInt32(value >> 8), opacity: Double(value & 0xFF) / 255)
        } else {
            self.init(hex: UInt32(value))
        }
    }
}

/// Color tokens from GUI_PLAN.md §4.1 and the prototype.
///
/// Hard rule: never put white text on `sage`. Text on sage is `ink`; sage-colored text on
/// cream or white uses `sageInk`.
enum Theme {
    // Brand
    static let sage = Color(hex: 0x899E88)
    static let sageInk = Color(hex: 0x4A5F49)
    static let ink = Color(hex: 0x18211C)
    static let cream = Color(hex: 0xF2F1EA)
    static let white = Color.white
    /// `#899E88` at 20% — selected-soft chips, sidequest blocks, info boxes.
    static let sageTint = Color(hex: 0x899E88, opacity: 0.2)
    /// `#899E88` at 40% — sidequest block border.
    static let sageBorder = Color(hex: 0x899E88, opacity: 0.4)

    // Groups / people only
    static let clay = Color(hex: 0xC77B58)
    static let clayTint = Color(hex: 0xF7E9E0)
    static let clayText = Color(hex: 0x6B3419)
    static let clayBorder = Color(hex: 0xE0B49B)

    // Transit, "Free now"
    static let transitBg = Color(hex: 0xEEF3FD)
    static let transitText = Color(hex: 0x1E3A8A)
    static let transitBorder = Color(hex: 0x9DB6EE)

    // Calendar / busy blocks
    static let busyBg = Color(hex: 0xEFEFF4)

    // Text
    static let text2 = Color(hex: 0x3A433D)
    static let text3 = Color(hex: 0x626A63)

    // Lines and surfaces
    static let line = Color(hex: 0xE3E4DC)
    static let lineStrong = Color(hex: 0xCFD2C8)
    /// Calendar day-panel hour rules.
    static let hourLine = Color(hex: 0xECEDE6)
    static let field = Color(hex: 0xF8F7F2)
    static let segmentBg = Color(hex: 0xE4E3DA)

    // Muted
    static let mutedIcon = Color(hex: 0x858D87)
    static let mutedStar = Color(hex: 0xA9AFA6)
    static let mutedBorder = Color(hex: 0xC3C7BD)

    // Feedback
    static let danger = Color(hex: 0xB91C1C)
    static let dangerBg = Color(hex: 0xFDECEC)
    static let dangerText = Color(hex: 0x991B1B)
    /// Field border after a failed submit.
    static let errorBorder = Color(hex: 0xE5A3A3)
    static let success = Color(hex: 0x166534)
    static let successBg = Color(hex: 0xE6F4EA)
    static let successBorder = Color(hex: 0x9CCFAA)
    static let successBannerText = Color(hex: 0x14532D)

    // Presence status dots
    static let statusOpen = Color(hex: 0x16A34A)
    static let statusOnline = Color(hex: 0x1D4ED8)
    static let statusNotFree = Color(hex: 0x858D87)

    // Misc
    static let visaNavy = Color(hex: 0x1A1F71)
    static let receiptRule = Color(hex: 0xD8D8DE)
    static let mapBackground = Color(hex: 0xE8ECE4)
    static let mapPark = Color(hex: 0xCFE3C8)
    static let dim = Color.black.opacity(0.35)

    // Loading placeholders
    /// Skeleton bars on white cards.
    static let skeleton = Color(hex: 0xECECE5)
    /// Skeleton bars directly on the cream background.
    static let skeletonOnCream = Color(hex: 0xE4E4DB)
}

/// Visual treatment of each timeline block kind (prototype `KIND` table).
struct BlockPalette {
    let background: Color
    let border: Color
    let text: Color
    let label: String
    let dashed: Bool
    /// Calendar-chip dot color for this kind.
    let dot: Color
}

extension BlockKind {
    var palette: BlockPalette {
        switch self {
        case .busy:
            BlockPalette(background: Theme.busyBg, border: Theme.lineStrong, text: Theme.text2, label: "Calendar", dashed: false, dot: Theme.mutedIcon)
        case .sidequest:
            BlockPalette(background: Theme.sageTint, border: Theme.sageBorder, text: Theme.ink, label: "Sidequest", dashed: false, dot: Theme.sage)
        case .transit:
            BlockPalette(background: Theme.transitBg, border: Theme.transitBorder, text: Theme.transitText, label: "Transit", dashed: true, dot: Theme.transitBorder)
        case .group:
            BlockPalette(background: Theme.clayTint, border: Theme.clayBorder, text: Theme.clayText, label: "Group", dashed: false, dot: Theme.clay)
        }
    }
}
