import SwiftUI

/// One Forum post (GUI_PLAN.md §7.8): an open plan ("Request to join") or a free-now post
/// ("Plan together"). White card, radius 16, padding 14, 10pt gaps.
///
/// An open plan's button follows where you stand (`join_status`): "Request to join" → "Requested ·
/// waiting on host" (tap again to cancel) or, when the plan lets you straight in, "You're in · Open
/// chat"; a full or closed plan says so and can't be tapped.
///
/// Motion: while the card's request runs the button shows `LoadingDots`; when it's done the label
/// and colors morph ("Message sent" and "You're in" draw a check in). The spot bubbles fill in one
/// after another when the card appears, and a bubble fills with a pop when someone takes a spot.
struct ForumPostCard: View {
    let post: ForumPost
    /// The card's action is in flight (button disabled, loading dots).
    var isBusy = false
    /// Last action failure for this card.
    var error: String?
    let action: () -> Void

    private var isPlan: Bool { post.type == .plan }
    /// A plan that can't be joined from here (full or closed).
    private var isUnavailable: Bool { isPlan && (post.joinStatus == .full || post.joinStatus == .closed) }
    /// "Requested · waiting on host", "You're in", "Message sent": the button turns quiet.
    private var isDone: Bool {
        isPlan ? post.joinStatus == .requested || post.joinStatus == .joined : post.planTogetherSent
    }
    /// The check that draws in next to "You're in" and "Message sent".
    private var showsCheck: Bool { isPlan ? post.joinStatus == .joined : post.planTogetherSent }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            header
            if isPlan {
                planDetails
            } else {
                Text(post.text ?? "")
                    .socialText(15, lineHeight: 1.4)
                    .foregroundStyle(Theme.ink)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if post.friendsOnly {
                Text("Shared with friends only")
                    .socialText(12)
                    .foregroundStyle(Theme.text3)
            }
            actionButton
            if let error {
                Text(error)
                    .socialText(12)
                    .foregroundStyle(Theme.dangerText)
                    .accessibilityAddTraits(.isStaticText)
                    .sqTransition(.rise)
            }
        }
        .padding(Metrics.cardPadding)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
    }

    // MARK: Header

    private var header: some View {
        HStack(spacing: 10) {
            Avatar(person: post.author, size: 36, fontSize: 13)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 0) {
                HStack(spacing: 6) {
                    Text(post.author.name)
                        .socialText(15, .semibold)
                        .foregroundStyle(Theme.ink)
                        .lineLimit(1)
                    if post.isFriend {
                        Text("Friend")
                            .socialText(11, .semibold)
                            .foregroundStyle(Theme.text2)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 1)
                            .background(Theme.cream, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
                    }
                }
                Text(post.meta)
                    .socialText(12)
                    .foregroundStyle(Theme.text3)
                    .lineLimit(1)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            SocialTag(text: isPlan ? "Open plan" : "Free now",
                      fill: isPlan ? Theme.sageTint : Theme.transitBg,
                      foreground: isPlan ? Theme.sageInk : Theme.transitText)
        }
        .accessibilityElement(children: .combine)
    }

    // MARK: Open plan

    @ViewBuilder private var planDetails: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(post.title ?? "")
                .socialText(17, .bold)
                .foregroundStyle(Theme.ink)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityAddTraits(.isHeader)
                .frame(maxWidth: .infinity, alignment: .leading)
            // How well the plan's stops fit your taste (server-scored).
            if let match = post.compatibility {
                MatchPill(percent: match)
            }
        }
        if post.when != nil || post.route != nil {
            VStack(alignment: .leading, spacing: 4) {
                if let when = post.when { Text(when).socialText(13) }
                if let route = post.route { Text(route).socialText(13) }
            }
            .foregroundStyle(Theme.text2)
            .accessibilityElement(children: .combine)
        }
        HStack(spacing: 0) {
            if !post.going.isEmpty {
                GoingFaces(people: post.going)
                    .padding(.trailing, 7)
            }
            Text(post.peopleLine)
                .socialText(13)
                .foregroundStyle(Theme.text2)
                .sqNumeric()
        }
        .accessibilityElement(children: .combine)
        if post.spotsLabel != nil || post.lockLabel != nil {
            VStack(alignment: .leading, spacing: 6) {
                if let capacity = post.capacity, let spotsLeft = post.spotsLeft, capacity > 0 {
                    SpotBubbles(capacity: capacity, taken: capacity - spotsLeft)
                }
                HStack {
                    if let spots = post.spotsLabel { Text(spots).sqNumeric() }
                    Spacer(minLength: 8)
                    if let lock = post.lockLabel {
                        HStack(spacing: 4) {
                            SocialGlyph(kind: .lock, size: 12, lineWidth: 2)
                            Text(lock)
                        }
                    }
                }
                .socialText(12)
                .foregroundStyle(Theme.text3)
            }
            .accessibilityElement(children: .combine)
        }
    }

    // MARK: Action

    private var buttonLabel: String {
        guard isPlan else { return post.planTogetherSent ? "Message sent" : "Plan together" }
        switch post.joinStatus {
        case .none: return "Request to join"
        case .requested: return "Requested · waiting on host"
        case .joined: return "You're in · Open chat"
        case .full: return "This plan is full"
        case .closed: return "Joining closed"
        }
    }

    private var buttonColors: (fill: Color, text: Color, border: Color) {
        if isPlan && post.joinStatus == .joined { return (Theme.sageTint, Theme.sageInk, Theme.sageTint) }
        if isUnavailable { return (Theme.cream, Theme.text3, Theme.cream) }
        if isDone { return (Theme.cream, Theme.text2, Theme.cream) }
        return isPlan ? (Theme.sage, Theme.ink, Theme.sage) : (.white, Theme.sageInk, Theme.sage)
    }

    private var actionButton: some View {
        let colors = buttonColors
        return Button(action: action) {
            SocialBusyLabel(isBusy: isBusy, color: colors.text) {
                HStack(spacing: 6) {
                    // "Message sent" and "You're in" confirm with a check that draws itself in.
                    if showsCheck {
                        AnimatedCheck(lineWidth: 2.8, delay: 0.12)
                            .frame(width: 14, height: 14)
                            .sqTransition(.pop)
                    }
                    Text(buttonLabel)
                        .contentTransition(.interpolate)
                }
            }
        }
        .buttonStyle(.sq(fill: colors.fill, foreground: colors.text, border: colors.border, borderWidth: 1.5,
                         height: 40, radius: 12, fontSize: 15))
        .disabled(isBusy || isUnavailable)
        .accessibilityLabel(buttonLabel)
        .accessibilityValue(isBusy ? "Loading" : "")
        .accessibilityHint(accessibilityHint)
    }

    private var accessibilityHint: String {
        guard isPlan else { return post.planTogetherSent ? "Opens your messages" : "Sends a message to plan something together" }
        switch post.joinStatus {
        case .none: return "Asks the host for a spot"
        case .requested: return "Cancels your request"
        case .joined: return "Opens the group chat"
        case .full, .closed: return ""
        }
    }
}

