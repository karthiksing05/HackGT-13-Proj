import AuthenticationServices
import SwiftUI

/// Account › Payments: your saved cards (the default marked), "Add a card", and Instant checkout with
/// its spending limit.
///
/// Cards are added on the backend's hosted card page (`PaymentMethodConnector`; a demo card in mock
/// mode), so card numbers never touch the app. Removing a card (after confirming) and the instant
/// checkout settings are optimistic: they change at once and go back with a note if the server says
/// no. Instant checkout is saved in the preferences (`PUT /me/preferences`) and needs a card.
struct AccountPaymentsCard: View {
    @Environment(AppEnvironment.self) private var env
    let model: AccountMeModel
    /// Skeletons only shimmer while Account is the visible tab.
    var shimmers = true

    @State private var adding = false
    @State private var confirmRemove: PaymentMethod?
    @State private var errorText: String?
    /// Rows rise in one after another when the cards first load while this card exists.
    @State private var animateRows: Bool
    /// The instant checkout setting the server last confirmed, kept while saves are in flight (where a
    /// failed save rolls back to).
    @State private var confirmedInstant: AccountInstantSetting?
    @State private var latestSave = 0
    @State private var savingInstant = false

    init(model: AccountMeModel, shimmers: Bool = true) {
        self.model = model
        self.shimmers = shimmers
        _animateRows = State(initialValue: model.cards.value == nil)
    }

