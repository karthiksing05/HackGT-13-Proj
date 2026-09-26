import Foundation

/// Equal-split math for the Add-expense preview, so the preview always adds up to the total, to the
/// cent. The server is authoritative (POST /groups/{id}/expenses) and computes balances; the mock
/// backend's version of that lives in `Mock/MockLedger.swift`.
enum SplitMath {
    /// `base = floor(cents / n)`, and the first `cents mod n` people pay 1¢ more.
    static func equalShares(totalCents: Int, count: Int) -> [Int] {
        guard count > 0, totalCents >= 0 else { return [] }
        let base = totalCents / count
        let extra = totalCents - base * count
        return (0..<count).map { base + ($0 < extra ? 1 : 0) }
    }

    /// How many people pay the extra cent.
    static func extraCentCount(totalCents: Int, count: Int) -> Int {
        guard count > 0 else { return 0 }
        return totalCents - (totalCents / count) * count
    }
}
