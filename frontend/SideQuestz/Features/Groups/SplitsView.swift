import SwiftUI

/// Thread › Splits (GUI_PLAN.md §7.9, equal splits only): your balance, per-person balances, the
/// expense list, "+ Add an expense" and "Settle up" with the default card. Balances and shares are
/// the server's (`GET /groups/{id}/expenses` + `/balances`).
///
/// Loading: a skeleton of the balance card and expenses, then the rows arrive one after another.
/// After adding an expense its row rises in right away (the server returned it) while the balance
/// card waits, dimmed, for the server's new balances; then the amounts roll to the new values.
/// Expenses other people add (`expense.added` over the socket) arrive the same way.
///
/// Deleting: long-press an expense you added (or use the VoiceOver action) → confirm → the row
/// fades out and the balances refresh from the server; if the server says no, the row comes back
/// with a note. "Settle up" pays the amount on the button; if the balance changed meanwhile the
/// server refuses (409), its sentence shows and the new amount loads.
struct GroupSplitsView: View {
    let groupId: String
    /// Set by the `thread/…/splits/expense` launch route: open Add expense once the ledger loads.
    @Binding var openExpenseOnLoad: Bool

    @Environment(AppEnvironment.self) private var env

    @State private var ledger: Loadable<GroupLedger> = .loading
    @State private var card: PaymentMethod?
    @State private var banner: String?
    /// The server is recomputing balances after a change (the balance card dims until they're back).
    @State private var balancesRefreshing = false
    /// Expenses from the load that replaced the skeleton: only these arrive one after another.
    @State private var arrivingIds: Set<String> = []
    @State private var showAddExpense = false
    /// What the Add expense sheet opens with (kept until the next open so the sheet can animate out).
    @State private var expenseRequest: ExpenseSheetRequest?
    @State private var settling = false
    @State private var settleError: String?
    /// The expense waiting for "Delete expense" to be confirmed.
    @State private var confirmingDelete: Expense?
    @State private var deleteError: String?
    @State private var addedCount = 0
    @State private var settledCount = 0
    /// A member (from the balances) whose profile is open.
    @State private var profileRoute: PersonProfileRoute?

    private static let top = "splits.top"
    private var meId: String { env.user?.id ?? "" }

