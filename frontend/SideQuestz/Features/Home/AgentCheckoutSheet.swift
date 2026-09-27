import LocalAuthentication
import SwiftUI

/// Where "Let Muse get your tickets" opens (Router.agentCheckout): right after a plan is saved or
/// joined, or from the plan's "Get tickets" on Home.
struct AgentCheckoutRoute: Identifiable, Equatable {
    let itineraryId: String
    var id: String { itineraryId }
}

extension Router {
    /// Agentic checkout on and the plan has stops Muse can still buy for you: ask "Let Muse get your
    /// tickets", `delay` after the plan comes back (so a closing cover is out of the way).
    @MainActor
    func offerAgentCheckout(itineraryId: String, env: AppEnvironment, delay: Duration = .zero) async {
        guard env.preferences?.instantCheckout == true,
              let plan = try? await env.api.checkoutPlan(itineraryId: itineraryId), plan.shouldPrompt, plan.agenticCheckout
        else { return }
        if delay > .zero { try? await Task.sleep(for: delay) }
        agentCheckout = AgentCheckoutRoute(itineraryId: itineraryId)
    }
}

/// Agentic checkout for one plan (API_CONTRACT.md › Agentic checkout): the prompt (which stops,
/// how many tickets, the budget), one Face ID approval, then Muse buys each ticket on the SideQuestz
/// Events sandbox merchant within that budget and the results come back (confirmations, tickets).
/// Stripe test mode: no real payment is made.
///
/// While the run is going the session follows it: `checkout.run` / `checkout.status` events plus a
/// fresh copy every 2 s. Closing the sheet mid-run leaves Muse working; reopening picks it up.
@Observable
final class AgentCheckoutSession {
    enum Phase: Equatable { case prompt, approving, running, finished }

    let itineraryId: String
    private(set) var plan: Loadable<CheckoutPlan> = .loading
    private(set) var run: CheckoutRun?
    private(set) var phase: Phase = .prompt
    private(set) var error: String?
    private(set) var isStopping = false
    var selected: Set<String> = []
    var quantities: [String: Int] = [:]
    var budgetCents = 0

    @ObservationIgnored private let env: AppEnvironment
    @ObservationIgnored private var followTask: Task<Void, Never>?

    init(itineraryId: String, env: AppEnvironment) {
        self.itineraryId = itineraryId
        self.env = env
    }

    deinit { followTask?.cancel() }

    // MARK: Prompt

    func load() async {
        withMotion { plan = .loading; error = nil }
        let result = await Loadable.run { [env, itineraryId] in try await env.api.checkoutPlan(itineraryId: itineraryId) }
        withMotion(Motion.gentle) { plan = result }
        guard let loaded = result.value else { return }
        if let active = loaded.activeRunId {
            await follow(runId: active)
            return
        }
        selected = Set(loaded.buyable.map(\.itemId))
        quantities = Dictionary(uniqueKeysWithValues: loaded.buyable.map { ($0.itemId, max(1, $0.quantity)) })
        budgetCents = max(budgetCents, estimateWithFees(loaded))
        if budgetCents == 0 { budgetCents = loaded.suggestedBudgetCents }
    }

    /// Tickets picked × price, before fees.
    var estimateCents: Int {
        guard let plan = plan.value else { return 0 }
        return plan.buyable.filter { selected.contains($0.itemId) }
            .reduce(0) { $0 + ($1.priceCents ?? 0) * quantity(for: $1.itemId) }
    }

    /// The estimate plus ~15% for fees, rounded up to whole dollars (what the server suggests).
    private func estimateWithFees(_ plan: CheckoutPlan) -> Int {
        let cents = plan.buyable.reduce(0) { $0 + ($1.priceCents ?? 0) * max(1, $1.quantity) }
        return max((cents * 115 / 100 + 99) / 100 * 100, plan.defaultBudgetCents)
    }

