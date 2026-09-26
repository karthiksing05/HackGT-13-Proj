import SwiftUI

/// Splits › "+ Add an expense" (GUI_PLAN.md §7.9). Equal split only. "Check the split" previews
/// the server's math on the client (`SplitMath`) so it always adds up to the cent ("Split equally ·
/// $13.33 each" on its first line); saving posts the expense and the server's result is what the
/// Splits tab shows.
///
/// Motion: the preview's amounts roll as you type, people slide in and out of the split, and the
/// "Add $…" button shows loading dots while the server saves.
///
/// Present with `.sqSheet(isPresented:, style: .cream(.fromTop(60)))`.
struct AddExpenseSheet: View {
    struct Prefill: Equatable {
        var what: String
        var amount: String
    }

    let groupId: String
    let members: [PersonRef]
    let meId: String
    let onCancel: () -> Void
    let onSaved: (Expense) -> Void

    @Environment(AppEnvironment.self) private var env

    @State private var what: String
    @State private var amount: String
    @State private var payerId: String
    @State private var splitIds: Set<String>
    /// Errors only show after a failed save.
    @State private var tried = false
    @State private var saving = false
    @State private var serverError: String?
    @FocusState private var focus: Field?

    private enum Field { case what, amount }

    init(groupId: String, members: [PersonRef], meId: String, prefill: Prefill? = nil,
         onCancel: @escaping () -> Void, onSaved: @escaping (Expense) -> Void) {
        self.groupId = groupId
        self.members = members
        self.meId = meId
        self.onCancel = onCancel
        self.onSaved = onSaved
        _what = State(initialValue: prefill?.what ?? "")
        _amount = State(initialValue: prefill?.amount ?? "")
        _payerId = State(initialValue: members.contains { $0.id == meId } ? meId : members.first?.id ?? meId)
        _splitIds = State(initialValue: Set(members.map(\.id)))
    }

    // MARK: Form state

    private var cents: Int { Money.parseCents(amount) }
    /// Split members in group order (the first ones pay the extra cent).
    private var splitMembers: [PersonRef] { members.filter { splitIds.contains($0.id) } }
    private var shares: [Int] { SplitMath.equalShares(totalCents: cents, count: splitMembers.count) }

    private var problems: [String] {
        var list: [String] = []
        if what.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { list.append("Add what the expense was for.") }
        if cents <= 0 { list.append("Enter an amount above $0.") }
        if splitMembers.isEmpty { list.append("Pick at least one person to split with.") }
        return list
    }

    private var isValid: Bool { problems.isEmpty }

    private var shownErrors: [String] {
        (tried && !isValid ? problems : []) + (serverError.map { [$0] } ?? [])
    }

    private func name(_ person: PersonRef) -> String { person.socialName(meId: meId) }

    // MARK: Body

    var body: some View {
        SheetScaffold(spacing: 12, grabberColor: Theme.mutedBorder) {
            header
            whatField
            amountField
            sectionLabel("PAID BY")
            FlowLayout(spacing: 8) {
                ForEach(members) { member in
                    SQChip(label: name(member), isOn: payerId == member.id, style: .socialCompactGrid) {
                        payerId = member.id
                    }
                }
            }
            sectionLabel("SPLIT BETWEEN")
            FlowLayout(spacing: 8) {
                ForEach(members) { member in
                    SQChip(label: name(member), isOn: splitIds.contains(member.id), style: .socialSoft, showsCheck: true) {
                        if splitIds.contains(member.id) { splitIds.remove(member.id) } else { splitIds.insert(member.id) }
                    }
                }
            }
            checkCard
            if !shownErrors.isEmpty {
                ErrorBox(messages: shownErrors)
                    .sqTransition(.rise)
            }
            saveButton
        }
        .onChange(of: amount) { _, newValue in
            let cleaned = Self.sanitizedAmount(newValue)
            if cleaned != newValue { amount = cleaned }
        }
    }

    private var header: some View {
        HStack(spacing: 0) {
            Button(action: onCancel) {
                Text("Cancel")
                    .sqFont(16)
                    .foregroundStyle(Theme.sageInk)
                    .frame(height: 36)
                    .contentShape(Rectangle().inset(by: -6))
            }
            .buttonStyle(.plain)
            .frame(width: 80, alignment: .leading)
            Text("Add an expense")
                .socialText(17, .bold)
                .foregroundStyle(Theme.ink)
                .frame(maxWidth: .infinity)
                .accessibilityAddTraits(.isHeader)
            Color.clear.frame(width: 80, height: 1)
        }
        .frame(height: 36)
    }

