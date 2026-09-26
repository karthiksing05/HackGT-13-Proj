import SwiftUI

/// Create › When: date, start, "Back at end by", a plan summary and the day's calendar.
/// No auto-picked slots.
///
/// Motion: the summary's times roll when you change them; the calendar section grows and shrinks
/// with its chevron turning, shows a skeleton of its rows while the day loads, then fades them in.
struct CreateWhenStep: View {
    @Bindable var model: CreateFlowModel
    @Environment(AppEnvironment.self) private var env
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            CreateStepTitle("When are you free?")
            // Changes animate everywhere they show: the summary's times roll, its box eases to a new height.
            CreateFieldCard(label: "Date") {
                picker("Date", selection: Binding(get: { model.date }, set: { date in withMotion { model.setDate(date) } }),
                       components: .date)
            }
            HStack(alignment: .top, spacing: 10) {
                CreateFieldCard(label: "Start") {
                    picker("Start", selection: Binding(get: { model.startTime }, set: { time in withMotion { model.setStartTime(time) } }),
                           components: .hourAndMinute)
                }
                CreateFieldCard(label: "Back at end by") {
                    picker("Back at end by", selection: Binding(get: { model.backBy }, set: { time in withMotion { model.setBackBy(time) } }),
                           components: .hourAndMinute)
                }
            }
            summary
            calendarCard
        }
        .task(id: CalendarKey(open: model.calendarOpen, day: env.clock.dayKey(model.date))) {
            if model.calendarOpen { await model.loadCalendar() }
        }
    }

    private struct CalendarKey: Hashable {
        var open: Bool
        var day: String
    }

    /// Native compact picker. Its capsule is taller than the prototype's 17pt input line, so it
    /// overlaps the card padding a little to keep the card at the prototype's height.
    private func picker(_ label: String, selection: Binding<Date>, components: DatePickerComponents) -> some View {
        DatePicker(label, selection: selection, displayedComponents: components)
            .labelsHidden()
            .datePickerStyle(.compact)
            .tint(Theme.sageInk)
            .environment(\.timeZone, env.clock.timeZone)
            .padding(.vertical, -5)
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    // MARK: Summary

    /// Wraps like the prototype; the times roll to their new values.
    private var summary: some View {
        let format = env.format
        let startName = model.start?.name ?? "your start"
        let endName = model.endPlace?.name ?? "your end"
        let text = "Leave \(startName) at \(format.time(model.startTime)). We plan backwards so you're at \(endName) by \(format.time(model.backBy))."
        return HStack(alignment: .top, spacing: 10) {
            CreatePinGlyph(size: 18)
                .foregroundStyle(Theme.sageInk)
                .padding(.top, 1)
            CreateLiveText(text: text, size: 14, lineHeight: 1.4)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .background(Theme.sageTint, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(text)
    }

    // MARK: Calendar

    private var calendarCard: some View {
        VStack(spacing: 0) {
            Button {
                withMotion(Motion.gentle) { model.calendarOpen.toggle() }
            } label: {
                HStack(spacing: 10) {
                    CalendarGlyph(size: 20)
                        .foregroundStyle(Theme.sageInk)
                    Text("Your calendar · \(env.format.shortDate(model.date))")
                        .sqFont(15, .semibold)
                        .foregroundStyle(Theme.ink)
                        .lineLimit(1)
                        .minimumScaleFactor(0.85)
                        .sqNumeric()
                        .frame(maxWidth: .infinity, alignment: .leading)
                    if model.calendarRefreshing {
                        LoadingDots(color: Theme.text3, dotSize: 4.5)
                            .sqTransition(.pop)
                    }
                    CreateChevron(direction: .down, size: 16)
                        .foregroundStyle(Theme.ink)
                        .rotationEffect(.degrees(model.calendarOpen ? 180 : 0))
                }
                .padding(.horizontal, 14)
                .frame(height: 50)
                .contentShape(Rectangle())
                .animation(Motion.quick, value: model.calendarRefreshing)
            }
            .buttonStyle(.sqPressable)
            .accessibilityValue(model.calendarOpen ? "Expanded" : "Collapsed")
            if model.calendarOpen {
                VStack(spacing: 0) {
                    RowDivider()
                    calendarBody
                }
                .transition(reduceMotion ? .opacity : .opacity.combined(with: .offset(y: -8)))
            }
        }
        .background(.white)
        .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        // Skeleton → rows (or empty / error) cross-fade while the card eases to its new height.
        .animation(Motion.standard, value: calendarState)
    }

    /// A VStack, not a ZStack: a ZStack would place the rows with a fixed height, and a VStack
    /// dividing that height among its rows shrinks the time column (it has a minimum scale).
    /// Removed views don't take space, so the old and new content still overlap as they cross-fade.
    private var calendarBody: some View {
        VStack(spacing: 0) {
            switch model.calendarItems {
            case .loading:
                CreateCalendarSkeleton()
                    .transition(.opacity)
            case .failed(let message):
                ErrorStateView(message: message, minHeight: 120) {
                    Task { await model.loadCalendar() }
                }
                .transition(.opacity)
            case .loaded(let items):
                Group {
                    if items.isEmpty {
                        Text("Nothing on your calendar this day.")
                            .sqFont(14)
                            .foregroundStyle(Theme.text3)
                            .createLine(14)
                            .padding(14)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    } else {
                        VStack(spacing: 0) {
                            ForEach(items) { item in
                                calendarRow(item)
                                RowDivider(color: Theme.cream)
                            }
                        }
                    }
                }
                .transition(.opacity)
            }
        }
    }

    /// What the calendar body is showing, for animating between loading, error and items.
    private var calendarState: [String] {
        switch model.calendarItems {
        case .loading: ["loading"]
        case .failed(let message): ["failed", message]
        case .loaded(let items): items.map { "\($0.id)" }
        }
    }

    private func calendarRow(_ item: CalendarItem) -> some View {
        let time = env.format.range(item.start, item.end)
        return HStack(spacing: 12) {
            Circle()
                .fill(item.kind.palette.dot)
                .frame(width: 10, height: 10)
            Text(time)
                .sqFont(12, relativeTo: .caption)
                .foregroundStyle(Theme.text3)
                .lineLimit(1)
                .minimumScaleFactor(0.85)
                .frame(width: 118, alignment: .leading)
            CreateWrapText(text: item.title, face: .system(.semibold), size: 14)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(time), \(item.title)")
    }
}

/// Three calendar rows (dot · time column · title) shimmering while the day loads.
private struct CreateCalendarSkeleton: View {
    private static let titleWidths: [CGFloat] = [96, 124, 150]

    var body: some View {
        VStack(spacing: 0) {
            ForEach(Array(Self.titleWidths.enumerated()), id: \.offset) { index, titleWidth in
                HStack(spacing: 12) {
                    Circle()
                        .fill(Theme.skeleton)
                        .frame(width: 10, height: 10)
                    SkeletonBlock(width: index == 1 ? 76 : 92, height: 10)
                        .frame(width: 118, alignment: .leading)
                    SkeletonBlock(width: titleWidth, height: 12)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .padding(.horizontal, 14)
                .frame(height: 41)
                RowDivider(color: Theme.cream)
            }
        }
        .sqShimmer()
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading")
    }
}
