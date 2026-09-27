import SwiftUI

/// Review › More options (cream sheet): every answer with an Edit pill that jumps back to its step,
/// getting around, pace, the group's lock time and size for shared plans, and "Regenerate options
/// with these settings".
struct CreateMoreOptionsSheet: View {
    @Bindable var model: CreateFlowModel
    /// Jumps to a step (the flow closes the sheet first).
    let edit: (Int) -> Void

    @Environment(AppEnvironment.self) private var env

    private struct Row: Identifiable {
        let label: String
        let value: String
        let step: Int
        var id: String { label }
    }

    var body: some View {
        SheetScaffold(spacing: 12, grabberColor: Theme.mutedBorder, shrinksToFit: true) {
            header
            summaryCard
            CreateEyebrow(text: "GETTING AROUND")
            HStack(spacing: 8) {
                ForEach(CreateFlowModel.travelModes) { mode in
                    SQChip(label: mode.label, isOn: model.modes.contains(mode), style: Self.modeStyle) {
                        model.toggleMode(mode)
                    }
                }
            }
            CreateEyebrow(text: "PACE")
            CreateChoiceGrid(options: Pace.allCases.map { ($0, $0.optionLabel) }, selection: model.pace) { model.pace = $0 }
            if model.who != .justMe {
                CreateEyebrow(text: "GROUP")
                HStack(spacing: 8) {
                    groupCard("Lock joining at") { lockPicker }
                    groupCard("Max group size") { CreateGroupSizeStepper(model: model) }
                        .accessibilityElement(children: .ignore)
                        .accessibilityLabel("Max group size")
                        .accessibilityValue("\(model.maxGroupSize) people")
                        .accessibilityAdjustableAction { direction in
                            switch direction {
                            case .increment: withMotion(Motion.quick) { model.changeMaxGroupSize(by: 1) }
                            case .decrement: withMotion(Motion.quick) { model.changeMaxGroupSize(by: -1) }
                            @unknown default: break
                            }
                        }
                }
                // Both cards take the taller one's height.
                .fixedSize(horizontal: false, vertical: true)
            }
            Button("Regenerate options with these settings") {
                model.moreOpen = false
                Task { await model.regenerate() }
            }
            .buttonStyle(.sq(fill: .white, foreground: Theme.sageInk, border: Theme.sage, borderWidth: 1.5, height: 46, fontSize: 15))
        }
    }

    private static var modeStyle: ChipStyle {
        var style = ChipStyle.soft
        style.height = 40
        style.fullWidth = true
        return style
    }

    // MARK: Header

    private var header: some View {
        HStack(spacing: 12) {
            Text("More options")
                .sqFont(20, .bold, relativeTo: .title3)
                .foregroundStyle(Theme.ink)
                .createLine(20, relativeTo: .title3)
                .accessibilityAddTraits(.isHeader)
            Spacer(minLength: 0)
            Button("Done") { model.moreOpen = false }
                .buttonStyle(.sqPill)
        }
    }

    // MARK: Answers

    /// The prototype's rows, plus the must-see picks when there are some.
    private var rows: [Row] {
        let format = env.format
        var rows = [
            Row(label: "Start", value: model.start?.name ?? "—", step: 1),
            Row(label: "End", value: model.endPlace?.name ?? "—", step: 1),
            Row(label: "Date", value: format.shortDate(model.date), step: 2),
            Row(label: "Time window", value: "\(format.time(model.startTime)) – back by \(format.time(model.backBy))", step: 2),
            Row(label: "Mood", value: model.moodSummary, step: 3),
            Row(label: "Budget", value: CreateFlowModel.budgetLabels[min(max(model.budget, 0), 3)], step: 3),
            Row(label: "Who's coming", value: model.who.label, step: 3),
            Row(label: "Ride", value: model.ride.summary(openSeats: model.openSeats), step: 1),
        ]
        if !model.mustSee.isEmpty {
            rows.insert(Row(label: "Must-see", value: model.mustSee.map(\.title).joined(separator: ", "), step: 3), at: rows.count - 1)
        }
        return rows
    }

    /// Like the prototype, the card is what gets shorter when the sheet hits 740pt; it scrolls inside
    /// itself so Who's coming and Ride stay reachable.
    private var summaryCard: some View {
        ScrollView {
            summaryRows
        }
        .scrollBounceBehavior(.basedOnSize)
        .background(.white)
        .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        .sheetShrinks()
    }

