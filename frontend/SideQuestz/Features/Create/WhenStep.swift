import SwiftUI

/// Create › When: date, start, "Back at end by", a plan summary and the day's calendar.
/// No auto-picked slots.
struct CreateWhenStep: View {
    @Bindable var model: CreateFlowModel
    @Environment(AppEnvironment.self) private var env

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            CreateStepTitle("When are you free?")
            CreateFieldCard(label: "Date") {
                picker("Date", selection: Binding(get: { model.date }, set: { model.setDate($0) }), components: .date)
            }
            HStack(alignment: .top, spacing: 10) {
                CreateFieldCard(label: "Start") {
                    picker("Start", selection: Binding(get: { model.startTime }, set: { model.setStartTime($0) }), components: .hourAndMinute)
                }
                CreateFieldCard(label: "Back at end by") {
                    picker("Back at end by", selection: Binding(get: { model.backBy }, set: { model.setBackBy($0) }), components: .hourAndMinute)
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

    private var summary: some View {
        let format = env.format
        let startName = model.start?.name ?? "your start"
        let endName = model.endPlace?.name ?? "your end"
        let text = "Leave \(startName) at \(format.time(model.startTime)). We plan backwards so you're at \(endName) by \(format.time(model.backBy))."
        return HStack(alignment: .top, spacing: 10) {
            CreatePinGlyph(size: 18)
                .foregroundStyle(Theme.sageInk)
                .padding(.top, 1)
            CreateWrapText(text: text, size: 14, lineHeight: 1.4)
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
                withAnimation(.easeInOut(duration: 0.2)) { model.calendarOpen.toggle() }
            } label: {
                HStack(spacing: 10) {
                    CalendarGlyph(size: 20)
                        .foregroundStyle(Theme.sageInk)
                    Text("Your calendar · \(env.format.shortDate(model.date))")
                        .sqFont(15, .semibold)
                        .foregroundStyle(Theme.ink)
                        .lineLimit(1)
                        .minimumScaleFactor(0.85)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    CreateChevron(direction: model.calendarOpen ? .up : .down, size: 16)
                        .foregroundStyle(Theme.ink)
                }
                .padding(.horizontal, 14)
                .frame(height: 50)
                .contentShape(Rectangle())
            }
            .buttonStyle(.sqPressable)
            .accessibilityValue(model.calendarOpen ? "Expanded" : "Collapsed")
            if model.calendarOpen {
                RowDivider()
                calendarBody
                    .transition(.opacity)
            }
        }
        .background(.white)
        .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
    }

    @ViewBuilder private var calendarBody: some View {
        switch model.calendarItems {
        case .loading:
            LoadingStateView(minHeight: 96)
        case .failed(let message):
            ErrorStateView(message: message, minHeight: 120) {
                Task { await model.loadCalendar() }
            }
        case .loaded(let items):
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
