import SwiftUI
import UIKit

/// Home › Calendar (GUI_PLAN.md §7.5b): day chips from Today, swipeable day panels (6 AM–11 PM),
/// and drag on empty time to plan a window. "+ Plan" is the non-drag alternative.
struct HomeCalendarView: View {
    @Bindable var store: HomeStore
    let pageWidth: CGFloat
    let openItem: (CalendarItem) -> Void
    let retry: () -> Void

    @Environment(AppEnvironment.self) private var env
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        LoadableView(state: store.days, minHeight: 240, retry: retry) { days in
            VStack(alignment: .leading, spacing: 0) {
                chips(days)
                pager(days)
                Text("Drag on empty time to plan a sidequest · swipe for more days")
                    .sqFont(12, relativeTo: .caption)
                    .foregroundStyle(Theme.text3)
                    .homeLine(12)
                    .multilineTextAlignment(.center)
                    .frame(maxWidth: .infinity)
                    .padding(.horizontal, Metrics.side)
                    .padding(.top, 6)
                    .padding(.bottom, 24)
            }
        }
    }

    private func selectedId(in days: [CalendarDay]) -> String? {
        store.selectedDayId ?? days.first?.id
    }

    // MARK: Day chips

    private func chips(_ days: [CalendarDay]) -> some View {
        let selected = selectedId(in: days)
        return ScrollViewReader { proxy in
            ScrollView(.horizontal) {
                HStack(spacing: 8) {
                    ForEach(days) { day in
                        HomeDayChip(day: day, isSelected: day.id == selected) {
                            withAnimation(reduceMotion ? nil : .smooth(duration: 0.35)) { store.selectedDayId = day.id }
                        }
                        .id(day.id)
                    }
                }
                .padding(.horizontal, Metrics.side)
                .padding(.top, 14)
                .padding(.bottom, 10)
            }
            .scrollIndicators(.hidden)
            .sensoryFeedback(.selection, trigger: store.selectedDayId)
            .onChange(of: store.selectedDayId) { _, id in
                // Swiping the panels keeps the selected chip in view.
                guard let id else { return }
                withAnimation(reduceMotion ? nil : .smooth(duration: 0.3)) { proxy.scrollTo(id, anchor: .center) }
            }
        }
    }

    // MARK: Day panels

    private func pager(_ days: [CalendarDay]) -> some View {
        ScrollView(.horizontal) {
            HStack(alignment: .top, spacing: 12) {
                ForEach(days) { day in
                    HomeDayPanel(day: day, store: store, openItem: openItem)
                        .frame(width: max(0, pageWidth - 2 * Metrics.side))
                        .id(day.id)
                }
            }
            .scrollTargetLayout()
        }
        .contentMargins(.horizontal, Metrics.side, for: .scrollContent)
        .scrollTargetBehavior(.viewAligned)
        .scrollPosition(id: $store.selectedDayId)
        .scrollIndicators(.hidden)
        .padding(.bottom, 8)
    }
}

/// 50×66 day chip: weekday ("Today" first), day number, up to 3 kind dots. Selected = sage.
struct HomeDayChip: View {
    let day: CalendarDay
    let isSelected: Bool
    let action: () -> Void
    @Environment(AppEnvironment.self) private var env

    var body: some View {
        Button(action: action) {
            VStack(spacing: 2) {
                Text(env.clock.isToday(day.date) ? "Today" : env.format.weekdayShort(day.date))
                    .sqFont(11, .semibold, relativeTo: .caption2)
                    .homeLine(11)
                    .opacity(0.8)
                Text(env.format.dayNumber(day.date))
                    .sqFont(19, .bold, relativeTo: .title3)
                    .homeLine(19)
                HStack(spacing: 2) {
                    ForEach(kinds, id: \.self) { kind in
                        Circle()
                            .fill(isSelected ? Color.white : kind.palette.dot)
                            .frame(width: 5, height: 5)
                    }
                }
                .frame(height: 5)
            }
            .lineLimit(1)
            .minimumScaleFactor(0.7)
            .foregroundStyle(Theme.ink)
            .frame(width: 50, height: 66)
            .background(isSelected ? Theme.sage : .white, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
            .contentShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        }
        .buttonStyle(.sqPressable)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(accessibilityText)
        .accessibilityAddTraits(isSelected ? [.isButton, .isSelected] : .isButton)
    }

    /// Distinct kinds in the order they first appear (busy grey, sidequest sage, group clay).
    private var kinds: [BlockKind] {
        var seen: [BlockKind] = []
        for item in day.items where !seen.contains(item.kind) { seen.append(item.kind) }
        return Array(seen.prefix(3))
    }

    private var accessibilityText: String {
        let count = day.items.count
        let summary = count == 0 ? "Nothing planned" : "\(count) on your calendar"
        return "\(env.format.longDate(day.date)), \(summary)"
    }
}

/// One day: "Friday, Sep 25" + "Today · 5 on your calendar" + "+ Plan", then the 6 AM–11 PM timeline.
struct HomeDayPanel: View {
    let day: CalendarDay
    let store: HomeStore
    let openItem: (CalendarItem) -> Void

    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router
    @State private var drops = 0