    /// Budget chips: the suggestion first, then a little more room.
    var budgetOptions: [Int] {
        let base = max(100, (estimateCents * 115 / 100 + 99) / 100 * 100)
        let options = [base, base + 2500, base + 5000, plan.value?.defaultBudgetCents ?? 0, budgetCents]
        return Array(Set(options.filter { $0 >= 100 && $0 <= 100_000 })).sorted()
    }

    func quantity(for itemId: String) -> Int { quantities[itemId] ?? 1 }

    func toggle(_ itemId: String) {
        withMotion(Motion.quick) {
            if selected.contains(itemId) { selected.remove(itemId) } else { selected.insert(itemId) }
        }
    }

    func changeQuantity(_ itemId: String, by delta: Int) {
        withMotion(Motion.quick) { quantities[itemId] = min(8, max(1, quantity(for: itemId) + delta)) }
    }

    var canApprove: Bool {
        guard let plan = plan.value, phase == .prompt else { return false }
        return !selected.isEmpty && budgetCents >= 100 && plan.paymentMethodId != nil && plan.available
    }

    /// "Approve with Face ID": the one approval, then Muse starts.
    func approve() {
        guard canApprove, let plan = plan.value else { return }
        withMotion { phase = .approving; error = nil }
        Task {
            do {
                try await Self.authenticate(allowUnprotected: env.isMock)
            } catch {
                withMotion { self.phase = .prompt; self.error = (error as? LocalizedError)?.errorDescription ?? "Face ID didn't work. Try again." }
                return
            }
            let items = plan.buyable.filter { selected.contains($0.itemId) }
                .map { CreateCheckoutRun.Item(itemId: $0.itemId, quantity: quantity(for: $0.itemId)) }
            let request = CreateCheckoutRun(budgetCents: budgetCents, items: items, paymentMethodId: plan.paymentMethodId)
            do {
                let started = try await env.api.startCheckoutRun(itineraryId: itineraryId, request)
                withMotion(Motion.gentle) { self.run = started; self.phase = .running }
                await follow(runId: started.id)
            } catch {
                withMotion { self.phase = .prompt; self.error = (error as? LocalizedError)?.errorDescription ?? "Muse couldn't start. Try again." }
            }
        }
    }

    enum AuthError: LocalizedError {
        case unavailable(String)
        var errorDescription: String? {
            switch self { case .unavailable(let message): message }
        }
    }

    /// Face ID (or the passcode). A simulator or demo device with no passcode can't prove anything,
    /// so only the offline demo lets that through.
    static func authenticate(allowUnprotected: Bool) async throws {
        let context = LAContext()
        var policyError: NSError?
        guard context.canEvaluatePolicy(.deviceOwnerAuthentication, error: &policyError) else {
            #if targetEnvironment(simulator)
            return
            #else
            if allowUnprotected { return }
            throw AuthError.unavailable("Set a passcode on this device to approve purchases.")
            #endif
        }
        try await context.evaluatePolicy(.deviceOwnerAuthentication, localizedReason: "Approve Muse buying tickets for your plan")
    }

    // MARK: Following the run

    func follow(runId: String) async {
        followTask?.cancel()
        let result = await Loadable.run { [env] in try await env.api.checkoutRun(id: runId) }
        switch result {
        case .loaded(let current): apply(current)
        case .failed(let message) where run == nil: withMotion { plan = .failed(message) }
        default: break
        }
        guard run?.isRunning == true else { return }
        followTask = Task { [weak self, env] in
            await withTaskGroup(of: Void.self) { group in
                // The hub lives on the main actor (like every other realtime listener).
                group.addTask { @MainActor in
                    for await event in env.realtime.subscribe() {
                        guard !Task.isCancelled else { return }
                        switch event {
                        case .checkoutRun(let id, _, _) where id == runId: await self?.refresh(runId)
                        case .checkoutStatus: await self?.refresh(runId)
                        default: break
                        }
                    }
                }
                group.addTask {
                    while !Task.isCancelled {
                        try? await Task.sleep(for: .seconds(2))
                        await self?.refresh(runId)
                    }
                }
                while let self, self.run?.isRunning == true, !Task.isCancelled {
                    try? await Task.sleep(for: .milliseconds(300))
                }
                group.cancelAll()
            }
        }
    }

