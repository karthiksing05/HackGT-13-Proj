import Foundation

/// The mock backend's ledger math: what `GET /groups/{id}/balances` returns. The real server owns this.
extension SplitMath {
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
