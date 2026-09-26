import SwiftUI

/// One Forum post (GUI_PLAN.md §7.8): an open plan ("Request to join") or a free-now post
/// ("Plan together"). White card, radius 16, padding 14, 10pt gaps.
struct ForumPostCard: View {
    let post: ForumPost
    /// The card's action is in flight (button disabled, spinner).
    var isBusy = false
    /// Last action failure for this card.
    var error: String?
    let action: () -> Void

    private var isPlan: Bool { post.type == .plan }
    /// "Requested · waiting on host" / "Message sent".
    private var isDone: Bool { isPlan ? post.joinRequested : post.planTogetherSent }

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
        Text(post.title ?? "")
            .socialText(17, .bold)
            .foregroundStyle(Theme.ink)
            .fixedSize(horizontal: false, vertical: true)
            .accessibilityAddTraits(.isHeader)
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
        }
        .accessibilityElement(children: .combine)
        if post.spotsLabel != nil || post.lockLabel != nil {
            VStack(spacing: 6) {
                SpotsBar(fraction: post.fillFraction)
                HStack {
                    if let spots = post.spotsLabel { Text(spots) }
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
        if isPlan { return post.joinRequested ? "Requested · waiting on host" : "Request to join" }
        return post.planTogetherSent ? "Message sent" : "Plan together"
    }

    private var buttonColors: (fill: Color, text: Color, border: Color) {
        if isDone { return (Theme.cream, Theme.text2, Theme.cream) }
        return isPlan ? (Theme.sage, Theme.ink, Theme.sage) : (.white, Theme.sageInk, Theme.sage)
    }

    private var actionButton: some View {
        let colors = buttonColors
        return Button(action: action) {
            HStack(spacing: 8) {
                if isBusy {
                    ProgressView().controlSize(.small).tint(colors.text)
                }
                Text(buttonLabel)
            }
        }
        .buttonStyle(.sq(fill: colors.fill, foreground: colors.text, border: colors.border, borderWidth: 1.5,
                         height: 40, radius: 12, fontSize: 15))
        .disabled(isBusy)
        .accessibilityHint(accessibilityHint)
    }

    private var accessibilityHint: String {
        switch (isPlan, isDone) {
        case (true, false): "Asks the host for a spot"
        case (true, true): "Cancels your request"
        case (false, false): "Sends a message to plan something together"
        case (false, true): "Opens your messages"
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

/// 6pt spots bar: sage fill on a `line` track, rounded ends (the fill is clipped, not rounded).
private struct SpotsBar: View {
    let fraction: Double

    var body: some View {
        GeometryReader { proxy in
            Rectangle()
                .fill(Theme.sage)
                .frame(width: proxy.size.width * max(0, min(1, fraction)))
        }
        .frame(height: 6)
        .background(Theme.line)
        .clipShape(Capsule())
        .accessibilityHidden(true)
    }
}
