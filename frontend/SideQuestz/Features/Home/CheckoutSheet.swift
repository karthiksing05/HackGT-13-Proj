import SwiftUI

/// Agent checkout (Visa), on top of the Event sheet (GUI_PLAN.md §7.6). The agent prepares the
/// purchase (`POST /checkout/intents`); nothing is spent until "Approve purchase" (`…/approve`).
/// × (or closing the sheet) cancels a pending intent.
struct HomeCheckoutSheet: View {
    let item: ItineraryItem
    let close: () -> Void
    /// The ticket was booked.
    let booked: () -> Void

    @Environment(AppEnvironment.self) private var env
    @State private var intent: Loadable<CheckoutIntent> = .loading
    @State private var isApproving = false
    @State private var approveError: String?
    @State private var bookings = 0

    var body: some View {
        SheetScaffold {
            if let current = intent.value, current.state == .booked {
                bookedContent
            } else {
                header
                switch intent {
                case .loading:
                    LoadingStateView(label: "Getting your tickets ready…", minHeight: 280)
                case .failed(let message):
                    ErrorStateView(message: message, minHeight: 280) { Task { await start() } }
                case .loaded(let current):
                    details(current)
                }
            }
        }
        .task { await start() }
        .onDisappear(perform: cancelIfPending)
        .sensoryFeedback(.success, trigger: bookings)
    }

    private var header: some View {
        HStack(spacing: 8) {
            Text("Agent checkout")
                .sqFont(22, .bold, relativeTo: .title2)
                .foregroundStyle(Theme.ink)
                .homeLine(22)
                .accessibilityAddTraits(.isHeader)
            Spacer(minLength: 0)
            CloseCircleButton(label: "Cancel checkout", action: close)
                .padding(-6)
        }
    }

    // MARK: Awaiting approval

    @ViewBuilder
    private func details(_ current: CheckoutIntent) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(Array(current.steps.enumerated()), id: \.offset) { _, step in
                stepRow(step)
            }
        }
        receipt(current)
        cardRow(current)
        Button(action: approve) {
            HStack(spacing: 8) {
                if isApproving { ProgressView().tint(.white) }
                Text("Approve purchase")
            }
        }
        .buttonStyle(.sqDark)
        .disabled(isApproving)
        if let approveError {
            Text(approveError)
                .sqFont(13)
                .foregroundStyle(Theme.danger)
                .frame(maxWidth: .infinity)
                .multilineTextAlignment(.center)
        }
        Text("The agent can't spend anything until you approve.")
            .sqFont(12, relativeTo: .caption)
            .foregroundStyle(Theme.text3)
            .homeLine(12)
            .multilineTextAlignment(.center)
            .frame(maxWidth: .infinity)
    }

    private func stepRow(_ step: CheckoutStep) -> some View {
        HStack(spacing: 10) {
            CheckGlyph(lineWidth: 3)
                .foregroundStyle(.white)
                .frame(width: 12, height: 12)
                .frame(width: 20, height: 20)
                .background(step.done ? Theme.success : Theme.mutedStar, in: Circle())
            Text(step.text)
                .sqFont(14)
                .foregroundStyle(step.done ? Theme.ink : Theme.text3)
                .homeLine(14)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(step.done ? "\(step.text), done" : step.text)
    }

    private func receipt(_ current: CheckoutIntent) -> some View {
        VStack(spacing: 6) {
            receiptLine("1 × \(current.itemTitle)", Money.orPlaceholder(current.subtotalCents))
            receiptLine("Fees", Money.orPlaceholder(current.feesCents, placeholder: "[fees]"))
                .foregroundStyle(Theme.text3)
            receiptLine("Total", Money.orPlaceholder(current.totalCents, placeholder: "[total]"))
                .fontWeight(.bold)
                .padding(.top, 6)
                .overlay(alignment: .top) { Rectangle().fill(Theme.receiptRule).frame(height: 1) }
        }
        .sqFont(15)
        .foregroundStyle(Theme.ink)
        .padding(.vertical, 12)
        .padding(.horizontal, 14)
        .background(Theme.cream, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
    }

    private func receiptLine(_ label: String, _ amount: String) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(label).lineLimit(2)
            Spacer(minLength: 8)
            Text(amount)
        }
        .homeLine(15)
        .accessibilityElement(children: .combine)
    }

    private func cardRow(_ current: CheckoutIntent) -> some View {
        HStack(spacing: 10) {
            Text(current.cardBrand.uppercased())
                .sqFont(12, .heavy)
                .tracking(1)
                .foregroundStyle(.white)
                .padding(.horizontal, 8)
                .padding(.vertical, 4)
                .background(Theme.visaNavy, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
            Text("•••• \(current.cardLast4)")
                .sqFont(15)
                .foregroundStyle(Theme.ink)
                .homeLine(15)
            Spacer(minLength: 8)
            Text("Change")
                .sqFont(13)
                .foregroundStyle(Theme.text3)
        }
        // 1pt border + the prototype's 12 / 14 padding.
        .padding(.vertical, 13)
        .padding(.horizontal, 15)
        .overlay { RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Theme.line, lineWidth: 1) }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Pay with \(current.cardBrand) ending in \(current.cardLast4)")
    }

    // MARK: Booked

    @ViewBuilder
    private var bookedContent: some View {
        VStack(spacing: 10) {
            CheckGlyph(lineWidth: 2.4)
                .foregroundStyle(.white)
                .frame(width: 32, height: 32)
                .frame(width: 64, height: 64)
                .background(Theme.success, in: Circle())
            Text("Booked")
                .sqFont(22, .bold, relativeTo: .title2)
                .foregroundStyle(Theme.ink)
                .homeLine(22)
                .accessibilityAddTraits(.isHeader)
            Text("Your ticket is saved to this itinerary. The group sees it too.")
                .sqFont(15)
                .foregroundStyle(Theme.text2)
                .homeLine(15)
                .multilineTextAlignment(.center)
                .frame(maxWidth: 280)
        }
        .frame(maxWidth: .infinity)
        .padding(.top, 20)
        .padding(.bottom, 8)
        Button("Done", action: close)
            .buttonStyle(.sqPrimary)
    }

    // MARK: Actions

    private func start() async {
        intent = .loading
        intent = await Loadable.run { try await env.api.createCheckoutIntent(itemId: item.id, quantity: 1) }
    }

    private func approve() {
        guard let current = intent.value, !isApproving else { return }
        isApproving = true
        approveError = nil
        Task {
            do {
                let updated = try await env.api.approveCheckout(id: current.id)
                withAnimation(.easeInOut(duration: 0.2)) { intent = .loaded(updated) }
                if updated.state == .booked {
                    bookings += 1
                    booked()
                } else {
                    approveError = "The purchase didn't go through. Nothing was charged."
                }
            } catch {
                approveError = (error as? LocalizedError)?.errorDescription ?? "Couldn't reach the checkout agent."
            }
            isApproving = false
        }
    }

    private func cancelIfPending() {
        guard let current = intent.value, current.state == .awaitingApproval, !isApproving else { return }
        let api = env.api, id = current.id
        Task { try? await api.cancelCheckout(id: id) }
    }
}
