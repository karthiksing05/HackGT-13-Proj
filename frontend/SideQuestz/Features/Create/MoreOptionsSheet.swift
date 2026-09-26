import SwiftUI

/// Review › More options (cream sheet): every answer with an Edit pill that jumps back to its step,
/// getting around, pace, group limits for shared plans, and "Regenerate options with these settings".
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
                ForEach([TravelMode.walk, .marta, .rideshare]) { mode in
                    SQChip(label: mode.label, isOn: model.modes.contains(mode), style: Self.modeStyle) {
                        if model.modes.contains(mode) { model.modes.remove(mode) } else { model.modes.insert(mode) }
                    }
                }
            }
            CreateEyebrow(text: "PACE")
            CreateChoiceGrid(options: Pace.allCases.map { ($0, $0.optionLabel) }, selection: model.pace) { model.pace = $0 }
            if model.who != .justMe {
                CreateEyebrow(text: "GROUP")
                HStack(spacing: 8) {
                    groupCard("Lock joining at", env.format.time(model.lockAt))
                    groupCard("Max group size", "\(CreateFlowModel.defaultMaxGroupSize) people")
                }
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
            VStack(alignment: .leading, spacing: 0) {
                Text("More options")
                    .sqFont(20, .bold, relativeTo: .title3)
                    .foregroundStyle(Theme.ink)
                    .createLine(20, relativeTo: .title3)
                    .accessibilityAddTraits(.isHeader)
                Text("Check everything before you start")
                    .sqFont(13)
                    .foregroundStyle(Theme.text3)
                    .createLine(13)
            }
            Spacer(minLength: 0)
            Button("Done") { model.moreOpen = false }
                .buttonStyle(.sqPill)
        }
    }

    // MARK: Answers

    private var rows: [Row] {
        let format = env.format
        let mood = model.selectedTags
        return [
            Row(label: "Start", value: model.start?.name ?? "—", step: 1),
            Row(label: "End", value: model.endPlace?.name ?? "—", step: 1),
            Row(label: "Date", value: format.shortDate(model.date), step: 2),
            Row(label: "Time window", value: "\(format.time(model.startTime)) – back by \(format.time(model.backBy))", step: 2),
            Row(label: "Mood", value: mood.isEmpty ? "Anything" : mood.joined(separator: ", "), step: 3),
            Row(label: "Budget", value: CreateFlowModel.budgetLabels[min(max(model.budget, 0), 3)], step: 3),
            Row(label: "Who's coming", value: model.who.label, step: 3),
            Row(label: "Ride", value: model.ride.summary(openSeats: model.openSeats), step: 1),
        ]
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

    private func groupCard(_ label: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(label)
                .sqFont(12, relativeTo: .caption)
                .foregroundStyle(Theme.text3)
                .createLine(12, relativeTo: .caption)
            Text(value)
                .sqFont(15, .semibold)
                .foregroundStyle(Theme.ink)
                .createLine(15)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.white, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .accessibilityElement(children: .combine)
    }
}
