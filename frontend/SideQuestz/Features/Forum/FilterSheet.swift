import SwiftUI

/// Forum › Filter → "Sort & filter" sheet (GUI_PLAN.md §7.8). Edits a pending copy of the query;
/// "Show N results" counts what the API returns for it. The forum applies it when the sheet closes.
/// While a new count loads the number turns into loading dots ("Show ••• results").
///
/// Present with `.sqSheet(isPresented:)` (fitted): the options scroll above a pinned
/// "Show N results" button, capped at the prototype's 760pt sheet height.
struct ForumFilterSheet: View {
    @Binding var pending: ForumQuery
    let close: () -> Void

    @Environment(AppEnvironment.self) private var env
    @Environment(\.safeAreaBottom) private var safeBottom
    @State private var count: Int?
    /// The query `count` belongs to (nil = none yet), so reopening with a known count skips the call.
    @State private var countedQuery: ForumQuery?
    /// A count for the current options is loading.
    @State private var counting = false
    /// Measured height of the options; starts large so the first frame opens at full height.
    @State private var optionsHeight: CGFloat = 10_000

    /// The prototype's sheet is at most 760pt tall (including the home-indicator area).
    private let maxSheetHeight: CGFloat = 760
    /// 12pt gap + 52pt button.
    private let footerHeight: CGFloat = 64

    init(pending: Binding<ForumQuery>, initialCount: Int?, close: @escaping () -> Void) {
        _pending = pending
        self.close = close
        _count = State(initialValue: initialCount)
        _countedQuery = State(initialValue: initialCount == nil ? nil : pending.wrappedValue)
    }

    var body: some View {
        VStack(spacing: 0) {
            ScrollView {
                SheetScaffold(spacing: 12) {
                    options
                }
                .onGeometryChange(for: CGFloat.self, of: { $0.size.height }) { optionsHeight = $0 }
            }
            .scrollBounceBehavior(.basedOnSize)
            .frame(height: min(optionsHeight, maxSheetHeight - safeBottom - footerHeight))

            Button(action: close) { showButtonLabel }
                .buttonStyle(.sqPrimary)
                .accessibilityLabel(showLabel)
                .accessibilityValue(counting ? "Loading" : "")
                .padding(.horizontal, Metrics.side)
                .padding(.top, 12)
                .padding(.bottom, max(0, 34 - safeBottom))
        }
        .task(id: pending) { await recount() }
    }

    // MARK: Options

    @ViewBuilder private var options: some View {
        HStack {
            Text("Sort & filter")
                .socialText(20, .bold)
                .foregroundStyle(Theme.ink)
                .accessibilityAddTraits(.isHeader)
            Spacer(minLength: 8)
            Button {
                withMotion(Motion.quick) { pending = pending.clearingFilters() }
            } label: {
                Text("Clear all")
                    .sqFont(15, .semibold)
                    .foregroundStyle(Theme.sageInk)
                    .padding(.horizontal, 10)
                    .frame(height: 32)
                    .contentShape(Rectangle().inset(by: -6))
            }
            .buttonStyle(.plain)
        }

        sectionLabel("SORT BY")
        sortList

        sectionLabel("WHEN")
        chipGrid(ForumWhen.allCases.map { ($0, $0.label) }, isOn: { pending.when == $0 }) { pending.when = $0 }

        sectionLabel("DISTANCE")
        chipGrid(Self.distances.map { ($0.miles, $0.label) }, isOn: { pending.maxDistanceMi == $0 }) { pending.maxDistanceMi = $0 }

        sectionLabel("COST")
        chipGrid(Self.costs.map { ($0.tier, $0.label) }, isOn: { pending.cost.contains($0) }) { tier in
            if pending.cost.contains(tier) { pending.cost.remove(tier) } else { pending.cost.insert(tier) }
        }

        sectionLabel("INTERESTS")
        FlowLayout(spacing: 8) {
            ForEach(ForumQuery.interestTags, id: \.self) { tag in
                SQChip(label: tag, isOn: pending.tags.contains(tag), style: .socialCompactGrid) {
                    if pending.tags.contains(tag) { pending.tags.remove(tag) } else { pending.tags.insert(tag) }
                }
            }
        }

        HStack(spacing: 10) {
            Text("Only plans with open spots")
                .socialText(15, .semibold)
                .foregroundStyle(Theme.ink)
                .accessibilityHidden(true)
            Spacer(minLength: 0)
            SQToggle(isOn: $pending.openOnly, label: "Only plans with open spots")
                .frame(height: 32)
                .accessibilityHint("Hides free-now posts and full plans")
        }
        .padding(.vertical, 10)
    }