    /// Spending limits to pick from (cents); the saved one is added if it's something else.
    private static let limits = [2500, 5000, 10000]

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            SetupCard {
                AuthLoadable(state: model.cards, shimmers: shimmers, retry: { Task { await model.loadCards(env) } }) {
                    AccountPaymentsSkeleton()
                } content: { cards in
                    VStack(spacing: 0) {
                        ForEach(Array(cards.enumerated()), id: \.element.id) { index, card in
                            // The divider comes and goes with its card.
                            VStack(spacing: 0) {
                                cardRow(card)
                                RowDivider()
                            }
                            .authArrive(index, animated: animateRows)
                            .sqTransition(.rise)
                        }
                        addCardRow
                        RowDivider()
                        instantCheckout(hasCard: !cards.isEmpty)
                    }
                    .onAppear { animateRows = false }
                }
            }
            if let errorText {
                AuthErrorText(message: errorText)
                    .padding(.horizontal, 4)
                    .sqTransition(.rise)
            }
        }
        .confirmationDialog(removeTitle, isPresented: removeShown, titleVisibility: .visible, presenting: confirmRemove) { card in
            Button("Remove card", role: .destructive) { remove(card) }
        } message: { _ in
            if removingLastCardWithInstant {
                Text("Instant checkout stops working until you add another card.")
            }
        }
    }

    // MARK: Cards

    /// [VISA] •••• 4242  Default ··· (•••)
    private func cardRow(_ card: PaymentMethod) -> some View {
        HStack(spacing: 10) {
            HStack(spacing: 10) {
                AccountCardBrandBadge(brand: card.brand)
                Text("•••• \(card.last4)")
                    .sqFont(15)
                    .foregroundStyle(Theme.ink)
                    .authLineHeight(1.35, size: 15)
                if card.isDefault {
                    TagLabel(text: "Default", fill: Theme.sageTint, foreground: Theme.sageInk)
                        .transition(.opacity)
                }
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("\(card.brand) ending in \(card.last4)\(card.isDefault ? ", default" : "")")
            .accessibilityAction(named: "Remove card") { confirmRemove = card }
            Spacer(minLength: 8)
            Menu {
                Button(role: .destructive) { confirmRemove = card } label: {
                    Label("Remove card", systemImage: "trash")
                }
            } label: {
                Image(systemName: "ellipsis")
                    .font(.system(size: 14, weight: .bold))
                    .foregroundStyle(Theme.sageInk)
                    .frame(width: 30, height: 30)
                    .background(Theme.cream, in: Circle())
                    .frame(width: Metrics.minTouch, height: Metrics.minTouch)
                    .contentShape(Circle())
            }
            .padding(.vertical, -7)
            .padding(.trailing, -7)
            .accessibilityLabel("Options for \(card.brand) ending in \(card.last4)")
        }
        .padding(.vertical, 13)
        .padding(.horizontal, 14)
    }

    /// "+ Add a card" in sageInk; dots while the card page is open.
    private var addCardRow: some View {
        Button(action: addCard) {
            HStack(spacing: 10) {
                Image(systemName: "plus")
                    .font(.system(size: 13, weight: .bold))
                    .foregroundStyle(Theme.sageInk)
                    .frame(width: 36, height: 24)
                    .background(Theme.sageTint, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
                ZStack(alignment: .leading) {
                    Text("Add a card")
                        .sqFont(15, .semibold)
                        .foregroundStyle(Theme.sageInk)
                        .opacity(adding ? 0 : 1)
                    if adding {
                        LoadingDots(color: Theme.sageInk, dotSize: 5)
                            .sqTransition(.pop)
                    }
                }
                .authLineHeight(1.35, size: 15)
                Spacer(minLength: 0)
            }
            .padding(.vertical, 13)
            .padding(.horizontal, 14)
            .contentShape(Rectangle())
        }
        .buttonStyle(.sqPressable)
        .disabled(adding)
        .authMotion(Motion.quick, value: adding)
        .accessibilityLabel("Add a card")
        .accessibilityValue(adding ? "In progress" : "")
    }

    // MARK: Agentic checkout (stored as the instant checkout preference)

    /// Toggle + one line on what it does; the limit chips grow in underneath while it's on.
    @ViewBuilder private func instantCheckout(hasCard: Bool) -> some View {
        let prefs = env.preferences
        let usable = hasCard && prefs != nil
        let on = usable && (prefs?.instantCheckout ?? false)
        let limit = prefs?.instantCheckoutLimitCents ?? 5000
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 10) {
                VStack(alignment: .leading, spacing: 0) {
                    Text("Agentic checkout")
                        .sqFont(15, .semibold)
                        .foregroundStyle(Theme.ink)
                        .authLineHeight(1.35, size: 15)
                    Text(instantSubtitle(hasCard: hasCard, loaded: prefs != nil, limit: limit))
                        .sqFont(12)
                        .foregroundStyle(Theme.text3)
                        .authLineHeight(1.35, size: 12)
                        .fixedSize(horizontal: false, vertical: true)
                        .contentTransition(.opacity)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .accessibilityHidden(true)
                SQToggle(isOn: Binding(get: { on }, set: { save(AccountInstantSetting(on: $0, limitCents: limit)) }),
                         label: "Agentic checkout")
                    .authHitHeight(32)
                    .disabled(!usable)
                    .opacity(usable ? (savingInstant ? 0.6 : 1) : 0.45)
                    .accessibilityHint(instantSubtitle(hasCard: hasCard, loaded: prefs != nil, limit: limit))
            }
            if on {
                limitChips(selected: limit)
                    .sqTransition(.rise)
            }
        }
        .padding(.vertical, 12)
        .padding(.horizontal, 14)
        .authMotion(value: on)
    }

    private func instantSubtitle(hasCard: Bool, loaded: Bool, limit: Int) -> String {
        if !hasCard { return "Add a card to use agentic checkout." }
        if !loaded { return "Couldn't load this setting. Pull down to try again." }
        return "After you save a plan, Muse can buy its tickets (Face ID, default budget \(Money.compact(limit))). Sandbox: no real payment is made."
    }

    /// "Default budget  $25  $50  $100"
    private func limitChips(selected: Int) -> some View {
        let options = Array(Set(Self.limits + [selected])).sorted()
        return HStack(spacing: 8) {
            Text("Default budget")
                .sqFont(13)
                .foregroundStyle(Theme.text3)
                .accessibilityHidden(true)
            ForEach(options, id: \.self) { cents in
                SQChip(label: Money.compact(cents), isOn: cents == selected, style: .tag) {
                    guard cents != selected else { return }
                    save(AccountInstantSetting(on: true, limitCents: cents))
                }
                .accessibilityLabel("Default budget \(Money.compact(cents))")
            }
            Spacer(minLength: 0)
        }
        .opacity(savingInstant ? 0.6 : 1)
    }

    // MARK: Actions

    private func addCard() {
        withMotion(Motion.quick) {
            adding = true
            errorText = nil
        }
        Task {
            do {
                let cards = try await PaymentMethodConnector.addCard(env: env)
                // The new card rises into the list; the toggle comes alive with the first card.
                withMotion(Motion.arrive) {
                    model.cards = .loaded(cards.filter { !model.removingCards.contains($0.id) })
                    adding = false
                }
            } catch let error as ASWebAuthenticationSessionError where error.code == .canceledLogin {
                // Closed the card page: nothing changed.
                withMotion(Motion.quick) { adding = false }
            } catch {
                withMotion {
                    adding = false
                    errorText = authMessage(for: error, fallback: "Couldn't add the card. Try again.")
                }
            }
        }
    }

    /// Optimistic: the row fades out, then the list reloads (the server may pick a new default);
    /// the card comes back with a note if removing it fails.
    private func remove(_ card: PaymentMethod) {
        guard case .loaded(var list) = model.cards, let index = list.firstIndex(where: { $0.id == card.id }) else { return }
        list.remove(at: index)
        withMotion(Motion.arrive) {
            model.cards = .loaded(list)
            model.removingCards.insert(card.id)
            errorText = nil
        }
        Task {
            do {
                try await env.api.deletePaymentMethod(id: card.id)
                model.removingCards.remove(card.id)
                await model.loadCards(env)
            } catch {
                withMotion(Motion.arrive) {
                    model.removingCards.remove(card.id)
                    if case .loaded(var current) = model.cards, !current.contains(where: { $0.id == card.id }) {
                        current.insert(card, at: min(index, current.count))
                        model.cards = .loaded(current)
                    }
                    errorText = authMessage(for: error, fallback: "Couldn't remove the card. Try again.")
                }
            }
        }
    }

    /// Optimistic: the toggle or chip changes at once (a little faint until the server confirms)
    /// and goes back with a note if saving fails. Only the newest change's answer settles it.
    private func save(_ setting: AccountInstantSetting) {
        guard var prefs = env.preferences else { return }
        if confirmedInstant == nil { confirmedInstant = AccountInstantSetting(prefs) }
        latestSave += 1
        let request = latestSave
        prefs.instantCheckout = setting.on
        prefs.instantCheckoutLimitCents = setting.limitCents
        withMotion(Motion.quick) {
            env.preferences = prefs
            savingInstant = true
            errorText = nil
        }
        Task {
            do {
                try await env.api.savePreferences(prefs)
                confirmedInstant = setting
                guard request == latestSave else { return }
                withMotion(Motion.quick) {
                    savingInstant = false
                    confirmedInstant = nil
                }
            } catch {
                guard request == latestSave else { return }
                withMotion {
                    if let confirmed = confirmedInstant {
                        env.preferences?.instantCheckout = confirmed.on
                        env.preferences?.instantCheckoutLimitCents = confirmed.limitCents
                    }
                    savingInstant = false
                    confirmedInstant = nil
                    errorText = authMessage(for: error, fallback: "Couldn't save agentic checkout. Try again.")
                }
            }
        }
    }

    // MARK: Confirm remove

    private var removeTitle: String {
        confirmRemove.map { "Remove \($0.brand) •••• \($0.last4)?" } ?? ""
    }

    private var removeShown: Binding<Bool> {
        Binding(get: { confirmRemove != nil }, set: { if !$0 { confirmRemove = nil } })
    }

    private var removingLastCardWithInstant: Bool {
        (model.cards.value?.count ?? 0) <= 1 && (env.preferences?.instantCheckout ?? false)
    }
}