    static let hourHeight: CGFloat = 30
    static let topInset: CGFloat = 10
    static let timelineHeight: CGFloat = 530
    static let firstHour = 6

    private static func y(minutes: Int) -> CGFloat {
        topInset + CGFloat(minutes - firstHour * 60) * hourHeight / 60
    }

    var body: some View {
        VStack(spacing: 0) {
            header
            timeline
        }
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
        .sensoryFeedback(.impact(weight: .medium), trigger: drops)
        // A light tick for every 15 minutes while drawing; the release gets the medium impact.
        .sensoryFeedback(.selection, trigger: draggingMinute) { old, new in old != nil && new != nil }
    }

    private var header: some View {
        HStack(alignment: .center, spacing: 8) {
            VStack(alignment: .leading, spacing: 0) {
                Text(env.format.dayTitle(day.date))
                    .sqFont(17, .bold)
                    .foregroundStyle(Theme.ink)
                    .homeLine(17)
                Text(subtitle)
                    .sqFont(12, relativeTo: .caption)
                    .foregroundStyle(Theme.text3)
                    .homeLine(12)
            }
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(.isHeader)
            Spacer(minLength: 0)
            Button("+ Plan") {
                router.openCreate(CreateDraft(date: day.date))
            }
            .buttonStyle(.sqTintPill)
            .padding(.vertical, -6)
            .accessibilityLabel("Plan a sidequest on \(env.format.longDate(day.date))")
        }
        .padding(.top, 14)
        .padding(.horizontal, 14)
        .padding(.bottom, 6)
    }

    private var subtitle: String {
        let count = day.items.count
        let summary = count == 0 ? "Nothing planned" : "\(count) on your calendar"
        return env.clock.isToday(day.date) ? "Today · \(summary)" : summary
    }

    // MARK: Timeline

