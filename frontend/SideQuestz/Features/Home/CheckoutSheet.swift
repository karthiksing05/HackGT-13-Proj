import SwiftUI

/// Agent checkout (Visa), on top of the Event sheet (GUI_PLAN.md §7.6), showing a
/// `HomeCheckoutSession`. The agent prepares the purchase (`POST /checkout/intents`, `preparing`),
/// you approve it (`…/approve`, the only step that spends money), it pays (`processing`) and books
/// it (`booked`, with the ticket), or it fails with the server's reason, or it's cancelled. With
/// instant checkout on and the total within your limit, the server skips the approval. "Change"
/// switches the card (`PATCH /checkout/intents/{id}`) or adds one. Closing before paying cancels
/// the intent; closing while it pays keeps following it (the Event sheet reads "Booking").
///
/// Motion: status lines rotate while the agent prepares; its steps check off one by one and the
/// receipt rises in; Approve turns into loading dots while it pays; "Booked" pops its circle, draws
/// the check in and the ticket rises in. The sheet eases to each height.
struct HomeCheckoutSheet: View {
    let session: HomeCheckoutSession
    /// Others are on this plan, so they see the ticket too.
    var isShared = false
    let close: () -> Void

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var browserLink: HomeBrowserLink?

    var body: some View {
        SheetScaffold {
            ZStack(alignment: .topLeading) {
                if let current = session.intent.value, current.state == .booked {
                    VStack(alignment: .leading, spacing: 14) { bookedContent(current) }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .transition(.opacity)
                } else {
                    VStack(alignment: .leading, spacing: 14) {
                        header
                        ZStack(alignment: .topLeading) {
                            switch session.intent {
                            case .loading:
                                LoadingStateView(lines: creatingLines, minHeight: 280)
                                    .transition(.opacity)
                            case .failed(let message):
                                ErrorStateView(message: message, minHeight: 280, retry: session.restart)
                                    .transition(.opacity)
                            case .loaded(let current):
                                VStack(alignment: .leading, spacing: 14) {
                                    if current.state == .failed || current.state == .cancelled {
                                        endedContent(current)
                                    } else {
                                        details(current)
                                    }
                                }
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .transition(.opacity)
                            }
                        }
                    }
                    .transition(.opacity)
                }
            }
            .animation(reduceMotion ? Motion.reduced : Motion.gentle, value: stage)
        }
        .onAppear {
            session.sheetOpened()
            session.startIfNeeded()
        }
        .sensoryFeedback(trigger: session.state) { _, new in
            switch new {
            case .booked: .success
            case .failed: .error
            default: nil
            }
        }
        .sheet(item: $browserLink) { link in
            SafariView(url: link.url).ignoresSafeArea()
        }
    }

    /// What the sheet shows, for animating between them.
    private var stage: HomeCheckoutStage {
        switch session.intent {
        case .loading: .creating
        case .failed: .createFailed
        case .loaded(let current):
            switch current.state {
            case .booked: .booked
            case .failed, .cancelled: .ended
            case .preparing, .awaitingApproval, .processing: .active
            }
        }
    }

    /// While the intent is being made. Honest whatever the backend does: nothing is charged before
    /// approval, unless instant checkout is on.
    private var creatingLines: [String] {
        [
            "Getting your tickets ready…",
            "Finding tickets on the official site…",
            session.requestedInstant ? "Buying instantly if it's within your limit" : "Nothing is charged until you approve",
        ]
    }

    private var header: some View {
        HStack(spacing: 8) {
            Text("Agent checkout")
                .sqFont(22, .bold, relativeTo: .title2)
                .foregroundStyle(Theme.ink)
                .homeLine(22)
                .accessibilityAddTraits(.isHeader)
            if session.isInstant {
                TagLabel(text: "Instant", fill: Theme.sageTint, foreground: Theme.sageInk, fontSize: 12)
                    .sqTransition(.pop)
            }
            Spacer(minLength: 0)
            // Closing cancels anything not yet paid for; once it's paying (or done) it just closes.
            CloseCircleButton(label: session.isPending || session.intent.isLoading ? "Cancel checkout" : "Close", action: close)
                .padding(-6)
        }
        .animation(reduceMotion ? Motion.reduced : Motion.quick, value: session.isInstant)
    }

