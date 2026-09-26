import SwiftUI

/// A placeholder bar for content that's still loading. `width` nil = fill the row.
struct SkeletonBlock: View {
    var width: CGFloat? = nil
    var height: CGFloat = 12
    var radius: CGFloat = 6
    var color: Color = Theme.skeleton

    var body: some View {
        RoundedRectangle(cornerRadius: radius, style: .continuous)
            .fill(color)
            .frame(width: width, height: height)
            .frame(maxWidth: width == nil ? .infinity : nil, alignment: .leading)
    }
}

/// Ready-made loading layouts that echo the real content's shape.
enum SkeletonLayout: Equatable {
    /// White cards (radius 16) with a title and `lines` lines (Forum posts, itineraries, options).
    case cards(count: Int = 3, lines: Int = 2)
    /// One white card of rows with a leading avatar circle (Groups, Friends, expenses).
    case rows(count: Int = 4, avatar: Bool = true)
    /// Square tiles (album).
    case grid(columns: Int = 3, count: Int = 9)
    /// Chat bubbles alternating sides.
    case bubbles(count: Int = 5)
    /// Plain text lines on the background.
    case lines(count: Int = 3)
}

/// A shimmering placeholder in one of the `SkeletonLayout` shapes. VoiceOver reads "Loading".
struct SkeletonView: View {
    let layout: SkeletonLayout

    var body: some View {
        content
            .sqShimmer()
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("Loading")
    }

    @ViewBuilder private var content: some View {
        switch layout {
        case .cards(let count, let lines):
            VStack(spacing: 12) {
                ForEach(0..<max(count, 1), id: \.self) { index in
                    card(lines: lines, seed: index)
                }
            }
        case .rows(let count, let avatar):
            VStack(spacing: 0) {
                ForEach(0..<max(count, 1), id: \.self) { index in
                    row(avatar: avatar, seed: index)
                    if index < count - 1 { RowDivider() }
                }
            }
            .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
        case .grid(let columns, let count):
            LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 4), count: max(columns, 1)), spacing: 4) {
                ForEach(0..<max(count, 1), id: \.self) { _ in
                    RoundedRectangle(cornerRadius: 6, style: .continuous)
                        .fill(Theme.skeletonOnCream)
                        .aspectRatio(1, contentMode: .fit)
                }
            }
        case .bubbles(let count):
            VStack(spacing: 10) {
                ForEach(0..<max(count, 1), id: \.self) { index in
                    bubble(mine: index % 3 == 1, seed: index)
                }
            }
        case .lines(let count):
            VStack(alignment: .leading, spacing: 8) {
                ForEach(0..<max(count, 1), id: \.self) { index in
                    SkeletonBlock(height: 12, color: Theme.skeletonOnCream)
                        .frame(maxWidth: index == count - 1 ? 180 : .infinity, alignment: .leading)
                }
            }
        }
    }

    /// Varied but stable widths so placeholders don't look like a barcode.
    private func fraction(_ seed: Int, _ salt: Int) -> CGFloat {
        let values: [CGFloat] = [0.92, 0.7, 0.84, 0.62, 0.78, 0.55]
        return values[(seed * 3 + salt) % values.count]
    }

    private func card(lines: Int, seed: Int) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 10) {
                Circle().fill(Theme.skeleton).frame(width: 34, height: 34)
                VStack(alignment: .leading, spacing: 6) {
                    SkeletonBlock(height: 13).fractionWidth(fraction(seed, 0) * 0.55)
                    SkeletonBlock(height: 10).fractionWidth(fraction(seed, 1) * 0.45)
                }
            }
            ForEach(0..<max(lines, 0), id: \.self) { line in
                SkeletonBlock(height: 11).fractionWidth(line == lines - 1 ? fraction(seed, line + 2) * 0.75 : 1)
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
    }

    private func row(avatar: Bool, seed: Int) -> some View {
        HStack(spacing: 12) {
            if avatar { Circle().fill(Theme.skeleton).frame(width: 40, height: 40) }
            VStack(alignment: .leading, spacing: 7) {
                SkeletonBlock(height: 13).fractionWidth(fraction(seed, 0) * 0.6)
                SkeletonBlock(height: 10).fractionWidth(fraction(seed, 1) * 0.8)
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 13)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func bubble(mine: Bool, seed: Int) -> some View {
        RoundedRectangle(cornerRadius: 18, style: .continuous)
            .fill(mine ? Theme.sageTint : .white)
            .frame(height: 40)
            .fractionWidth(0.35 + fraction(seed, 2) * 0.3, trailing: mine)
    }
}

/// Takes the full proposed width and gives its child `fraction` of it, pinned to one side, so bars
/// keep their rounded ends at any length.
private struct FractionWidth: Layout {
    let fraction: CGFloat
    var trailing = false

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width ?? 200
        let height = subviews.first?.sizeThatFits(ProposedViewSize(width: width * clamped, height: proposal.height)).height ?? 0
        return CGSize(width: width, height: height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let width = bounds.width * clamped
        let x = trailing ? bounds.maxX - width : bounds.minX
        subviews.first?.place(at: CGPoint(x: x, y: bounds.minY), anchor: .topLeading,
                              proposal: ProposedViewSize(width: width, height: bounds.height))
    }

    private var clamped: CGFloat { min(max(fraction, 0.05), 1) }
}

private extension View {
    /// Shows this view at `fraction` of the available width.
    func fractionWidth(_ fraction: CGFloat, trailing: Bool = false) -> some View {
        FractionWidth(fraction: fraction, trailing: trailing) { self }
    }
}