    private func sectionLabel(_ text: String) -> some View {
        Text(text)
            .socialText(13, .semibold)
            .tracking(0.4)
            .foregroundStyle(Theme.text3)
            .accessibilityAddTraits(.isHeader)
    }

    private var sortList: some View {
        VStack(spacing: 0) {
            ForEach(ForumSort.allCases) { sort in
                let selected = pending.sort == sort
                Button {
                    withMotion(Motion.quick) { pending.sort = sort }
                } label: {
                    HStack {
                        Text(sort.label)
                            .socialText(15, selected ? .semibold : .regular)
                            .foregroundStyle(Theme.ink)
                        Spacer(minLength: 0)
                        if selected {
                            SocialGlyph(kind: .check, size: 14, lineWidth: 2.8).foregroundStyle(Theme.sageInk)
                                .sqTransition(.pop)
                        }
                    }
                    .padding(.horizontal, 14)
                    .frame(height: 43)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Sort by \(sort.label)")
                .accessibilityAddTraits(selected ? .isSelected : [])
                RowDivider()
            }
        }
        .background(Theme.cream)
        .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        .animation(Motion.quick, value: pending.sort)
        .sensoryFeedback(.selection, trigger: pending.sort)
    }

    /// Four equal columns of grid chips (When, Distance, Cost).
    private func chipGrid<Value: Hashable>(_ items: [(Value, String)], isOn: @escaping (Value) -> Bool,
                                           pick: @escaping (Value) -> Void) -> some View {
        HStack(spacing: 8) {
            ForEach(items.indices, id: \.self) { index in
                let item = items[index]
                SQChip(label: item.1, isOn: isOn(item.0), style: .socialGrid) { pick(item.0) }
                    .accessibilityLabel(Self.spokenLabel(item.1))
            }
        }
    }

    // MARK: Count

    private var showLabel: String {
        guard let count else { return "Show results" }
        return "Show \(count) \(count == 1 ? "result" : "results")"
    }

    /// "Show 5 results", or "Show ••• results" while the count for new options loads.
    private var showButtonLabel: some View {
        ZStack {
            if counting {
                HStack(spacing: 7) {
                    Text("Show")
                    LoadingDots(color: Theme.ink, dotSize: 6).accessibilityHidden(true)
                    Text("results")
                }
                .transition(.opacity)
            } else {
                Text(showLabel)
                    .sqNumeric()
                    .transition(.opacity)
            }
        }
        .animation(Motion.standard, value: counting)
    }

    private func recount() async {
        let request = pending
        guard request != countedQuery else {
            if counting { withMotion { counting = false } }
            return
        }
        counting = true
        do {
            let posts = try await env.api.forumPosts(request)
            guard !Task.isCancelled else { return }
            withMotion {
                count = posts.count
                countedQuery = request
                counting = false
            }
        } catch {
            guard !Task.isCancelled else { return }
            withMotion {
                count = nil
                countedQuery = nil
                counting = false
            }
        }
    }

    // MARK: Options data

    private static let distances: [(miles: Double?, label: String)] = [(nil, "Any"), (1, "≤ 1 mi"), (2, "≤ 2 mi"), (5, "≤ 5 mi")]
    private static let costs: [(tier: Int, label: String)] = [(0, "Free"), (1, "$"), (2, "$$"), (3, "$$$")]

    /// VoiceOver wording for symbol-only chip labels.
    private static func spokenLabel(_ label: String) -> String {
        switch label {
        case "$": "Low cost"
        case "$$": "Medium cost"
        case "$$$": "High cost"
        case "≤ 1 mi": "Within 1 mile"
        default: label.replacingOccurrences(of: "≤ ", with: "Within ").replacingOccurrences(of: " mi", with: " miles")
        }
    }
}

extension ForumQuery {
    /// Same area, radius, scope and type; default sort and no filters ("Clear all", "Clear filters").
    func clearingFilters() -> ForumQuery {
        var query = self
        query.when = .any
        query.maxDistanceMi = nil
        query.cost = []
        query.tags = []
        query.openOnly = false
        query.sort = .forYou
        return query
    }
}
