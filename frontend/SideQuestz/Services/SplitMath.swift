import Foundation

/// Equal-split math. The server is authoritative (POST /groups/{id}/expenses); the client runs the
/// same numbers for the Add-expense preview so the preview always adds up to the total, to the cent.
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

    /// Net balance of each other member relative to `me` (+ = they owe me, − = I owe them).
    static func balances(expenses: [Expense], me: String, members: [String]) -> [Balance] {
        var net: [String: Int] = [:]
        for member in members where member != me { net[member] = 0 }
        for expense in expenses {
            let shares = expense.shares.count == expense.splitAmong.count
                ? expense.shares
                : equalShares(totalCents: expense.amountCents, count: expense.splitAmong.count)
            for (i, member) in expense.splitAmong.enumerated() where member != expense.payerId {
                if expense.payerId == me {
                    net[member, default: 0] += shares[i]
                } else if member == me {
                    net[expense.payerId, default: 0] -= shares[i]
                }
            }
        }
        return members.filter { $0 != me }.map { Balance(userId: $0, netCents: net[$0] ?? 0) }
    }
}