    private var summaryRows: some View {
        VStack(spacing: 0) {
            ForEach(rows) { row in
                HStack(spacing: 10) {
                    VStack(alignment: .leading, spacing: 0) {
                        Text(row.label)
                            .sqFont(12, relativeTo: .caption)
                            .foregroundStyle(Theme.text3)
                            .createLine(12, relativeTo: .caption)
                        Text(row.value)
                            .sqFont(15, .semibold)
                            .foregroundStyle(Theme.ink)
                            .lineLimit(1)
                            .truncationMode(.tail)
                            .createLine(15)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .accessibilityElement(children: .combine)
                    CreatePillButton(title: "Edit", accessibilityLabel: "Edit \(row.label)") {
                        edit(row.step)
                    }
                }
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                RowDivider(color: Theme.cream)
            }
        }
    }

    // MARK: Group

    /// A white card: 12pt `text3` label over its control (the label is read by the control itself).
    private func groupCard<Content: View>(_ label: String, @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label)
                .sqFont(12, relativeTo: .caption)
                .foregroundStyle(Theme.text3)
                .lineLimit(1)
                .minimumScaleFactor(0.85)
                .createLine(12, relativeTo: .caption)
                .accessibilityHidden(true)
            content()
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .background(.white, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
    }

    /// Native compact time picker, like Create › When: from now (or the start of the plan's day)
    /// up to when you leave.
    private var lockPicker: some View {
        DatePicker("Lock joining at", selection: Binding(get: { model.lockAt }, set: { model.setLockAt($0) }),
                   in: model.lockRange, displayedComponents: .hourAndMinute)
            .labelsHidden()
            .datePickerStyle(.compact)
            .tint(Theme.sageInk)
            .environment(\.timeZone, env.clock.timeZone)
            .padding(.vertical, -5)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

// MARK: - Group size

/// "6 people" with − / + (30pt cream circles, 44pt touch targets that don't overlap). The number
/// rolls; the ends of the range grey their button out. VoiceOver adjusts the whole card instead
/// (see the sheet).
private struct CreateGroupSizeStepper: View {
    let model: CreateFlowModel

    private static let buttonSize: CGFloat = 30

    var body: some View {
        HStack(spacing: 6) {
            Text("\(model.maxGroupSize) people")
                .sqFont(15, .semibold)
                .foregroundStyle(Theme.ink)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
                .sqNumeric()
                .createLine(15)
            Spacer(minLength: 0)
            HStack(spacing: Metrics.minTouch - Self.buttonSize) {
                button(-1)
                button(1)
            }
            // As tall as the time picker's line next to it, so both values sit level.
            .padding(.vertical, -2.5)
        }
        .sensoryFeedback(.selection, trigger: model.maxGroupSize)
    }

    private func button(_ delta: Int) -> some View {
        let sizes = CreateFlowModel.groupSizes
        let enabled = delta < 0 ? model.maxGroupSize > sizes.lowerBound : model.maxGroupSize < sizes.upperBound
        let reach = (Metrics.minTouch - Self.buttonSize) / 2
        return Button {
            withMotion(Motion.quick) { model.changeMaxGroupSize(by: delta) }
        } label: {
            CreateStepGlyph(plus: delta > 0)
                .foregroundStyle(enabled ? Theme.ink : Theme.mutedStar)
                .frame(width: Self.buttonSize, height: Self.buttonSize)
                .background(Theme.cream, in: Circle())
                // A 44pt target that doesn't change the layout.
                .padding(reach)
                .contentShape(Rectangle())
                .padding(-reach)
        }
        .buttonStyle(.plain)
        .disabled(!enabled)
        .animation(Motion.quick, value: enabled)
        .accessibilityLabel(delta > 0 ? "More people" : "Fewer people")
    }
}

/// A − or + drawn with round-capped 2pt strokes (11pt).
private struct CreateStepGlyph: View {
    let plus: Bool

    var body: some View {
        Path { p in
            p.move(to: CGPoint(x: 0, y: 5.5)); p.addLine(to: CGPoint(x: 11, y: 5.5))
            if plus {
                p.move(to: CGPoint(x: 5.5, y: 0)); p.addLine(to: CGPoint(x: 5.5, y: 11))
            }
        }
        .stroke(style: StrokeStyle(lineWidth: 2, lineCap: .round))
        .frame(width: 11, height: 11)
    }
}
