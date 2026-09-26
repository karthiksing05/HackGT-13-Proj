import SwiftUI

/// Home › Past (GUI_PLAN.md §7.5c): events you went to, grouped by day. Tap one to rate it.
struct HomePastView: View {
    let store: HomeStore
    let rate: (PastEvent) -> Void
    let retry: () -> Void
    @Environment(AppEnvironment.self) private var env

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("Events you went to. Tap one to rate it. Ratings update your taste profile.")
                .sqFont(13, relativeTo: .footnote)
                .foregroundStyle(Theme.text3)
                .homeLine(13)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.horizontal, Metrics.side)
                .padding(.top, 14)
            LoadableView(state: store.past, retry: retry) { events in
                if events.isEmpty {
                    EmptyStateView(message: "Nothing here yet. Events you go to show up here to rate.")
                } else {
                    ForEach(groups(events), id: \.key) { group in
                        section(title: group.title, events: group.events)
                    }
                }
            }
            Color.clear.frame(height: 24)
        }
    }

    private struct DayGroup {
        let key: String
        let title: String
        var events: [PastEvent]
    }

    /// Consecutive events on the same day, newest first as the server sends them.
    private func groups(_ events: [PastEvent]) -> [DayGroup] {
        var result: [DayGroup] = []
        for event in events {
            let key = env.clock.dayKey(event.date)
            if result.last?.key == key {
                result[result.count - 1].events.append(event)
            } else {
                result.append(DayGroup(key: key, title: env.format.dayTitle(event.date).uppercased(), events: [event]))
            }
        }
        return result
    }

    private func section(title: String, events: [PastEvent]) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            // "THURSDAY, SEP 24" (the prototype's 16pt top margin collapses with the intro's 4pt).
            Text(title)
                .sqFont(13, .semibold, relativeTo: .footnote)
                .tracking(0.4)
                .foregroundStyle(Theme.text3)
                .homeLine(13)
                .padding(.horizontal, Metrics.side)
                .padding(.top, 16)
                .padding(.bottom, 8)
                .accessibilityAddTraits(.isHeader)
            VStack(spacing: 0) {
                ForEach(events) { event in
                    row(event)
                    RowDivider(color: Theme.cream)
                }
            }
            .sqGroupedCard()
            .padding(.horizontal, Metrics.side)
        }
    }

    private func row(_ event: PastEvent) -> some View {
        Button {
            rate(event)
        } label: {
            HStack(spacing: 12) {
                Circle()
                    .fill(event.kind == .group ? Theme.clay : Theme.sage)
                    .frame(width: 10, height: 10)
                VStack(alignment: .leading, spacing: 0) {
                    Text(event.title)
                        .sqFont(15, .semibold)
                        .foregroundStyle(Theme.ink)
                        .homeLine(15)
                    Text(event.subtitle)
                        .sqFont(12, relativeTo: .caption)
                        .foregroundStyle(Theme.text3)
                        .homeLine(12)
                    if let rating = event.rating, !rating.tagLine.isEmpty {
                        Text(rating.tagLine)
                            .sqFont(12, relativeTo: .caption)
                            .foregroundStyle(Theme.text2)
                            .homeLine(12)
                            .padding(.top, 2)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                if let rating = event.rating {
                    StarRow(rating: rating.stars, size: 14, spacing: 1)
                } else {
                    Text("Rate")
                        .sqFont(13, .semibold)
                        .foregroundStyle(Theme.ink)
                        .padding(.horizontal, 12)
                        .frame(height: 30)
                        .background(Theme.sage, in: Capsule())
                }
            }
            .padding(.vertical, 12)
            .padding(.horizontal, 14)
            .contentShape(Rectangle())
        }
        .buttonStyle(.sqPressable)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(accessibilityText(event))
        .accessibilityAddTraits(.isButton)
    }

    private func accessibilityText(_ event: PastEvent) -> String {
        guard let rating = event.rating else { return "Rate \(event.title), \(event.subtitle)" }
        return "\(event.title), \(event.subtitle), rated \(rating.stars) of 5. Edit rating"
    }
}