/// The instant checkout part of the preferences.
private struct AccountInstantSetting: Equatable {
    var on: Bool
    var limitCents: Int

    init(on: Bool, limitCents: Int) {
        self.on = on
        self.limitCents = limitCents
    }

    init(_ preferences: Preferences) {
        on = preferences.instantCheckout
        limitCents = preferences.instantCheckoutLimitCents
    }
}

/// "VISA" in white on navy (like Checkout's card row); other brands as a short mark on ink.
private struct AccountCardBrandBadge: View {
    let brand: String

    /// Fits the 36pt badge: "VISA", "MC", "AMEX", "DISC", else the first four letters.
    private var mark: String {
        switch brand.lowercased().filter(\.isLetter) {
        case "visa": "VISA"
        case "mastercard", "mc": "MC"
        case "americanexpress", "amex": "AMEX"
        case "discover": "DISC"
        default: String(brand.uppercased().filter(\.isLetter).prefix(4))
        }
    }

    var body: some View {
        Text(mark)
            .sqFont(10, .heavy)
            .tracking(0.8)
            .foregroundStyle(.white)
            .lineLimit(1)
            .minimumScaleFactor(0.7)
            .frame(width: 36, height: 24)
            .background(brand.lowercased() == "visa" ? Theme.visaNavy : Theme.ink,
                        in: RoundedRectangle(cornerRadius: 6, style: .continuous))
            .accessibilityHidden(true)
    }
}

/// A card row, the add row and the instant checkout row, shaped like the real ones.
private struct AccountPaymentsSkeleton: View {
    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 10) {
                SkeletonBlock(width: 36, height: 24, radius: 6)
                SkeletonBlock(width: 64, height: 12)
                SkeletonBlock(width: 52, height: 18, radius: 8)
                Spacer(minLength: 8)
                Circle().fill(Theme.skeleton).frame(width: 30, height: 30)
            }
            .frame(height: 15 * 1.35)
            .padding(.vertical, 13)
            .padding(.horizontal, 14)
            RowDivider()
            HStack(spacing: 10) {
                SkeletonBlock(width: 36, height: 24, radius: 6)
                SkeletonBlock(width: 84, height: 12)
                Spacer(minLength: 0)
            }
            .frame(height: 15 * 1.35)
            .padding(.vertical, 13)
            .padding(.horizontal, 14)
            RowDivider()
            HStack(spacing: 10) {
                VStack(alignment: .leading, spacing: 8) {
                    SkeletonBlock(width: 118, height: 12)
                    SkeletonBlock(width: 210, height: 10)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                SkeletonBlock(width: 52, height: 32, radius: 16)
            }
            .padding(.vertical, 12)
            .padding(.horizontal, 14)
        }
    }
}