/// 26pt faces overlapping by 7 with a 2pt white border; like the prototype, each face sits on top
/// of the one before it. (A white disc behind a 22pt avatar avoids a halo on the white card.)
private struct GoingFaces: View {
    let people: [PersonRef]

    var body: some View {
        HStack(spacing: -7) {
            ForEach(Array(people.prefix(5).enumerated()), id: \.offset) { _, person in
                Circle()
                    .fill(.white)
                    .frame(width: 26, height: 26)
                    .overlay { Avatar(person: person, size: 22, fontSize: 10) }
            }
        }
        .accessibilityHidden(true)
    }
}

/// One bubble per spot: sage for taken, a sage ring for open. Past `maxBubbles` the rest collapse
/// into "+N" (the "N of M spots left" line under it has the exact count). The taken bubbles fill in
/// one after another when the card first appears; later, a newly taken spot fills with a spring.
private struct SpotBubbles: View {
    let capacity: Int
    let taken: Int

    private static let maxBubbles = 10

    private var shown: Int { min(capacity, Self.maxBubbles) }

    var body: some View {
        HStack(spacing: 5) {
            ForEach(0..<shown, id: \.self) { index in
                SpotBubble(filled: index < taken, index: index)
            }
            if capacity > shown {
                Text("+\(capacity - shown)")
                    .socialText(12, .semibold)
                    .foregroundStyle(Theme.text3)
                    .padding(.leading, 2)
            }
        }
        .accessibilityHidden(true)
    }
}

/// A 12pt spot: the ring is always there; the sage fill grows in when the spot is taken.
private struct SpotBubble: View {
    let filled: Bool
    /// Order in the first fill-in.
    let index: Int
    @State private var appeared = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        let showsFill = filled && (appeared || reduceMotion)
        Circle()
            .strokeBorder(Theme.sage, lineWidth: 1.5)
            .background {
                Circle()
                    .fill(Theme.sage)
                    .scaleEffect(showsFill ? 1 : 0.2)
                    .opacity(showsFill ? 1 : 0)
            }
            .frame(width: 12, height: 12)
            // A spot taken (or given back) while the card is on screen.
            .animation(reduceMotion ? Motion.reduced : Motion.arrive, value: filled)
            .onAppear {
                guard !appeared, !reduceMotion else { return }
                withAnimation(Motion.arrive.delay(0.12 + Motion.stagger(index, step: 0.05, cap: 0.45))) { appeared = true }
            }
    }
}

/// Loading placeholder shaped like a post card: avatar and name lines, the type tag, two lines of
/// text and the action button. Stack a few inside `.sqShimmer()`.
struct ForumPostSkeletonCard: View {
    /// Varies the line lengths from card to card.
    var seed = 0

    private var widths: (name: CGFloat, meta: CGFloat, line: CGFloat) {
        let options: [(CGFloat, CGFloat, CGFloat)] = [(96, 150, 0.72), (82, 128, 0.58), (104, 140, 0.8)]
        return options[seed % options.count]
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 10) {
                Circle().fill(Theme.skeleton).frame(width: 36, height: 36)
                VStack(alignment: .leading, spacing: 7) {
                    SkeletonBlock(width: widths.name, height: 13)
                    SkeletonBlock(width: widths.meta, height: 10)
                }
                Spacer(minLength: 0)
                SkeletonBlock(width: 66, height: 22, radius: 8)
            }
            VStack(alignment: .leading, spacing: 8) {
                SkeletonBlock(height: 12)
                GeometryReader { proxy in
                    SkeletonBlock(width: proxy.size.width * widths.line, height: 12)
                }
                .frame(height: 12)
            }
            .padding(.vertical, 3)
            SkeletonBlock(height: 40, radius: 12)
        }
        .padding(Metrics.cardPadding)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
    }
}
