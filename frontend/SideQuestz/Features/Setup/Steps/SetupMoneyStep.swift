import AuthenticationServices
import SwiftUI

/// Setup · 4 "Money preferences": spend (the default Create budget), flexibility, split style,
/// "Prefer free events", and an optional Visa card for tickets and splits.
///
/// The card is added on the backend's hosted card page (`PaymentMethodConnector`; a demo card in
/// mock mode), so card numbers never touch the app; the row then shows the saved card.
struct SetupMoneyStep: View {
    @Environment(AppEnvironment.self) private var env
    @Bindable var draft: SetupDraft
    @State private var card: Loadable<PaymentMethod?> = .loading
    @State private var addingCard = false
    @State private var cardError: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            SetupHeading(title: "Money preferences")

            SetupEyebrow(text: "TYPICAL SPEND PER SIDEQUEST")
            SetupChoiceGrid(options: SpendTier.allCases, columns: 2, selection: $draft.preferences.spend, style: .grid) { $0.label }

            SetupEyebrow(text: "IF A GREAT OPTION COSTS A BIT MORE")
            SetupChoiceGrid(options: Flexibility.allCases, columns: 2, selection: $draft.preferences.flexibility, style: .grid) { $0.label }

            SetupEyebrow(text: "SPLITTING WITH A GROUP")
            SetupChoiceGrid(options: SplitStyle.allCases, columns: 3, selection: $draft.preferences.splitStyle, style: .grid) { $0.label }

            freeEventsRow
            cardRow
        }
        .task { await loadCard() }
    }

    private var freeEventsRow: some View {
        HStack(spacing: 10) {
            VStack(alignment: .leading, spacing: 0) {
                Text("Prefer free events")
                    .sqFont(15, .semibold)
                    .foregroundStyle(Theme.ink)
                    .authLineHeight(1.35, size: 15)
                Text("Show free options first when they fit")
                    .sqFont(12)
                    .foregroundStyle(Theme.text3)
                    .authLineHeight(1.35, size: 12)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityHidden(true)
            SQToggle(isOn: $draft.preferences.preferFree, label: "Prefer free events")
                .authHitHeight(32)
        }
        .padding(.vertical, 12)
        .padding(.horizontal, 14)
        .background(.white, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
    }

    private var cardRow: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 10) {
                VStack(alignment: .leading, spacing: 0) {
                    Text("Card for tickets and splits")
                        .sqFont(15, .semibold)
                        .foregroundStyle(Theme.ink)
                        .authLineHeight(1.35, size: 15)
                    Text(cardSubtitle)
                        .sqFont(12)
                        .foregroundStyle(Theme.text3)
                        .authLineHeight(1.35, size: 12)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .accessibilityElement(children: .combine)
                // One slot: the states cross-fade in place instead of sitting side by side.
                ZStack(alignment: .trailing) { cardButton }
            }
            if let cardError {
                AuthErrorText(message: cardError)
                    .sqTransition(.rise)
            }
            Text("The agent asks before it buys anything, unless you turn on instant checkout.")
                .sqFont(12)
                .foregroundStyle(Theme.text3)
                .authLineHeight(1.35, size: 12)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.vertical, 12)
        .padding(.horizontal, 14)
        .background(.white, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
    }

    @ViewBuilder private var cardButton: some View {
        switch card {
        case .loaded(let method?):
            Text("Added")
                .sqFont(14, .semibold)
                .foregroundStyle(Theme.success)
                .padding(.horizontal, 12)
                .frame(height: 34)
                .background(Theme.successBg, in: Capsule())
                .accessibilityLabel("\(method.brand) card added")
                .sqTransition(.pop)
        case .loading:
            // Shaped like the "Add Visa card" pill while we check for a saved card.
            SkeletonBlock(width: 112, height: 34, radius: 17)
                .sqShimmer()
                .accessibilityElement()
                .accessibilityLabel("Loading")
                .transition(.opacity)
        case .loaded(nil), .failed:
            Button(action: addCard) {
                ZStack {
                    Text("Add Visa card").opacity(addingCard ? 0 : 1)
                    if addingCard {
                        LoadingDots(color: Theme.ink, dotSize: 5)
                            .sqTransition(.pop)
                    }
                }
            }
            .buttonStyle(.sqPill(fill: Theme.sage, foreground: Theme.ink, horizontalPadding: 12))
            .authHitHeight(34)
            .disabled(addingCard)
            .authMotion(Motion.quick, value: addingCard)
            .accessibilityLabel("Add Visa card")
            .accessibilityValue(addingCard ? "In progress" : "")
            .transition(.opacity)
        }
    }

    private var cardSubtitle: String {
        if case .loaded(let method?) = card {
            return "\(method.brand) •••• \(method.last4)\(method.isDefault ? " · default" : "")"
        }
        return "Optional · add later in Account"
    }

    private func loadCard() async {
        // Only the add button depends on this; a failed lookup just offers "Add Visa card".
        let result: Loadable<PaymentMethod?>
        do {
            let methods = try await env.api.paymentMethods()
            result = .loaded(methods.first(where: \.isDefault) ?? methods.first)
        } catch {
            result = .failed(authMessage(for: error))
        }
        withMotion { card = result }
    }

    private func addCard() {
        withMotion(Motion.quick) {
            addingCard = true
            cardError = nil
        }
        Task {
            do {
                let methods = try await PaymentMethodConnector.addCard(env: env)
                guard let method = methods.first(where: \.isDefault) ?? methods.first else {
                    // Came back from the card page without a saved card.
                    withMotion {
                        addingCard = false
                        cardError = "No card was added. Try again."
                    }
                    return
                }
                // "Added" pops in where the button was; the subtitle cross-fades to the card.
                withMotion(Motion.arrive) {
                    card = .loaded(method)
                    addingCard = false
                }
            } catch let error as ASWebAuthenticationSessionError where error.code == .canceledLogin {
                // Closed the card page: nothing changed.
                withMotion(Motion.quick) { addingCard = false }
            } catch {
                withMotion {
                    addingCard = false
                    cardError = authMessage(for: error, fallback: "Couldn't add the card. Try again.")
                }
            }
        }
    }
}
