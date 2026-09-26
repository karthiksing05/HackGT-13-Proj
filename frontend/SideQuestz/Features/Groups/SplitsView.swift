import SwiftUI

/// Thread › Splits (GUI_PLAN.md §7.9, equal splits only): your balance, per-person balances, the
/// expense list, "+ Add an expense" and "Settle up" with the default card. Balances and shares are
/// the server's (`GET /groups/{id}/expenses` + `/balances`).
struct GroupSplitsView: View {
    let thread: ChatThread
    /// Set by the `thread/…/splits/expense` launch route: open Add expense once the ledger loads.
    @Binding var openExpenseOnLoad: Bool

    @Environment(AppEnvironment.self) private var env
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    @State private var ledger: Loadable<GroupLedger> = .loading
    @State private var card: PaymentMethod?
    @State private var banner: String?
    @State private var showAddExpense = false
    /// What the Add expense sheet opens with (kept until the next open so the sheet can animate out).
    @State private var expenseRequest: ExpenseSheetRequest?
    @State private var settling = false
    @State private var settleError: String?
    @State private var addedCount = 0
    @State private var settledCount = 0

    private var meId: String { env.user?.id ?? "" }

    var body: some View {
        // Read here (not only inside the sheet closure) so the sheet is built with the current request.
        let request = expenseRequest
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                if let banner {
                    SuccessBanner(text: banner) { setBanner(nil) }
                        .transition(.opacity)
                }
                LoadableView(state: ledger, retry: { Task { await load(showLoading: true) } }) { ledger in
                    ledgerContent(ledger)
                }
            }
            .padding(.horizontal, 16)
            .padding(.top, 15)
            .padding(.bottom, 30)
        }
        .scrollIndicators(.hidden)
        .task {
            await load()
            if openExpenseOnLoad, ledger.value != nil {
                openExpenseOnLoad = false
                try? await Task.sleep(for: .milliseconds(300))
                let demo = UserDefaults.standard.bool(forKey: "SQExpenseDemo")
                openAddExpense(prefill: demo ? AddExpenseSheet.Prefill(what: "Pizza", amount: "40") : nil)
            }
        }
        .sqSheet(isPresented: $showAddExpense, style: .cream(.fromTop(60))) {
            if let request {
                AddExpenseSheet(groupId: thread.id, members: request.members, meId: meId, prefill: request.prefill,
                                onCancel: { showAddExpense = false }, onSaved: expenseAdded)
                    .id(request.id)
            }
        }
        .sensoryFeedback(.success, trigger: addedCount)
        .sensoryFeedback(.success, trigger: settledCount)
    }

    // MARK: Ledger

    private func ledgerContent(_ ledger: GroupLedger) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            balanceCard(ledger)
            Text("EXPENSES · SPLIT EQUALLY")
                .socialText(13, .semibold)
                .tracking(0.4)
                .foregroundStyle(Theme.text3)
                .padding(.top, 4)
                .accessibilityAddTraits(.isHeader)
            expenseList(ledger)
            Button("+ Add an expense") { openAddExpense(prefill: nil) }
                .buttonStyle(.sq(fill: Theme.sage, foreground: Theme.ink, height: 50, fontSize: 16))
                .accessibilityLabel("Add an expense")
            if ledger.netCents < 0 {
                settleButton(amount: -ledger.netCents)
            }
            if let settleError {
                Text(settleError)
                    .socialText(12)
                    .foregroundStyle(Theme.dangerText)
            }
        }
    }

    private func balanceCard(_ ledger: GroupLedger) -> some View {
        let net = ledger.netCents
        let headline = net < 0 ? "You owe \(Money.format(-net))" : net > 0 ? "You're owed \(Money.format(net))" : "All settled"
        let color = net < 0 ? Theme.danger : net > 0 ? Theme.success : Theme.ink
        let count = ledger.expenses.count
        return VStack(spacing: 12) {
            VStack(spacing: 2) {
                Text("Your balance in this group")
                    .socialText(13)
                    .foregroundStyle(Theme.text3)
                Text(headline)
                    .socialText(30, .bold)
                    .foregroundStyle(color)
                    .lineLimit(1)
                    .minimumScaleFactor(0.7)
                Text("\(count) \(count == 1 ? "expense" : "expenses") · all split equally")
                    .socialText(12)
                    .foregroundStyle(Theme.text3)
            }
            .frame(maxWidth: .infinity)
            .accessibilityElement(children: .combine)

            VStack(spacing: 0) {
                RowDivider(color: Theme.cream)
                ForEach(ledger.balances, id: \.userId) { balance in
                    balanceRow(balance, members: ledger.members)
                    RowDivider(color: Theme.cream)
                }
            }
        }
        .padding(16)
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
    }

    private func balanceRow(_ balance: Balance, members: [PersonRef]) -> some View {
        let person = members.first { $0.id == balance.userId }
        let name = person?.firstName ?? "Someone"
        let net = balance.netCents
        let text = net > 0 ? "\(name) owes you" : net < 0 ? "You owe \(name)" : "Settled with \(name)"
        let amount = net == 0 ? "—" : Money.format(abs(net))
        let color = net > 0 ? Theme.success : net < 0 ? Theme.danger : Theme.text3
        return HStack(spacing: 10) {
            if let person {
                Avatar(person: person, size: 30, fontSize: 11)
            }
            Text(text)
                .socialText(15)
                .foregroundStyle(Theme.ink)
                .frame(maxWidth: .infinity, alignment: .leading)
            Text(amount)
                .socialText(15, .bold)
                .foregroundStyle(color)
        }
        .padding(.vertical, 10)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(net == 0 ? text : "\(text) \(amount)")
    }

    private func expenseList(_ ledger: GroupLedger) -> some View {
        VStack(spacing: 0) {
            if ledger.expenses.isEmpty {
                Text("No expenses yet.")
                    .socialText(15)
                    .foregroundStyle(Theme.text3)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.vertical, 13)
                    .padding(.horizontal, 14)
            }
            // Newest first.
            ForEach(ledger.expenses.reversed()) { expense in
                expenseRow(expense, members: ledger.members)
                RowDivider(color: Theme.cream)
            }
        }
        .background(.white)
        .clipShape(RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
    }

    private func expenseRow(_ expense: Expense, members: [PersonRef]) -> some View {
        let people = expense.splitAmong.count
        // Shares are the server's; fall back to the same equal-split rule if they're missing.
        let shares = expense.shares.count == people
            ? expense.shares
            : SplitMath.equalShares(totalCents: expense.amountCents, count: people)
        return VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .top, spacing: 10) {
                VStack(alignment: .leading, spacing: 0) {
                    Text(expense.what)
                        .socialText(15, .semibold)
                        .foregroundStyle(Theme.ink)
                    Text("\(name(for: expense.payerId, in: members)) paid · split equally, \(people) \(people == 1 ? "person" : "people")")
                        .socialText(12)
                        .foregroundStyle(Theme.text3)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                Text(Money.format(expense.amountCents))
                    .socialText(16, .bold)
                    .foregroundStyle(Theme.ink)
            }
            FlowLayout(spacing: 6) {
                ForEach(Array(zip(expense.splitAmong, shares).enumerated()), id: \.offset) { _, pair in
                    let isMe = pair.0 == meId
                    SocialTag(text: "\(name(for: pair.0, in: members)) \(Money.format(pair.1))",
                              fill: isMe ? Theme.sageTint : Theme.cream,
                              foreground: isMe ? Theme.sageInk : Theme.text2)
                }
            }
        }
        .padding(.vertical, 13)
        .padding(.horizontal, 14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }

    private func name(for userId: String, in members: [PersonRef]) -> String {
        if userId == meId { return "You" }
        return members.first { $0.id == userId }?.firstName ?? "Someone"
    }

    private func settleButton(amount: Int) -> some View {
        let label = card.map { "Settle up \(Money.format(amount)) with \($0.brand) •••• \($0.last4)" }
            ?? "Settle up \(Money.format(amount))"
        return Button(action: settle) {
            HStack(spacing: 8) {
                if settling {
                    ProgressView().controlSize(.small).tint(.white)
                }
                Text(label)
            }
        }
        .buttonStyle(.sq(fill: Theme.ink, foreground: .white, height: 48, fontSize: 15))
        .disabled(settling)
        .accessibilityLabel(card.map { "Settle up \(Money.format(amount)) with \($0.brand) ending in \($0.last4)" } ?? label)
    }

    // MARK: Actions

    private func load(showLoading: Bool = false) async {
        if showLoading || ledger.value == nil { ledger = .loading }
        // The default card names the Settle up button; fetch it alongside the ledger.
        let cards = Task { try? await env.api.paymentMethods() }
        let result = await Loadable.run { try await env.api.ledger(groupId: thread.id) }
        if let methods = await cards.value { card = methods.first(where: \.isDefault) ?? methods.first }
        ledger = result
    }

    private func openAddExpense(prefill: AddExpenseSheet.Prefill?) {
        guard let members = ledger.value?.members else { return }
        expenseRequest = ExpenseSheetRequest(members: members, prefill: prefill)
        showAddExpense = true
    }

    private func expenseAdded(_ expense: Expense) {
        showAddExpense = false
        addedCount += 1
        let people = max(expense.splitAmong.count, 1)
        let each = expense.shares.min() ?? expense.amountCents / people
        let ways = people == 1 ? "1 way" : "\(people) ways"
        setBanner("Added \"\(expense.what)\" · \(Money.format(expense.amountCents)) split equally \(ways) (\(Money.format(each)) each). Everyone was notified.")
        Task { await load() }
    }

    private func settle() {
        settling = true
        settleError = nil
        Task {
            do {
                try await env.api.settleUp(groupId: thread.id)
                await load()
                settledCount += 1
            } catch {
                settleError = error.socialMessage
            }
            settling = false
        }
    }

    private func setBanner(_ text: String?) {
        if reduceMotion {
            banner = text
        } else {
            withAnimation(.easeInOut(duration: 0.2)) { banner = text }
        }
    }
}

/// One presentation of the Add expense sheet.
private struct ExpenseSheetRequest: Identifiable {
    let id = UUID()
    let members: [PersonRef]
    let prefill: AddExpenseSheet.Prefill?
}