    private var timeline: some View {
        ZStack(alignment: .topLeading) {
            ForEach(Array(stride(from: Self.firstHour, through: 22, by: 2)), id: \.self) { hour in
                hourRow(hour)
                    .offset(y: Self.y(minutes: hour * 60) - 6)
            }
            HomeTimeDragSurface(
                onTap: { tap(at: $0) },
                onDrag: { drag(from: $0, to: $1) },
                onDragEnd: { endDrag() }
            )
            .frame(maxWidth: .infinity)
            .frame(height: Self.timelineHeight)
            .accessibilityHidden(true)
            ForEach(day.items) { item in
                if let span = visibleSpan(item) {
                    block(item, span: span)
                }
            }
            if let selection = store.planSelection, selection.dayId == day.id {
                selectionBox(selection)
                if !selection.isDragging {
                    planBar(selection)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .frame(height: Self.timelineHeight, alignment: .top)
    }

    private func hourRow(_ hour: Int) -> some View {
        HStack(spacing: 8) {
            Text(env.format.hourLabel(hour))
                .sqFont(10, relativeTo: .caption2)
                .foregroundStyle(Theme.text3)
                .lineLimit(1)
                .fixedSize()
                .frame(width: 44, alignment: .trailing)
            Rectangle().fill(Theme.hourLine).frame(height: 1)
        }
        .frame(height: 12)
        .padding(.trailing, 12)
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }

    private func minutes(_ date: Date) -> Int {
        Int((date.timeIntervalSince(env.clock.startOfDay(day.date)) / 60).rounded())
    }

    /// The part of an entry inside 6 AM – 11:20 PM (the bottom of the panel), in minutes.
    private func visibleSpan(_ item: CalendarItem) -> ClosedRange<Int>? {
        let lo = max(minutes(item.start), HomePlanSelection.earliest)
        let hi = min(minutes(item.end), Self.firstHour * 60 + Int((Self.timelineHeight - Self.topInset) / Self.hourHeight * 60))
        return hi > lo ? lo...hi : nil
    }

    private func block(_ item: CalendarItem, span: ClosedRange<Int>) -> some View {
        let palette = item.kind.palette
        let top = Self.y(minutes: span.lowerBound)
        let height = max(20, Self.y(minutes: span.upperBound) - top - 2)
        let time = env.format.range(item.start, item.end)
        let faces = Array((item.people + item.interested).prefix(4))
        let shape = RoundedRectangle(cornerRadius: 8, style: .continuous)
        return Button {
            openItem(item)
        } label: {
            HStack(alignment: .top, spacing: 6) {
                Text("\(item.title) · \(time)")
                    .sqFont(12, .semibold)
                    .homeLine(12, 1.25)
                    .frame(maxWidth: .infinity, alignment: .topLeading)
                if !faces.isEmpty {
                    HomeAvatarStack(people: faces, size: 20, fontSize: 8)
                }
            }
            .foregroundStyle(palette.text)
            // 1pt border + the prototype's 3 / 8 padding.
            .padding(.vertical, 4)
            .padding(.horizontal, 9)
            .frame(maxWidth: .infinity, alignment: .topLeading)
            .frame(height: height, alignment: .top)
            .background(palette.background, in: shape)
            .clipShape(shape)
            .overlay { shape.strokeBorder(palette.border, lineWidth: 1) }
            .contentShape(shape)
        }
        .buttonStyle(.sqPressable)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(item.title), \(time), \(palette.label)")
        .accessibilityHint("Opens details")
        .accessibilityAddTraits(.isButton)
        .padding(.leading, 58)
        .padding(.trailing, 12)
        .offset(y: top)
    }

    // MARK: Drag to plan

    private func selectionBox(_ selection: HomePlanSelection) -> some View {
        let top = Self.y(minutes: selection.lower)
        let height = max(14, CGFloat(selection.upper - selection.lower) * Self.hourHeight / 60)
        let shape = RoundedRectangle(cornerRadius: 8, style: .continuous)
        return VStack(alignment: .leading, spacing: 0) {
            Text("New sidequest")
                .sqFont(12, .bold)
                .foregroundStyle(Theme.sageInk)
                .homeLine(12)
            Text(label(selection))
                .sqFont(12)
                .foregroundStyle(Theme.ink)
                .homeLine(12)
        }
        .lineLimit(1)
        // 2pt border + the prototype's 4 / 8 padding.
        .padding(.vertical, 6)
        .padding(.horizontal, 10)
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .frame(height: height, alignment: .top)
        .background(Theme.sageTint, in: shape)
        .overlay { shape.strokeBorder(Theme.sage, lineWidth: 2) }
        .padding(.leading, 58)
        .padding(.trailing, 12)
        .offset(y: top)
        .allowsHitTesting(false)
        .accessibilityElement(children: .combine)
    }

    private func planBar(_ selection: HomePlanSelection) -> some View {
        let top = Self.y(minutes: selection.lower)
        let height = max(14, CGFloat(selection.upper - selection.lower) * Self.hourHeight / 60)
        return HStack(spacing: 6) {
            Button("Plan this window") { plan(selection) }
                .buttonStyle(.sq(fill: Theme.sage, foreground: Theme.ink, height: 36, radius: 10, fontSize: 14))
                .shadow(color: .black.opacity(0.15), radius: 6, x: 0, y: 4)
                .contentShape(Rectangle().inset(by: -4))
                .accessibilityLabel("Plan this window, \(label(selection))")
            Button {
                withAnimation(.easeOut(duration: 0.15)) { store.planSelection = nil }
            } label: {
                HomeIcon(glyph: .close, size: 14, strokeWidth: 2.6)
                    .foregroundStyle(.white)
                    .frame(width: 36, height: 36)
                    .background(Theme.ink, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                    .contentShape(Rectangle().inset(by: -4))
            }
            .buttonStyle(.sqPressable)
            .accessibilityLabel("Clear selection")
        }
        .padding(.leading, 58)
        .padding(.trailing, 12)
        .offset(y: min(Self.timelineHeight - 40, top + height + 6))
        .transition(.opacity)
    }

    private func label(_ selection: HomePlanSelection) -> String {
        env.format.range(date(selection.lower), date(selection.upper))
    }

    private func date(_ minutes: Int) -> Date {
        env.clock.on(day.date, minutes / 60, minutes % 60)
    }

    /// The minute under `y`, snapped to 15 and clamped to 6 AM–11 PM.
    private func minute(at y: CGFloat) -> Int {
        let steps = ((y - Self.topInset) / Self.hourHeight * 60 / 15).rounded()
        let value = Int(steps) * 15 + Self.firstHour * 60
        return min(max(value, HomePlanSelection.earliest), HomePlanSelection.latest)
    }

    private var draggingMinute: Int? {
        guard let selection = store.planSelection, selection.dayId == day.id, selection.isDragging else { return nil }
        return selection.current
    }

    /// A tap alone selects one hour.
    private func tap(at y: CGFloat) {
        let start = minute(at: y)
        store.planSelection = HomePlanSelection(dayId: day.id, anchor: start, current: start, isDragging: false).finished()
        drops += 1
    }

    private func drag(from startY: CGFloat, to y: CGFloat) {
        if var selection = store.planSelection, selection.dayId == day.id, selection.isDragging {
            let current = minute(at: y)
            if current != selection.current {
                selection.current = current
                store.planSelection = selection
            }
        } else {
            store.planSelection = HomePlanSelection(dayId: day.id, anchor: minute(at: startY), current: minute(at: y), isDragging: true)
        }
    }

    private func endDrag() {
        guard let selection = store.planSelection, selection.dayId == day.id, selection.isDragging else { return }
        store.planSelection = selection.finished()
        drops += 1
    }

    private func plan(_ selection: HomePlanSelection) {
        router.openCreate(CreateDraft(date: day.date, start: date(selection.lower), end: date(selection.upper)))
        store.planSelection = nil
    }
}

/// Transparent UIKit surface under the calendar blocks that turns a vertical press-and-drag (or a
/// tap) into a planning window. The direction is decided from the first movement: a horizontal
/// pan fails right away so the day pager pages; a vertical one draws the selection, and the
/// enclosing scroll views wait for it, so the page doesn't scroll while you draw.
struct HomeTimeDragSurface: UIViewRepresentable {
    var onTap: (CGFloat) -> Void
    var onDrag: (_ startY: CGFloat, _ currentY: CGFloat) -> Void
    var onDragEnd: () -> Void

    func makeCoordinator() -> Coordinator { Coordinator(surface: self) }

    func makeUIView(context: Context) -> UIView {
        let view = UIView()
        view.backgroundColor = .clear
        view.isAccessibilityElement = false

        let pan = UIPanGestureRecognizer(target: context.coordinator, action: #selector(Coordinator.handlePan(_:)))
        pan.maximumNumberOfTouches = 1
        pan.delegate = context.coordinator
        view.addGestureRecognizer(pan)

        let tap = UITapGestureRecognizer(target: context.coordinator, action: #selector(Coordinator.handleTap(_:)))
        view.addGestureRecognizer(tap)
        return view
    }

    func updateUIView(_ view: UIView, context: Context) {
        context.coordinator.surface = self
    }

    final class Coordinator: NSObject, UIGestureRecognizerDelegate {
        var surface: HomeTimeDragSurface
        private var startY: CGFloat = 0

        init(surface: HomeTimeDragSurface) {
            self.surface = surface
        }

        @objc func handleTap(_ recognizer: UITapGestureRecognizer) {
            surface.onTap(recognizer.location(in: recognizer.view).y)
        }

        @objc func handlePan(_ recognizer: UIPanGestureRecognizer) {
            let y = recognizer.location(in: recognizer.view).y
            switch recognizer.state {
            case .began:
                startY = y - recognizer.translation(in: recognizer.view).y
                surface.onDrag(startY, y)
            case .changed:
                surface.onDrag(startY, y)
            case .ended, .cancelled, .failed:
                surface.onDragEnd()
            default:
                break
            }
        }

        /// Vertical first movement → draw; horizontal → fail so the pager scrolls.
        func gestureRecognizerShouldBegin(_ recognizer: UIGestureRecognizer) -> Bool {
            guard let pan = recognizer as? UIPanGestureRecognizer else { return true }
            let translation = pan.translation(in: pan.view)
            if translation != .zero { return abs(translation.y) > abs(translation.x) }
            let velocity = pan.velocity(in: pan.view)
            return abs(velocity.y) > abs(velocity.x)
        }

        /// Enclosing scroll views wait until the direction is decided.
        func gestureRecognizer(_ recognizer: UIGestureRecognizer, shouldBeRequiredToFailBy other: UIGestureRecognizer) -> Bool {
            recognizer is UIPanGestureRecognizer && other.view is UIScrollView
        }
    }
}