    private func refresh(_ runId: String) async {
        guard let current = try? await env.api.checkoutRun(id: runId) else { return }
        apply(current)
    }

    private func apply(_ current: CheckoutRun) {
        withMotion(Motion.gentle) {
            run = current
            phase = current.isRunning ? .running : .finished
        }
    }

    /// "Stop": nothing more is bought; tickets already bought stay.
    func stop() {
        guard let run, run.isRunning, !isStopping else { return }
        isStopping = true
        Task {
            if let stopped = try? await env.api.cancelCheckoutRun(id: run.id) { apply(stopped) }
            isStopping = false
        }
    }

    func stopFollowing() {
        followTask?.cancel()
        followTask = nil
    }
}

// MARK: - Sheet

/// Owns the session for as long as the sheet is up. Closing mid-run leaves Muse working; reopening
/// finds the run through the plan's `activeRunId` and follows it again.
struct AgentCheckoutHost: View {
    @State private var session: AgentCheckoutSession
    let close: () -> Void

    init(route: AgentCheckoutRoute, env: AppEnvironment, close: @escaping () -> Void) {
        _session = State(initialValue: AgentCheckoutSession(itineraryId: route.itineraryId, env: env))
        self.close = close
    }

    var body: some View {
        AgentCheckoutSheet(session: session, close: close)
            .onDisappear { session.stopFollowing() }
    }
}

struct AgentCheckoutSheet: View {
    let session: AgentCheckoutSession
    let close: () -> Void

    @Environment(AppEnvironment.self) private var env
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var browserLink: HomeBrowserLink?