    private var whatField: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text("What was it for?")
                .socialText(12)
                .foregroundStyle(Theme.text3)
                .accessibilityHidden(true)
            TextField("", text: $what, prompt: Text("e.g. Pizza, tickets, rideshare").foregroundStyle(Theme.text3))
                .socialText(17)
                .foregroundStyle(Theme.ink)
                .textInputAutocapitalization(.sentences)
                .submitLabel(.next)
                .focused($focus, equals: .what)
                .onSubmit { focus = .amount }
                .accessibilityLabel("What was it for?")
        }
        .expenseFieldCard()
    }

    private var amountField: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text("Total amount")
                .socialText(12)
                .foregroundStyle(Theme.text3)
                .accessibilityHidden(true)
            HStack(alignment: .firstTextBaseline, spacing: 2) {
                Text("$").accessibilityHidden(true)
                TextField("", text: $amount, prompt: Text("0.00").foregroundStyle(Theme.text3))
                    .keyboardType(.decimalPad)
                    .focused($focus, equals: .amount)
                    .accessibilityLabel("Total amount in dollars")
            }
            .socialText(26, .bold)
            .foregroundStyle(Theme.ink)
        }
        .expenseFieldCard()
    }

    private func sectionLabel(_ text: String) -> some View {
        Text(text)
            .socialText(13, .semibold)
            .tracking(0.4)
            .foregroundStyle(Theme.text3)
            .accessibilityAddTraits(.isHeader)
    }

    // MARK: Check the split

    private var checkCard: some View {
        let people = splitMembers
        let total = shares.reduce(0, +)
        let extra = SplitMath.extraCentCount(totalCents: cents, count: people.count)
        let balanced = total == cents && cents > 0
        return VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline) {
                Text("CHECK THE SPLIT")
                    .socialText(12, .bold)
                    .tracking(0.4)
                    .foregroundStyle(Theme.text3)
                Spacer(minLength: 8)
                Text(!people.isEmpty && cents > 0 ? "Split equally · \(Money.format(cents / people.count)) each" : "Split equally")
                    .socialText(13, .semibold)
                    .foregroundStyle(Theme.ink)
                    .lineLimit(1)
                    .minimumScaleFactor(0.8)
                    .sqNumeric()
            }
            ForEach(Array(people.enumerated()), id: \.element.id) { index, person in
                HStack(spacing: 10) {
                    Avatar(person: person, size: 26, fontSize: 10)
                        .accessibilityHidden(true)
                    Text(shareLabel(for: person))
                        .socialText(14)
                        .foregroundStyle(Theme.ink)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    Text(Money.format(index < shares.count ? shares[index] : 0))
                        .socialText(14, .bold)
                        .foregroundStyle(Theme.ink)
                        .sqNumeric()
                }
                .accessibilityElement(children: .combine)
                .sqTransition(.rise)
            }
            if extra > 0 {
                Text("Rounded to the cent: the first \(extra == 1 ? "person pays" : "\(extra) people pay") 1¢ more so it adds up exactly.")
                    .socialText(12)
                    .foregroundStyle(Theme.text3)
                    .fixedSize(horizontal: false, vertical: true)
                    .transition(.opacity)
            }
            HStack {
                Text("Shares add up to")
                Spacer(minLength: 8)
                Text("\(Money.format(total)) of \(Money.format(cents))")
                    .foregroundStyle(balanced ? Theme.success : Theme.text3)
                    .sqNumeric()
            }
            .socialText(13, .semibold)
            .foregroundStyle(Theme.ink)
            .padding(.top, 6)
            .overlay(alignment: .top) { RowDivider(color: Theme.cream) }
            .accessibilityElement(children: .combine)
        }
        .padding(14)
        .background(.white, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 14, style: .continuous)
                .strokeBorder(isValid ? Theme.successBorder : Theme.line, lineWidth: 1.5)
        }
        // Amounts roll as you type; people slide in and out of the split.
        .animation(Motion.quick, value: cents)
        .animation(Motion.standard, value: splitIds)
        .animation(Motion.standard, value: payerId)
        .animation(Motion.quick, value: isValid)
    }

    /// "Your share (you paid)", "Maya owes you", "You owe Dev", "Dev's share (paid)", "Maya owes Dev".
    private func shareLabel(for person: PersonRef) -> String {
        let payerName = members.first { $0.id == payerId }.map(name) ?? "Someone"
        if person.id == payerId {
            return person.id == meId ? "Your share (you paid)" : "\(name(person))'s share (paid)"
        }
        if person.id == meId { return "You owe \(payerName)" }
        return "\(name(person)) owes \(payerId == meId ? "you" : payerName)"
    }

    // MARK: Save

    private var saveButton: some View {
        let count = splitMembers.count
        let label = isValid ? "Add \(Money.format(cents)) · split \(count) \(count == 1 ? "way" : "ways")" : "Add expense"
        return Button(action: save) {
            SocialBusyLabel(isBusy: saving, color: Theme.ink) {
                Text(label).sqNumeric()
            }
        }
        .buttonStyle(.sq(fill: isValid ? Theme.sage : Theme.mutedStar, foreground: Theme.ink))
        .disabled(saving)
        .animation(Motion.quick, value: cents)
        .animation(Motion.quick, value: count)
        .animation(Motion.quick, value: isValid)
        .accessibilityLabel(label)
        .accessibilityValue(saving ? "Loading" : "")
    }

    private func save() {
        guard isValid else {
            withMotion(Motion.quick) { tried = true }
            return
        }
        focus = nil
        saving = true
        if serverError != nil { withMotion(Motion.quick) { serverError = nil } }
        let expense = NewExpense(what: what.trimmingCharacters(in: .whitespacesAndNewlines), amountCents: cents,
                                 payerId: payerId, splitAmong: splitMembers.map(\.id))
        Task {
            do {
                let saved = try await env.api.addExpense(groupId: groupId, expense)
                onSaved(saved)
            } catch {
                withMotion { serverError = error.socialMessage }
            }
            saving = false
        }
    }

    // MARK: Helpers

    /// Digits and one decimal point, at most two decimals ("12.345" → "12.34").
    static func sanitizedAmount(_ text: String) -> String {
        var result = ""
        var seenPoint = false
        var decimals = 0
        for character in text {
            if character.isASCII, character.isNumber {
                if seenPoint {
                    guard decimals < 2 else { continue }
                    decimals += 1
                }
                result.append(character)
            } else if character == "." || character == ",", !seenPoint {
                seenPoint = true
                result.append(".")
            }
        }
        return result
    }
}

private extension View {
    /// White field card: padding 10×14, radius 14 (What was it for? / Total amount).
    func expenseFieldCard() -> some View {
        padding(.horizontal, 14)
            .padding(.vertical, 10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.white, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
    }
}
