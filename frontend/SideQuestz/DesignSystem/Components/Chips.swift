import SwiftUI

/// Colors and sizes for a selectable chip. Presets cover every chip in the prototype.
struct ChipStyle {
    var height: CGFloat = 36
    /// nil = pill (height / 2)
    var radius: CGFloat? = nil
    var fontSize: CGFloat = 14
    var horizontalPadding: CGFloat = 14
    var onFill: Color = Theme.sage
    var onText: Color = Theme.ink
    var onBorder: Color? = Theme.sage
    var offFill: Color = .white
    var offText: Color = Theme.ink
    var offBorder: Color? = Theme.lineStrong
    /// Grid chips stretch to their column.
    var fullWidth = false

    /// Quick picks / pills: 36 tall, radius 18, sage when on.
    static let tag = ChipStyle()
    /// Setup money / forum filters / paid-by: 36 tall, radius 12, 1pt border, stretch to column.
    static let grid = ChipStyle(radius: 12, horizontalPadding: 12, fullWidth: true)
    /// Borderless grid options (Budget, Who's coming, Pace, Company): 40 tall, radius 12.
    static let gridPlain = ChipStyle(height: 40, radius: 12, horizontalPadding: 8, onBorder: nil, offBorder: nil, fullWidth: true)
    /// Soft selection (tint + sageInk + sage border): rate tags, split members, getting-around modes.
    static let soft = ChipStyle(radius: 12, horizontalPadding: 12, onFill: Theme.sageTint, onText: Theme.sageInk, onBorder: Theme.sage)
    /// Rate sheet "What stood out?": 34 tall pills, 13 Semibold, soft selection.
    static let rateTag = ChipStyle(height: 34, fontSize: 13, horizontalPadding: 12, onFill: Theme.sageTint, onText: Theme.sageInk, onBorder: Theme.sage)
    /// Forum All / Open plans / Free now: 32 tall, ink when on.
    static let dark = ChipStyle(height: 32, fontSize: 13, horizontalPadding: 11, onFill: Theme.ink, onText: .white, onBorder: Theme.ink)
    /// Cream-off grid chips (Forum radius, Open seats): no border.
    static let creamGrid = ChipStyle(height: 38, radius: 12, horizontalPadding: 8, onBorder: nil, offFill: Theme.cream, offBorder: nil, fullWidth: true)
}

/// A selectable chip. Light haptic on selection.
struct SQChip: View {
    let label: String
    let isOn: Bool
    var style: ChipStyle = .tag
    var showsCheck = false
    let action: () -> Void

    var body: some View {
        let radius = style.radius ?? style.height / 2
        Button(action: action) {
            HStack(spacing: 5) {
                if showsCheck && isOn {
                    CheckGlyph(lineWidth: 2.6).frame(width: 14, height: 14)
                }
                Text(label).lineLimit(1).minimumScaleFactor(0.85)
            }
            .sqFont(style.fontSize, .semibold)
            .foregroundStyle(isOn ? style.onText : style.offText)
            .padding(.horizontal, style.horizontalPadding)
            .frame(maxWidth: style.fullWidth ? .infinity : nil)
            .frame(height: style.height)
            .background(isOn ? style.onFill : style.offFill, in: RoundedRectangle(cornerRadius: radius, style: .continuous))
            .overlay {
                if let border = isOn ? style.onBorder : style.offBorder {
                    RoundedRectangle(cornerRadius: radius, style: .continuous).strokeBorder(border, lineWidth: 1)
                }
            }
            .contentShape(RoundedRectangle(cornerRadius: radius, style: .continuous))
        }
        .buttonStyle(.plain)
        .animation(Motion.quick, value: isOn)
        .accessibilityAddTraits(isOn ? .isSelected : [])
        .sensoryFeedback(.selection, trigger: isOn)
    }
}

/// Two-line selectable card (Create › Where: "Walkable / ≤ 15 min", ride choices).
struct OptionCard: View {
    let title: String
    let subtitle: String
    let isOn: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            VStack(spacing: 2) {
                Text(title).sqFont(15, .semibold)
                Text(subtitle).sqFont(12)
            }
            .lineLimit(1)
            .minimumScaleFactor(0.8)
            .foregroundStyle(Theme.ink)
            .frame(maxWidth: .infinity)
            .padding(.vertical, 10)
            .padding(.horizontal, 6)
            .background(isOn ? Theme.sage : .white, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
            .contentShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
        }
        .buttonStyle(.plain)
        .animation(Motion.quick, value: isOn)
        .accessibilityAddTraits(isOn ? .isSelected : [])
        .sensoryFeedback(.selection, trigger: isOn)
    }
}

/// Small label chip (not interactive): "Friend", "Open plan", kind chips, share chips.
struct TagLabel: View {
    let text: String
    var fill: Color = Theme.cream
    var foreground: Color = Theme.text2
    var fontSize: CGFloat = 12
    var horizontalPadding: CGFloat = 8
    var verticalPadding: CGFloat = 3
    var radius: CGFloat = 8

    var body: some View {
        Text(text)
            .sqFont(fontSize, .semibold)
            .foregroundStyle(foreground)
            .lineLimit(1)
            .padding(.horizontal, horizontalPadding)
            .padding(.vertical, verticalPadding)
            .background(fill, in: RoundedRectangle(cornerRadius: radius, style: .continuous))
    }
}

/// "Sidequest" / "Transit" / "Calendar" / "Group" chip in that kind's colors (Event sheet).
struct KindChip: View {
    let kind: BlockKind

    var body: some View {
        TagLabel(text: kind.palette.label, fill: kind.palette.background, foreground: kind.palette.text,
                 fontSize: 13, horizontalPadding: 10, verticalPadding: 4)
    }
}

/// Simple wrapping layout for chip rows (Quick picks, interests, share chips).
struct FlowLayout: Layout {
    var spacing: CGFloat = 8
    var lineSpacing: CGFloat? = nil

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let maxWidth = proposal.width ?? .infinity
        var x: CGFloat = 0, y: CGFloat = 0, rowHeight: CGFloat = 0, widest: CGFloat = 0
        for view in subviews {
            let size = view.sizeThatFits(.unspecified)
            if x > 0, x + size.width > maxWidth {
                y += rowHeight + (lineSpacing ?? spacing)
                x = 0
                rowHeight = 0
            }
            x += size.width + spacing
            rowHeight = max(rowHeight, size.height)
            widest = max(widest, x - spacing)
        }
        return CGSize(width: proposal.width ?? widest, height: y + rowHeight)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var x = bounds.minX, y = bounds.minY, rowHeight: CGFloat = 0
        for view in subviews {
            let size = view.sizeThatFits(.unspecified)
            if x > bounds.minX, x + size.width > bounds.maxX {
                y += rowHeight + (lineSpacing ?? spacing)
                x = bounds.minX
                rowHeight = 0
            }
            view.place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(size))
            x += size.width + spacing
            rowHeight = max(rowHeight, size.height)
        }
    }
}

/// "88% match": a taste match from the server (People for you, Forum plans). Soft sage, not tappable.
struct MatchPill: View {
    let percent: Int

    var body: some View {
        Text("\(percent)% match")
            .sqFont(11, .semibold)
            .foregroundStyle(Theme.sageInk)
            .lineLimit(1)
            .fixedSize()
            .padding(.horizontal, 7)
            .frame(height: 20)
            .background(Theme.sageTint, in: Capsule())
            .sqNumeric()
            .accessibilityLabel("\(percent) percent taste match")
    }
}