    var body: some View {
        SheetScaffold {
            header
            ZStack(alignment: .topLeading) {
                switch session.plan {
                case .loading where session.run == nil:
                    LoadingStateView(lines: ["Finding the tickets on your plan…"], minHeight: 240)
                        .transition(.opacity)
                case .failed(let message) where session.run == nil:
                    ErrorStateView(message: message, minHeight: 240) { Task { await session.load() } }
                        .transition(.opacity)
                default:
                    VStack(alignment: .leading, spacing: 14) {
                        switch session.phase {
                        case .prompt, .approving: prompt
                        case .running: running
                        case .finished: results
                        }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .transition(.opacity)
                }
            }
            .animation(reduceMotion ? Motion.reduced : Motion.gentle, value: session.phase)
        }
        .task { if session.run == nil && session.plan.value == nil { await session.load() } }
        .sensoryFeedback(trigger: session.phase) { _, new in new == .finished ? .success : nil }
        .sheet(item: $browserLink) { link in
            SafariView(url: link.url).ignoresSafeArea()
        }
    }

    private var title: String {
        switch session.phase {
        case .prompt, .approving: "Let Muse get your tickets"
        case .running: "Muse is getting your tickets"
        case .finished: "Your tickets"
        }
    }

    private var header: some View {
        HStack(spacing: 8) {
            Text(title)
                .sqFont(22, .bold, relativeTo: .title2)
                .foregroundStyle(Theme.ink)
                .homeLine(22)
                .accessibilityAddTraits(.isHeader)
                .contentTransition(.opacity)
            TagLabel(text: "Sandbox", fill: Theme.sageTint, foreground: Theme.sageInk, fontSize: 12)
            Spacer(minLength: 0)
            CloseCircleButton(label: session.phase == .running ? "Close (Muse keeps going)" : "Close", action: close)
                .padding(-6)
        }
    }

    // MARK: Prompt

    @ViewBuilder private var prompt: some View {
        if let plan = session.plan.value {
            if plan.buyable.isEmpty {
                EmptyStateView(message: plan.items.isEmpty ? "No stops on this plan sell tickets." : "You already have tickets for every stop.")
            } else {
                VStack(spacing: 8) {
                    ForEach(Array(plan.buyable.enumerated()), id: \.element.id) { index, item in
                        itemRow(item).sqAppear(index)
                    }
                }
                budget(plan)
                cardLine(plan)
                if let error = session.error { errorText(error) }
                approveButton(plan)
                Text("Muse buys only what's checked, within your budget, on events.sidequestz.tech. Sandbox: no real payment is made.")
                    .sqFont(12, relativeTo: .caption)
                    .foregroundStyle(Theme.text3)
                    .homeLine(12)
                    .multilineTextAlignment(.center)
                    .frame(maxWidth: .infinity)
            }
        }
    }

    private func itemRow(_ item: CheckoutPlanItem) -> some View {
        let on = session.selected.contains(item.itemId)
        let qty = session.quantity(for: item.itemId)
        return HStack(spacing: 10) {
            Button { session.toggle(item.itemId) } label: {
                HStack(spacing: 10) {
                    CheckGlyph(lineWidth: 3)
                        .foregroundStyle(.white)
                        .frame(width: 12, height: 12)
                        .frame(width: 22, height: 22)
                        .background(on ? Theme.ink : Theme.mutedStar, in: Circle())
                    VStack(alignment: .leading, spacing: 2) {
                        Text(item.title)
                            .sqFont(15, .semibold)
                            .foregroundStyle(on ? Theme.ink : Theme.text3)
                            .lineLimit(2)
                        Text("\(TimeFormat(clock: env.clock).time(item.start)) · \(Money.orPlaceholder(item.priceCents)) each")
                            .sqFont(12)
                            .foregroundStyle(Theme.text3)
                    }
                    Spacer(minLength: 4)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel(item.title)
            .accessibilityValue(on ? "Included" : "Not included")
            if on {
                HStack(spacing: 6) {
                    stepButton("−", enabled: qty > 1) { session.changeQuantity(item.itemId, by: -1) }
                    Text("\(qty)")
                        .sqFont(15, .semibold)
                        .sqNumeric()
                        .frame(minWidth: 16)
                        .accessibilityLabel("\(qty) tickets")
                    stepButton("+", enabled: qty < 8) { session.changeQuantity(item.itemId, by: 1) }
                }
                .sqTransition(.pop)
            }
        }
        .padding(.vertical, 10)
        .padding(.horizontal, 12)
        .background(Theme.cream, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .disabled(session.phase != .prompt)
    }

    private func stepButton(_ label: String, enabled: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(label)
                .sqFont(17, .semibold)
                .foregroundStyle(enabled ? Theme.ink : Theme.mutedStar)
                .frame(width: 30, height: 30)
                .overlay { Circle().strokeBorder(Theme.line, lineWidth: 1) }
                .frame(width: Metrics.minTouch, height: Metrics.minTouch)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(!enabled)
        .accessibilityLabel(label == "+" ? "One more ticket" : "One fewer ticket")
    }

    private func budget(_ plan: CheckoutPlan) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text("Budget")
                    .sqFont(15, .semibold)
                    .foregroundStyle(Theme.ink)
                Spacer()
                Text("Tickets ~\(Money.compact(session.estimateCents)) + fees")
                    .sqFont(12)
                    .foregroundStyle(Theme.text3)
                    .sqNumeric()
            }
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 8) {
                    ForEach(session.budgetOptions, id: \.self) { cents in
                        SQChip(label: Money.compact(cents), isOn: cents == session.budgetCents, style: .tag) {
                            withMotion(Motion.quick) { session.budgetCents = cents }
                        }
                        .accessibilityLabel("Budget \(Money.compact(cents))")
                    }
                }
            }
            if session.budgetCents < session.estimateCents {
                Text("That's less than the tickets cost, so Muse will skip what doesn't fit.")
                    .sqFont(12)
                    .foregroundStyle(Theme.danger)
                    .sqTransition(.rise)
            }
        }
        .disabled(session.phase != .prompt)
    }

    @ViewBuilder private func cardLine(_ plan: CheckoutPlan) -> some View {
        if let brand = plan.cardBrand, let last4 = plan.cardLast4 {
            Text("Pays with \(brand) •••• \(last4) (Stripe test mode)")
                .sqFont(13)
                .foregroundStyle(Theme.text2)
        } else {
            Text("Add a card in Account › Payments first.")
                .sqFont(13)
                .foregroundStyle(Theme.danger)
        }
    }

    private func approveButton(_ plan: CheckoutPlan) -> some View {
        let approving = session.phase == .approving
        return Button(action: session.approve) {
            ZStack {
                Label("Approve \(Money.compact(session.budgetCents)) with Face ID", systemImage: "faceid")
                    .opacity(approving ? 0 : 1)
                if approving {
                    LoadingDots(color: .white, dotSize: 7)
                        .transition(.opacity.combined(with: .scale(scale: 0.8)))
                }
            }
        }
        .buttonStyle(session.canApprove || approving ? .sqDark : .sqDisabled())
        .disabled(!session.canApprove)
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: approving)
        .accessibilityLabel("Approve a \(Money.compact(session.budgetCents)) budget with Face ID")
    }

    // MARK: Running

    @ViewBuilder private var running: some View {
        if let run = session.run {
            spendBar(run)
            VStack(spacing: 8) {
                ForEach(Array(run.intents.enumerated()), id: \.element.id) { index, intent in
                    progressRow(intent).sqAppear(index)
                }
            }
            Button(action: session.stop) {
                ZStack {
                    Text("Stop").opacity(session.isStopping ? 0 : 1)
                    if session.isStopping { LoadingDots(color: Theme.ink, dotSize: 6) }
                }
            }
            .buttonStyle(.sqTintPill)
            .frame(maxWidth: .infinity)
            .disabled(session.isStopping)
            CyclingStatusText(lines: ["Muse is reading the ticket pages…", "Each payment is capped at its quote", "You can close this; Muse keeps going"])
                .sqFont(12, relativeTo: .caption)
                .foregroundStyle(Theme.text3)
                .frame(maxWidth: .infinity)
        }
    }

    private func spendBar(_ run: CheckoutRun) -> some View {
        let fraction = run.budgetCents > 0 ? min(1, Double(run.spentCents) / Double(run.budgetCents)) : 0
        return VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text("\(Money.format(run.spentCents)) of \(Money.compact(run.budgetCents))")
                    .sqFont(15, .semibold)
                    .foregroundStyle(Theme.ink)
                    .sqNumeric()
                    .contentTransition(.numericText())
                Spacer()
                Text("\(run.cardBrand) •••• \(run.cardLast4)")
                    .sqFont(12)
                    .foregroundStyle(Theme.text3)
            }
            GeometryReader { geo in
                ZStack(alignment: .leading) {
                    Capsule().fill(Theme.cream)
                    Capsule().fill(Theme.success).frame(width: max(0, geo.size.width * fraction))
                }
            }
            .frame(height: 6)
            .animation(reduceMotion ? Motion.reduced : Motion.gentle, value: fraction)
        }
        .accessibilityElement(children: .combine)
    }

    private func progressRow(_ intent: CheckoutIntent) -> some View {
        HStack(spacing: 10) {
            stateBadge(intent.state)
            VStack(alignment: .leading, spacing: 2) {
                Text("\(intent.quantity) × \(intent.itemTitle)")
                    .sqFont(15, .semibold)
                    .foregroundStyle(Theme.ink)
                    .lineLimit(2)
                Text(statusLine(intent))
                    .sqFont(12)
                    .foregroundStyle(intent.state == .failed ? Theme.danger : Theme.text3)
                    .contentTransition(.opacity)
            }
            Spacer(minLength: 0)
        }
        .padding(.vertical, 10)
        .padding(.horizontal, 12)
        .background(Theme.cream, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .accessibilityElement(children: .combine)
    }

    private func statusLine(_ intent: CheckoutIntent) -> String {
        switch intent.state {
        case .booked: return intent.confirmation.map { "Booked · \($0)" } ?? "Booked"
        case .failed, .cancelled: return intent.outcomeLine
        default: return intent.steps.last(where: { !$0.done })?.text ?? intent.steps.last?.text ?? "Waiting for Muse"
        }
    }

    @ViewBuilder private func stateBadge(_ state: CheckoutState) -> some View {
        switch state {
        case .booked:
            AnimatedCheck(lineWidth: 3, delay: 0.1)
                .foregroundStyle(.white)
                .frame(width: 12, height: 12)
                .frame(width: 22, height: 22)
                .background(Theme.success, in: Circle())
        case .failed, .cancelled:
            Text("!")
                .sqFont(13, .heavy)
                .foregroundStyle(.white)
                .frame(width: 22, height: 22)
                .background(Theme.danger, in: Circle())
        default:
            LoadingDots(color: Theme.text3, dotSize: 4)
                .frame(width: 22, height: 22)
        }
    }

    // MARK: Results

    @ViewBuilder private var results: some View {
        if let run = session.run {
            if let summary = run.summary, !summary.isEmpty {
                Text(summary)
                    .sqFont(15)
                    .foregroundStyle(Theme.ink)
                    .homeLine(15)
                    .fixedSize(horizontal: false, vertical: true)
            }
            spendBar(run)
            VStack(spacing: 8) {
                ForEach(Array(run.booked.enumerated()), id: \.element.id) { index, intent in
                    bookedRow(intent).sqAppear(index)
                }
                ForEach(Array(run.notBooked.enumerated()), id: \.element.id) { index, intent in
                    missedRow(intent).sqAppear(run.booked.count + index)
                }
            }
            Button("Done", action: close)
                .buttonStyle(.sqDark)
        }
    }

    private func bookedRow(_ intent: CheckoutIntent) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 10) {
                stateBadge(.booked)
                Text(intent.itemTitle)
                    .sqFont(15, .semibold)
                    .foregroundStyle(Theme.ink)
                    .lineLimit(2)
                Spacer(minLength: 0)
                Text(Money.orPlaceholder(intent.finalCents ?? intent.totalCents, placeholder: "[total]"))
                    .sqFont(15, .semibold)
                    .sqNumeric()
            }
            HStack(spacing: 8) {
                if let code = intent.confirmation {
                    Text(code)
                        .sqFont(13, .bold, design: .monospaced)
                        .foregroundStyle(Theme.ink)
                        .textSelection(.enabled)
                }
                Text("Admits \(intent.quantity)")
                    .sqFont(12)
                    .foregroundStyle(Theme.text3)
                Spacer(minLength: 0)
                if let url = intent.ticketURL {
                    Button("Open ticket") { browserLink = HomeBrowserLink(url: url) }
                        .buttonStyle(.sqTintPill)
                }
            }
        }
        .padding(.vertical, 12)
        .padding(.horizontal, 12)
        .background(Theme.cream, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
    }

    private func missedRow(_ intent: CheckoutIntent) -> some View {
        HStack(spacing: 10) {
            stateBadge(intent.state)
            VStack(alignment: .leading, spacing: 2) {
                Text(intent.itemTitle)
                    .sqFont(15, .semibold)
                    .foregroundStyle(Theme.ink)
                    .lineLimit(2)
                Text(intent.outcomeLine)
                    .sqFont(12)
                    .foregroundStyle(Theme.text3)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
            if let url = intent.checkoutURL {
                Button("Event page") { browserLink = HomeBrowserLink(url: url) }
                    .buttonStyle(.sqTintPill)
            }
        }
        .padding(.vertical, 10)
        .padding(.horizontal, 12)
        .background(Theme.cream, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
    }

    private func errorText(_ message: String) -> some View {
        Text(message)
            .sqFont(13)
            .foregroundStyle(Theme.danger)
            .frame(maxWidth: .infinity)
            .multilineTextAlignment(.center)
            .sqTransition(.rise)
    }
}
