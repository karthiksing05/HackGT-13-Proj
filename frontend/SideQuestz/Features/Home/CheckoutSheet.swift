import SwiftUI

/// Agent checkout (Visa), on top of the Event sheet (GUI_PLAN.md §7.6). The agent prepares the
/// purchase (`POST /checkout/intents`); nothing is spent until "Approve purchase" (`…/approve`).
/// × (or closing the sheet) cancels a pending intent.
///
/// Motion: status lines rotate for as long as the agent prepares the order; then its steps check
/// off one by one and the receipt rises in. Approving shows loading dots and status lines while the
/// request runs; "Booked" pops its circle and draws the check in. The sheet eases to each height.
struct HomeCheckoutSheet: View {
    let item: ItineraryItem
    let close: () -> Void
    /// The ticket was booked.
    let booked: () -> Void

    @Environment(AppEnvironment.self) private var env
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var intent: Loadable<CheckoutIntent> = .loading
    @State private var isApproving = false
    @State private var approveError: String?
    @State private var bookings = 0

    /// While the agent prepares the order (true whatever the backend does).
    private static let preparingLines = [
        "Getting your tickets ready…",
        "Finding tickets on the official site…",
        "Nothing is charged until you approve",
    ]

    var body: some View {
        SheetScaffold {
            ZStack(alignment: .topLeading) {
                if let current = intent.value, current.state == .booked {
                    VStack(alignment: .leading, spacing: 14) { bookedContent }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .transition(.opacity)
                } else {
                    VStack(alignment: .leading, spacing: 14) {
                        header
                        ZStack(alignment: .topLeading) {
                            switch intent {
                            case .loading:
                                LoadingStateView(lines: Self.preparingLines, minHeight: 280)
                                    .transition(.opacity)
                            case .failed(let message):
                                ErrorStateView(message: message, minHeight: 280) { Task { await start() } }
                                    .transition(.opacity)
                            case .loaded(let current):
                                VStack(alignment: .leading, spacing: 14) { details(current) }
                                    .frame(maxWidth: .infinity, alignment: .leading)
                                    .transition(.opacity)
                            }
                        }
                    }
                    .transition(.opacity)
                }
            }
            .animation(reduceMotion ? Motion.reduced : Motion.gentle, value: intent.phase)
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
        let count = current.steps.count
        VStack(alignment: .leading, spacing: 8) {
            ForEach(Array(current.steps.enumerated()), id: \.offset) { index, step in
                stepRow(step, index: index)
                    .sqAppear(index)
            }
        }
        receipt(current)
            .sqAppear(count)
        cardRow(current)
            .sqAppear(count + 1)
        Button(action: approve) {
            // Loading dots replace the label while the purchase goes through (same size, no jump).
            ZStack {
                Text("Approve purchase")
                    .opacity(isApproving ? 0 : 1)
                if isApproving {
                    LoadingDots(color: .white, dotSize: 7)
                        .transition(.opacity.combined(with: .scale(scale: 0.8)))
                }
            }
        }
        .buttonStyle(.sqDark)
        .disabled(isApproving)
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: isApproving)
        .accessibilityLabel("Approve purchase")
        .accessibilityValue(isApproving ? "Processing" : "")
        .sqAppear(count + 2)
        if let approveError {
            Text(approveError)
                .sqFont(13)
                .foregroundStyle(Theme.danger)
                .frame(maxWidth: .infinity)
                .multilineTextAlignment(.center)
                .sqTransition(.rise)
        }
        // While approving, what the agent is doing rotates here for as long as the request runs.
        ZStack {
            if isApproving {
                CyclingStatusText(lines: approvingLines(current))
                    .transition(.opacity)
            } else {
                Text("The agent can't spend anything until you approve.")
                    .transition(.opacity)
            }
        }
        .sqFont(12, relativeTo: .caption)
        .foregroundStyle(Theme.text3)
        .homeLine(12)
        .multilineTextAlignment(.center)
        .frame(maxWidth: .infinity)
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: isApproving)
        .sqAppear(count + 3)
    }

    private func approvingLines(_ current: CheckoutIntent) -> [String] {
        ["Paying with \(current.cardBrand) •••• \(current.cardLast4)…", "Confirming your booking…"]
    }

    /// A step with its check: done steps draw their check in, one after another.
    private func stepRow(_ step: CheckoutStep, index: Int) -> some View {
        HStack(spacing: 10) {
            Group {
                if step.done {
                    AnimatedCheck(lineWidth: 3, delay: 0.15 + 0.12 * Double(index))
                } else {
                    CheckGlyph(lineWidth: 3)
                }
            }
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
            Text(amount).sqNumeric()
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
            AnimatedCheck(lineWidth: 2.4, delay: 0.3)
                .foregroundStyle(.white)
                .frame(width: 32, height: 32)
                .frame(width: 64, height: 64)
                .background(Theme.success, in: Circle())
                .homePopIn(delay: 0.05)
            Text("Booked")
                .sqFont(22, .bold, relativeTo: .title2)
                .foregroundStyle(Theme.ink)
                .homeLine(22)
                .accessibilityAddTraits(.isHeader)
                .sqAppear(2)
            Text("Your ticket is saved to this itinerary. The group sees it too.")
                .sqFont(15)
                .foregroundStyle(Theme.text2)
                .homeLine(15)
                .multilineTextAlignment(.center)
                .frame(maxWidth: 280)
                .sqAppear(3)
        }
        .frame(maxWidth: .infinity)
        .padding(.top, 20)
        .padding(.bottom, 8)
        Button("Done", action: close)
            .buttonStyle(.sqPrimary)
            .sqAppear(5)
    }

    // MARK: Actions

    private func start() async {
        withMotion { intent = .loading }
        let result = await Loadable.run { try await env.api.createCheckoutIntent(itemId: item.id, quantity: 1) }
        withMotion(Motion.gentle) { intent = result }
    }

    private func approve() {
        guard let current = intent.value, !isApproving else { return }
        withMotion {
            isApproving = true
            approveError = nil
        }
        Task {
            do {
                let updated = try await env.api.approveCheckout(id: current.id)
                withMotion(Motion.gentle) {
                    intent = .loaded(updated)
                    isApproving = false
                    if updated.state != .booked {
                        approveError = "The purchase didn't go through. Nothing was charged."
                    }
                }
                if updated.state == .booked {
                    bookings += 1
                    booked()
                }
            } catch {
                withMotion(Motion.arrive) {
                    approveError = (error as? LocalizedError)?.errorDescription ?? "Couldn't reach the checkout agent."
                    isApproving = false
                }
            }
        }
    }

    private func cancelIfPending() {
        guard let current = intent.value, current.state == .awaitingApproval, !isApproving else { return }
        let api = env.api, id = current.id
        Task { try? await api.cancelCheckout(id: id) }
    }
}