    var body: some View {
        // Read here (not only inside the sheet closure) so the sheet is built with the current request.
        let request = expenseRequest
        ScrollViewReader { proxy in
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    if let banner {
                        SuccessBanner(text: banner) { setBanner(nil) }
                            .sqTransition(.banner)
                    }
                    content
                }
                // Lined up with the header's Chat | Album | Splits control.
                .padding(.horizontal, Metrics.side)
                .padding(.top, 15)
                .padding(.bottom, 30)
                .id(Self.top)
            }
            .scrollIndicators(.hidden)
            .sqPullToRefresh()
            // A new banner scrolls into view if you were further down (Settle up is at the bottom).
            .onChange(of: banner) { _, text in
                if text != nil { withMotion(Motion.gentle) { proxy.scrollTo(Self.top, anchor: .top) } }
            }
        }
        .task {
            await load()
            if openExpenseOnLoad, ledger.value != nil {
                openExpenseOnLoad = false
                try? await Task.sleep(for: .milliseconds(300))
                let demo = UserDefaults.standard.bool(forKey: "SQExpenseDemo")
                openAddExpense(prefill: demo ? AddExpenseSheet.Prefill(what: "Pizza", amount: "40") : nil)
            }
        }
        .task { await listenForExpenses() }
        .sqReloadable("thread.\(groupId).splits") { await reload() }
        .confirmationDialog(confirmingDelete.map { "Delete \"\($0.what)\"?" } ?? "",
                            isPresented: Binding(get: { confirmingDelete != nil }, set: { if !$0 { confirmingDelete = nil } }),
                            titleVisibility: .visible, presenting: confirmingDelete) { expense in
            Button("Delete expense", role: .destructive) { delete(expense) }
        } message: { _ in
            Text("It comes off everyone's balances.")
        }
        .sqSheet(isPresented: $showAddExpense, style: .cream(.fromTop(60))) {
            if let request {
                AddExpenseSheet(groupId: groupId, members: request.members, meId: meId, prefill: request.prefill,
                                onCancel: { showAddExpense = false }, onSaved: expenseAdded)
                    .id(request.id)
            }
        }
        .sensoryFeedback(.success, trigger: addedCount)
        .sensoryFeedback(.success, trigger: settledCount)
        .personProfileSheet($profileRoute)
    }

    private var content: some View {
        ZStack(alignment: .top) {
            switch ledger {
            case .loading:
                SplitsSkeleton()
                    .sqSlowLoading()
                    .transition(.opacity)
            case .failed(let message):
                ErrorStateView(message: message) { Task { await retryLoad() } }
                    .transition(.opacity)
            case .loaded(let ledger):
                ledgerContent(ledger)
                    .transition(.opacity)
            }
        }
        .animation(Motion.standard, value: ledger.phase)
    }

    // MARK: Ledger

    private func ledgerContent(_ ledger: GroupLedger) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            balanceCard(ledger)
                .sqRefreshing(balancesRefreshing)
            Text("EXPENSES · SPLIT EQUALLY")
                .socialText(13, .semibold)
                .tracking(0.4)
                .foregroundStyle(Theme.text3)
                .padding(.top, 4)
                .accessibilityAddTraits(.isHeader)
            expenseList(ledger)
            if let deleteError {
                Text(deleteError)
                    .socialText(12)
                    .foregroundStyle(Theme.dangerText)
                    .sqTransition(.rise)
            }
            Button("+ Add an expense") { openAddExpense(prefill: nil) }
                .buttonStyle(.sq(fill: Theme.sage, foreground: Theme.ink, height: 50, fontSize: 16))
                .accessibilityLabel("Add an expense")
            if ledger.owedCents > 0 {
                settleButton(amount: ledger.owedCents)
                    .transition(.opacity)
            }
            if let settleError {
                Text(settleError)
                    .socialText(12)
                    .foregroundStyle(Theme.dangerText)
                    .sqTransition(.rise)
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
                    .sqNumeric()
                Text("\(count) \(count == 1 ? "expense" : "expenses")")
                    .socialText(12)
                    .foregroundStyle(Theme.text3)
                    .sqNumeric()
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

    /// A member and where you stand with them; tap for their profile.
    private func balanceRow(_ balance: Balance, members: [PersonRef]) -> some View {
        let person = members.first { $0.id == balance.userId }
        let name = person?.firstName ?? "Someone"
        let net = balance.netCents
        let text = net > 0 ? "\(name) owes you" : net < 0 ? "You owe \(name)" : "Settled with \(name)"
        let amount = net == 0 ? "—" : Money.format(abs(net))
        let color = net > 0 ? Theme.success : net < 0 ? Theme.danger : Theme.text3
        return Button {
            if let person { profileRoute = PersonProfileRoute(person) }
        } label: {
            HStack(spacing: 10) {
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
                    .sqNumeric()
            }
            .padding(.vertical, 10)
        }
        .buttonStyle(.sqPressable)
        .disabled(person == nil)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(net == 0 ? text : "\(text) \(amount)")
        .accessibilityHint(person == nil ? "" : "Opens their profile")
        .accessibilityAddTraits(person == nil ? [] : .isButton)
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
                    .transition(.opacity)
            }
            // Newest first; a new one rises in at the top.
            ForEach(Array(ledger.expenses.reversed().enumerated()), id: \.element.id) { index, expense in
                VStack(spacing: 0) {
                    if isOwn(expense) {
                        expenseRow(expense, members: ledger.members)
                            .contentShape(.contextMenuPreview, RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
                            .contextMenu {
                                Button(role: .destructive) { confirmingDelete = expense } label: {
                                    Label("Delete expense", systemImage: "trash")
                                }
                            }
                            .accessibilityAction(named: "Delete expense") { confirmingDelete = expense }
                    } else {
                        expenseRow(expense, members: ledger.members)
                    }
                    RowDivider(color: Theme.cream)
                }
                .socialArrival(index, staggered: arrivingIds.contains(expense.id))
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
                    Text("\(name(for: expense.payerId, in: members)) paid · split \(people == 1 ? "1 way" : "\(people) ways")")
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
        .background(.white)
        .accessibilityElement(children: .combine)
    }

    /// Expenses you added (the payer, when the server doesn't say who added it).
    private func isOwn(_ expense: Expense) -> Bool {
        !meId.isEmpty && (expense.createdBy ?? expense.payerId) == meId
    }

    private func name(for userId: String, in members: [PersonRef]) -> String {
        if userId == meId { return "You" }
        return members.first { $0.id == userId }?.firstName ?? "Someone"
    }

    private func settleButton(amount: Int) -> some View {
        let label = card.map { "Settle up \(Money.format(amount)) with \($0.brand) •••• \($0.last4)" }
            ?? "Settle up \(Money.format(amount))"
        return Button(action: settle) {
            SocialBusyLabel(isBusy: settling, color: .white) {
                Text(label).sqNumeric()
            }
        }
        .buttonStyle(.sq(fill: Theme.ink, foreground: .white, height: 48, fontSize: 15))
        .disabled(settling || balancesRefreshing)
        .opacity(balancesRefreshing && !settling ? 0.5 : 1)
        .animation(Motion.standard, value: balancesRefreshing)
        .accessibilityLabel(card.map { "Settle up \(Money.format(amount)) with \($0.brand) ending in \($0.last4)" } ?? label)
        .accessibilityValue(settling ? "Loading" : "")
    }

    // MARK: Loading

    private func load() async {
        // The default card names the Settle up button; fetch it alongside the ledger.
        let cards = Task { try? await env.api.paymentMethods() }
        let result = await Loadable.run { try await env.api.ledger(groupId: groupId) }
        if let methods = await cards.value { card = methods.first(where: \.isDefault) ?? methods.first }
        show(result)
    }

    private func retryLoad() async {
        withMotion { ledger = .loading }
        await load()
    }

    /// Pull to refresh: the ledger stays on screen and the amounts roll to the fresh ones; a failed
    /// call keeps what's there.
    private func reload() async {
        let cards = Task { try? await env.api.paymentMethods() }
        let fresh = try? await env.api.ledger(groupId: groupId)
        if let methods = await cards.value {
            withMotion { card = methods.first(where: \.isDefault) ?? methods.first }
        }
        if let fresh { show(.loaded(fresh)) }
    }

    private func show(_ result: Loadable<GroupLedger>) {
        let fromSkeleton = ledger.value == nil
        arrivingIds = fromSkeleton ? Set(result.value?.expenses.map(\.id) ?? []) : []
        withMotion(fromSkeleton ? Motion.standard : Motion.arrive) {
            ledger = result
            balancesRefreshing = false
        }
    }

    // MARK: Actions

    private func openAddExpense(prefill: AddExpenseSheet.Prefill?) {
        guard let members = ledger.value?.members else { return }
        expenseRequest = ExpenseSheetRequest(members: members, prefill: prefill)
        showAddExpense = true
    }

    /// The server saved it: its row rises in now; the balances follow when the server has them.
    private func expenseAdded(_ expense: Expense) {
        showAddExpense = false
        addedCount += 1
        let people = max(expense.splitAmong.count, 1)
        let each = expense.shares.min() ?? expense.amountCents / people
        let ways = people == 1 ? "1 way" : "\(people) ways"
        withMotion(Motion.arrive) {
            if var current = ledger.value, !current.expenses.contains(where: { $0.id == expense.id }) {
                current.expenses.append(expense)
                ledger = .loaded(current)
                balancesRefreshing = true
            }
            banner = "Added \"\(expense.what)\" · \(Money.format(expense.amountCents)) split equally \(ways) (\(Money.format(each)) each). Everyone was notified."
        }
        Task { await refreshBalances() }
    }

    private func refreshBalances() async {
        if let fresh = try? await env.api.ledger(groupId: groupId) {
            show(.loaded(fresh))
        } else {
            withMotion { balancesRefreshing = false }
        }
    }

    private func settle() {
        guard !settling, let owed = ledger.value.map(\.owedCents), owed > 0 else { return }
        settling = true
        if settleError != nil { withMotion(Motion.quick) { settleError = nil } }
        let method = card
        Task {
            do {
                // The amount the button showed; the server refuses it if the balance changed meanwhile.
                try await env.api.settleUp(groupId: groupId, amountCents: owed, paymentMethodId: method?.id)
                // Settled: confirm now; the balances follow when the server has them (the button
                // keeps its dots until then, and fades away once nothing is owed).
                settledCount += 1
                withMotion(Motion.arrive) {
                    balancesRefreshing = true
                    banner = "Settled up \(Money.format(owed))\(method.map { " with \($0.brand) •••• \($0.last4)" } ?? "")."
                }
                await refreshBalances()
                withMotion { settling = false }
            } catch {
                // 409: the balance changed since the button was drawn. Say so and load the new one.
                let stale = error.socialConflictMessage
                withMotion {
                    settleError = stale ?? error.socialMessage
                    settling = false
                    if stale != nil { balancesRefreshing = true }
                }
                if stale != nil { await refreshBalances() }
            }
        }
    }

    /// Confirmed: the row fades out now and the balances follow from the server; a refusal puts the
    /// row back with the server's reason.
    private func delete(_ expense: Expense) {
        confirmingDelete = nil
        guard var current = ledger.value, let index = current.expenses.firstIndex(where: { $0.id == expense.id }) else { return }
        current.expenses.remove(at: index)
        withMotion {
            ledger = .loaded(current)
            balancesRefreshing = true
            deleteError = nil
        }
        Task {
            do {
                try await env.api.deleteExpense(groupId: groupId, expenseId: expense.id)
                await refreshBalances()
            } catch {
                withMotion(Motion.arrive) {
                    if var restored = ledger.value, !restored.expenses.contains(where: { $0.id == expense.id }) {
                        restored.expenses.insert(expense, at: min(index, restored.expenses.count))
                        ledger = .loaded(restored)
                    }
                    balancesRefreshing = false
                    deleteError = error.socialMessage
                }
            }
        }
    }

    /// `expense.added` for this group (someone else's): its row rises in and the balances refresh.
    private func listenForExpenses() async {
        for await event in env.realtime.subscribe() {
            guard case .expenseAdded(let id, let expense) = event, id == groupId,
                  var current = ledger.value, !current.expenses.contains(where: { $0.id == expense.id }) else { continue }
            current.expenses.append(expense)
            withMotion(Motion.arrive) {
                ledger = .loaded(current)
                balancesRefreshing = true
            }
            Task { await refreshBalances() }
        }
    }

    private func setBanner(_ text: String?) {
        withMotion(text == nil ? Motion.standard : Motion.arrive) { banner = text }
    }
}

/// One presentation of the Add expense sheet.
private struct ExpenseSheetRequest: Identifiable {
    let id = UUID()
    let members: [PersonRef]
    let prefill: AddExpenseSheet.Prefill?
}

/// First-load placeholder: the balance card (headline and two people) and two expense rows with
/// their share chips, shimmering. Reads "Loading".
private struct SplitsSkeleton: View {
    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            VStack(spacing: 12) {
                VStack(spacing: 10) {
                    SkeletonBlock(width: 160, height: 11)
                    SkeletonBlock(width: 200, height: 30, radius: 8)
                    SkeletonBlock(width: 150, height: 10)
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, 2)
                VStack(spacing: 0) {
                    RowDivider(color: Theme.cream)
                    ForEach(0..<2, id: \.self) { index in
                        HStack(spacing: 10) {
                            Circle().fill(Theme.skeleton).frame(width: 30, height: 30)
                            SkeletonBlock(width: index == 0 ? 112 : 92, height: 12)
                            Spacer(minLength: 0)
                            SkeletonBlock(width: 52, height: 12)
                        }
                        .padding(.vertical, 10)
                        RowDivider(color: Theme.cream)
                    }
                }
            }
            .padding(16)
            .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))

            SkeletonBlock(width: 176, height: 11, color: Theme.skeletonOnCream)
                .padding(.vertical, 5)
                .padding(.top, 4)

            VStack(spacing: 0) {
                ForEach(0..<2, id: \.self) { index in
                    VStack(alignment: .leading, spacing: 9) {
                        HStack {
                            SkeletonBlock(width: index == 0 ? 110 : 96, height: 14)
                            Spacer(minLength: 0)
                            SkeletonBlock(width: 58, height: 14)
                        }
                        SkeletonBlock(width: 186, height: 10)
                        HStack(spacing: 6) {
                            ForEach(0..<3, id: \.self) { _ in
                                SkeletonBlock(width: index == 0 ? 68 : 76, height: 22, radius: 8)
                            }
                        }
                        .padding(.top, 2)
                    }
                    .padding(.vertical, 13)
                    .padding(.horizontal, 14)
                    RowDivider(color: Theme.cream)
                }
            }
            .background(.white)
            .clipShape(RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
        }
        .sqShimmer()
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading")
    }
}