    // MARK: Preparing / awaiting approval / paying

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
        if let cardError = session.cardError {
            errorText(cardError)
        }
        payButton(current)
            .sqAppear(count + 2)
        if let approveError = session.approveError {
            errorText(approveError)
        }
        caption(current)
            .sqAppear(count + 3)
    }

    /// "Approve purchase": enabled only once the quote is ready; loading dots while it pays. An
    /// instant checkout has nothing to approve, so the same bar just shows it paying.
    @ViewBuilder
    private func payButton(_ current: CheckoutIntent) -> some View {
        let paying = session.isPaying
        if current.instant {
            LoadingDots(color: .white, dotSize: 7)
                .frame(maxWidth: .infinity, minHeight: Metrics.buttonHeight)
                .background(Theme.ink, in: RoundedRectangle(cornerRadius: Metrics.buttonRadius, style: .continuous))
                .accessibilityElement()
                .accessibilityLabel("Paying instantly")
        } else {
            Button(action: session.approve) {
                // Loading dots replace the label while the purchase goes through (same size, no jump).
                ZStack {
                    Text("Approve purchase")
                        .opacity(paying ? 0 : 1)
                    if paying {
                        LoadingDots(color: .white, dotSize: 7)
                            .transition(.opacity.combined(with: .scale(scale: 0.8)))
                    }
                }
            }
            // Greyed until the agent has a quote to approve.
            .buttonStyle(current.state == .preparing ? .sqDisabled() : .sqDark)
            .disabled(!session.canApprove)
            .animation(reduceMotion ? Motion.reduced : Motion.standard, value: paying)
            .animation(reduceMotion ? Motion.reduced : Motion.quick, value: current.state)
            .accessibilityLabel("Approve purchase")
            .accessibilityValue(paying ? "Paying" : current.state == .preparing ? "Waiting for the quote" : "")
        }
    }

    /// One line under the button: what the agent is doing, or the approval promise.
    private func caption(_ current: CheckoutIntent) -> some View {
        ZStack {
            if session.isPaying {
                CyclingStatusText(lines: payingLines(current))
                    .transition(.opacity)
            } else if current.state == .preparing {
                CyclingStatusText(lines: ["The agent is getting your tickets ready…", "Nothing is charged until you approve"])
                    .transition(.opacity)
            } else if session.requestedInstant {
                Text("This one is over your instant checkout limit, so the agent asks first.")
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
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: session.isPaying)
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: current.state)
    }

    private func payingLines(_ current: CheckoutIntent) -> [String] {
        let card = "\(current.cardBrand) •••• \(current.cardLast4)"
        return [current.instant ? "Paying instantly with \(card)…" : "Paying with \(card)…", "Confirming your booking…"]
    }

    private func errorText(_ message: String) -> some View {
        Text(message)
            .sqFont(13)
            .foregroundStyle(Theme.danger)
            .frame(maxWidth: .infinity)
            .multilineTextAlignment(.center)
            .sqTransition(.rise)
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
            receiptLine("\(current.quantity) × \(current.itemTitle)", Money.orPlaceholder(current.subtotalCents))
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

    // MARK: Card

    /// The card the agent pays with. The whole row opens a menu of your saved cards (the current
    /// one checked) and "Add a card"; while the switch reaches the server, loading dots stand in for
    /// "Change".
    private func cardRow(_ current: CheckoutIntent) -> some View {
        Menu {
            cardMenu(current)
        } label: {
            HStack(spacing: 10) {
                Text(current.cardBrand.uppercased())
                    .sqFont(12, .heavy)
                    .tracking(1)
                    .foregroundStyle(.white)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 4)
                    .background(current.cardBrand.lowercased() == "visa" ? Theme.visaNavy : Theme.ink,
                                in: RoundedRectangle(cornerRadius: 6, style: .continuous))
                    .contentTransition(.opacity)
                Text("•••• \(current.cardLast4)")
                    .sqFont(15)
                    .foregroundStyle(Theme.ink)
                    .homeLine(15)
                    .contentTransition(.numericText())
                Spacer(minLength: 8)
                ZStack(alignment: .trailing) {
                    if session.isChangingCard {
                        LoadingDots(color: Theme.text3, dotSize: 5)
                            .transition(.opacity)
                    } else {
                        Text("Change")
                            .sqFont(13)
                            .foregroundStyle(session.canChangeCard ? Theme.text3 : Theme.mutedStar)
                            .transition(.opacity)
                    }
                }
            }
            // 1pt border + the prototype's 12 / 14 padding.
            .padding(.vertical, 13)
            .padding(.horizontal, 15)
            .overlay { RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Theme.line, lineWidth: 1) }
            .contentShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
            .animation(reduceMotion ? Motion.reduced : Motion.quick, value: session.isChangingCard)
        }
        .menuOrder(.fixed)
        .buttonStyle(.sqPressable)
        .disabled(!session.canChangeCard || session.isChangingCard)
        .accessibilityLabel("Pay with \(current.cardBrand) ending in \(current.cardLast4)")
        .accessibilityValue(session.isChangingCard ? "Changing card" : "")
        .accessibilityHint("Pick another card or add one")
    }

    @ViewBuilder
    private func cardMenu(_ current: CheckoutIntent) -> some View {
        switch session.cards {
        case .loading:
            Text("Loading your cards…")
        case .failed:
            Button {
                Task { await session.loadCards() }
            } label: {
                Label("Couldn't load your cards. Try again", systemImage: "arrow.clockwise")
            }
        case .loaded(let cards):
            Section("Pay with") {
                ForEach(cards) { card in
                    Button {
                        session.choose(card)
                    } label: {
                        if session.isSelected(card, in: current) {
                            Label(Self.cardName(card), systemImage: "checkmark")
                        } else {
                            Text(Self.cardName(card))
                        }
                    }
                }
            }
        }
        Section {
            Button(action: session.addCard) {
                Label("Add a card", systemImage: "plus")
            }
        }
    }

    private static func cardName(_ card: PaymentMethod) -> String {
        "\(card.brand) •••• \(card.last4)\(card.isDefault ? " · default" : "")"
    }

    // MARK: Failed / cancelled

    @ViewBuilder
    private func endedContent(_ current: CheckoutIntent) -> some View {
        let failed = current.state == .failed
        // No card to pay with: adding one is the way forward.
        let needsCard = failed && current.cardLast4.isEmpty && (session.cards.value?.isEmpty ?? true)
        VStack(spacing: 10) {
            Image(systemName: failed ? "exclamationmark" : "xmark")
                .font(.system(size: 26, weight: .bold))
                .foregroundStyle(failed ? Theme.dangerText : Theme.text2)
                .frame(width: 64, height: 64)
                .background(failed ? Theme.dangerBg : Theme.cream, in: Circle())
                .homePopIn(delay: 0.05)
                .accessibilityHidden(true)
            Text(failed ? "Couldn't book it" : "Checkout cancelled")
                .sqFont(22, .bold, relativeTo: .title2)
                .foregroundStyle(Theme.ink)
                .homeLine(22)
                .accessibilityAddTraits(.isHeader)
                .sqAppear(1)
            Text(failed ? (current.failureReason ?? "The agent couldn't finish this purchase.") : "Nothing was bought.")
                .sqFont(15)
                .foregroundStyle(Theme.text2)
                .homeLine(15)
                .multilineTextAlignment(.center)
                .frame(maxWidth: 300)
                .fixedSize(horizontal: false, vertical: true)
                .sqAppear(2)
        }
        .frame(maxWidth: .infinity)
        .padding(.top, 6)
        .padding(.bottom, 8)
        if let cardError = session.cardError {
            errorText(cardError)
        }
        Button {
            if needsCard {
                session.addCardAndRestart()
            } else {
                session.restart()
            }
        } label: {
            ZStack {
                Text(needsCard ? "Add a card" : failed ? "Try again" : "Start again")
                    .opacity(session.isChangingCard ? 0 : 1)
                if session.isChangingCard {
                    LoadingDots(color: Theme.ink, dotSize: 7)
                        .transition(.opacity.combined(with: .scale(scale: 0.8)))
                }
            }
        }
        .buttonStyle(.sqPrimary)
        .disabled(session.isChangingCard)
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: session.isChangingCard)
        .sqAppear(3)
    }

    // MARK: Booked

    @ViewBuilder
    private func bookedContent(_ current: CheckoutIntent) -> some View {
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
            Text(isShared ? "Your ticket is saved to this sidequest. The group sees it too." : "Your ticket is saved to this sidequest.")
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
        ticketSummary(current)
            .sqAppear(4)
        if case .loaded(let ticket?) = session.ticket, let url = ticket.url {
            Button("View ticket") { browserLink = HomeBrowserLink(url: url) }
                .buttonStyle(.sqOutlined)
                .accessibilityHint("Opens your ticket in the app")
                .sqTransition(.rise)
        }
        Button("Done", action: close)
            .buttonStyle(.sqPrimary)
            .sqAppear(5)
    }

    /// The booking as the server has it: confirmation code, how many, what it cost, which card.
    /// Shimmers while the item is reloaded for its ticket.
    private func ticketSummary(_ current: CheckoutIntent) -> some View {
        VStack(spacing: 6) {
            switch session.ticket {
            case .loaded(let ticket?):
                if let code = ticket.confirmation {
                    summaryLine("Confirmation", code, emphasized: true)
                }
                summaryLine(Self.ticketCount(ticket.quantity), (ticket.totalCents ?? current.totalCents).map { Money.format($0) })
            case .loaded(nil):
                summaryLine(Self.ticketCount(current.quantity), current.totalCents.map { Money.format($0) })
            case .loading, .failed, nil:
                HStack {
                    SkeletonBlock(width: 96, height: 11, radius: 5, color: Theme.skeletonOnCream)
                    Spacer(minLength: 8)
                    SkeletonBlock(width: 72, height: 11, radius: 5, color: Theme.skeletonOnCream)
                }
                .frame(height: 15 * 1.35)
                .sqShimmer()
                .accessibilityElement()
                .accessibilityLabel("Loading your ticket")
            }
            summaryLine("Paid with", "\(current.cardBrand) •••• \(current.cardLast4)")
                .foregroundStyle(Theme.text3)
        }
        .sqFont(15)
        .foregroundStyle(Theme.ink)
        .padding(.vertical, 12)
        .padding(.horizontal, 14)
        .background(Theme.cream, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: session.ticket?.phase)
    }

    private func summaryLine(_ label: String, _ value: String?, emphasized: Bool = false) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(label)
            Spacer(minLength: 8)
            if let value {
                Text(value)
                    .fontWeight(emphasized ? .semibold : .regular)
                    .textSelection(.enabled)
            }
        }
        .homeLine(15)
        .accessibilityElement(children: .combine)
    }

    private static func ticketCount(_ quantity: Int) -> String {
        quantity == 1 ? "1 ticket" : "\(quantity) tickets"
    }
}

private enum HomeCheckoutStage: Hashable {
    case creating, createFailed, active, booked, ended
}
