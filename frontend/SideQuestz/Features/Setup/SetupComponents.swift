import SwiftUI

/// Step title (Mono 28 ExtraBold) + an optional 15pt `text2` subtitle. Both sit in the step's
/// 12pt column.
struct SetupHeading: View {
    let title: String
    var subtitle: String? = nil

    var body: some View {
        AuthStepTitle(text: title)
        if let subtitle {
            Text(subtitle)
                .sqFont(15)
                .foregroundStyle(Theme.text2)
                .authLineHeight(1.35, size: 15)
                .fixedSize(horizontal: false, vertical: true)
        }
    }
}

/// Stands in for the initials before a name is typed: a person glyph in the initials color.
struct SetupPersonGlyph: View {
    /// The avatar's diameter.
    let size: CGFloat
    let color: Color

    var body: some View {
        Image(systemName: "person.fill")
            .font(.system(size: size * 0.42))
            .foregroundStyle(color)
            .accessibilityHidden(true)
    }
}

/// "WHO DO YOU USUALLY GO WITH?" — 13 Semibold, tracking 0.4, `text3`.
struct SetupEyebrow: View {
    let text: String

    var body: some View {
        Text(text)
            .eyebrowStyle()
            .authLineHeight(1.35, size: 13)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityAddTraits(.isHeader)
    }
}

/// Single-select chips in equal columns (8pt gaps), e.g. Solo / Small group / Big group.
struct SetupChoiceGrid<Option: Identifiable & Hashable>: View {
    let options: [Option]
    let columns: Int
    @Binding var selection: Option
    var style: ChipStyle = .gridPlain
    let label: (Option) -> String

    var body: some View {
        VStack(spacing: 8) {
            ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                HStack(spacing: 8) {
                    ForEach(row) { option in
                        SQChip(label: label(option), isOn: option == selection, style: style) {
                            selection = option
                        }
                        .frame(maxWidth: .infinity)
                    }
                }
            }
        }
    }

    private var rows: [[Option]] {
        stride(from: 0, to: options.count, by: columns).map { Array(options[$0..<min($0 + columns, options.count)]) }
    }
}

/// White rounded card that clips its rows (Setup likes, calendars).
struct SetupCard<Content: View>: View {
    var radius: CGFloat = Metrics.cardRadius
    @ViewBuilder var content: Content

    var body: some View {
        VStack(spacing: 0) { content }
            .frame(maxWidth: .infinity)
            .background(.white)
            .clipShape(RoundedRectangle(cornerRadius: radius, style: .continuous))
    }
}
