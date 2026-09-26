import SwiftUI

/// Setup · 4 "Money preferences": spend (the default Create budget), flexibility, split style,
/// "Prefer free events", and an optional Visa card for tickets and splits.
struct SetupMoneyStep: View {
    @Environment(AppEnvironment.self) private var env
    @Bindable var draft: SetupDraft
    @State private var card: Loadable<PaymentMethod?> = .loading
    @State private var addingCard = false
    @State private var cardError: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            SetupHeading(title: "Money preferences",
                         subtitle: "So we suggest things you're comfortable paying for. You can change these anytime.")

            SetupEyebrow(text: "TYPICAL SPEND PER SIDEQUEST")
            SetupChoiceGrid(options: SpendTier.allCases, columns: 2, selection: $draft.preferences.spend, style: .grid) { $0.label }

            SetupEyebrow(text: "IF A GREAT OPTION COSTS A BIT MORE")
            SetupChoiceGrid(options: Flexibility.allCases, columns: 2, selection: $draft.preferences.flexibility, style: .grid) { $0.label }

            SetupEyebrow(text: "SPLITTING WITH A GROUP")
            SetupChoiceGrid(options: SplitStyle.allCases, columns: 3, selection: $draft.preferences.splitStyle, style: .grid) { $0.label }
            Text("Sets the default for new expenses in Groups › Splits.")
                .sqFont(12)
                .foregroundStyle(Theme.text3)
                .authLineHeight(1.35, size: 12)

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
                cardButton
            }
            if let cardError {
                AuthErrorText(message: cardError)
            }
            Text("The checkout agent always asks before it spends anything.")
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
        case .loading:
            ProgressView()
                .tint(Theme.sageInk)
                .frame(width: 44, height: 34)
        case .loaded(nil), .failed:
            Button(action: addCard) {
                ZStack {
                    Text("Add Visa card").opacity(addingCard ? 0 : 1)
                    if addingCard { ProgressView().controlSize(.small).tint(Theme.ink) }
                }
            }
            .buttonStyle(.sqPill(fill: Theme.sage, foreground: Theme.ink, horizontalPadding: 12))
            .authHitHeight(34)
            .disabled(addingCard)
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
        do {
            let methods = try await env.api.paymentMethods()
            card = .loaded(methods.first(where: \.isDefault) ?? methods.first)
        } catch {
            card = .failed(authMessage(for: error))
        }
    }

    private func addCard() {
        addingCard = true
        cardError = nil
        Task {
            defer { addingCard = false }
            do {
                card = .loaded(try await env.api.addPaymentMethod(token: "tok_visa"))
            } catch {
                cardError = authMessage(for: error, fallback: "Couldn't add the card. Try again.")
            }
        }
    }
}
