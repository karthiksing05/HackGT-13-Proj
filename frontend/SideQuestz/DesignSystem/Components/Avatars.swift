import SwiftUI
import UIKit

/// Circle avatar with initials (or a photo) and an optional status dot at the bottom-right.
struct Avatar: View {
    var initials: String
    var fill: Color
    var foreground: Color = .white
    var size: CGFloat
    var fontSize: CGFloat? = nil
    var fontWeight: Font.Weight = .bold
    var image: UIImage? = nil
    var imageURL: URL? = nil
    /// Ring drawn inside the circle (stacks: 2pt white; Who's in: 2pt sage / lineStrong).
    var ring: Color? = nil
    var ringWidth: CGFloat = 2
    var statusColor: Color? = nil
    var statusSize: CGFloat = 14
    var statusBorder: Color = Theme.cream
    var statusBorderWidth: CGFloat = 2
    /// Nudges the status dot (Home header: -1; Account: +2 inset).
    var statusOffset: CGFloat = 1

    var body: some View {
        ZStack(alignment: .bottomTrailing) {
            ZStack {
                Circle().fill(fill)
                if let image {
                    Image(uiImage: image).resizable().scaledToFill()
                } else if let imageURL {
                    AsyncImage(url: imageURL) { phase in
                        if let loaded = phase.image { loaded.resizable().scaledToFill() } else { initialsText }
                    }
                } else {
                    initialsText
                }
            }
            .frame(width: size, height: size)
            .clipShape(Circle())
            .overlay {
                if let ring { Circle().strokeBorder(ring, lineWidth: ringWidth) }
            }

            if let statusColor {
                Circle()
                    .fill(statusColor)
                    .frame(width: statusSize, height: statusSize)
                    .overlay(Circle().strokeBorder(statusBorder, lineWidth: statusBorderWidth))
                    .offset(x: statusOffset, y: statusOffset)
            }
        }
        .frame(width: size, height: size)
    }

    private var initialsText: some View {
        Text(initials)
            .font(.system(size: fontSize ?? size * 0.36, weight: fontWeight))
            .foregroundStyle(foreground)
            .lineLimit(1)
            .minimumScaleFactor(0.5)
    }
}

extension Avatar {
    /// Avatar for another person (colored circle, white bold initials).
    init(person: PersonRef, size: CGFloat, fontSize: CGFloat? = nil, ring: Color? = nil, ringWidth: CGFloat = 2) {
        self.init(initials: person.initials, fill: person.color, foreground: .white, size: size, fontSize: fontSize,
                  fontWeight: .bold, ring: ring, ringWidth: ringWidth)
    }
}

/// Overlapping avatars (timeline blocks 22pt, calendar 20pt, forum 26pt), each with a 2pt white border.
struct AvatarStack: View {
    let people: [PersonRef]
    var size: CGFloat = 22
    var overlap: CGFloat = 6
    var fontSize: CGFloat? = nil
    var maxCount = 4

    var body: some View {
        // Later faces draw on top, as in the prototype's CSS stacks.
        HStack(spacing: -overlap) {
            ForEach(Array(people.prefix(maxCount).enumerated()), id: \.offset) { _, person in
                Avatar(person: person, size: size, fontSize: fontSize ?? size * 0.4, ring: .white, ringWidth: 2)
            }
        }
        .accessibilityElement()
        .accessibilityLabel(people.prefix(maxCount).map(\.name).joined(separator: ", "))
    }
}

/// The signed-in user's avatar (photo or initials on their chosen color) with the presence dot.
struct CurrentUserAvatar: View {
    @Environment(AppEnvironment.self) private var env
    var size: CGFloat
    var showsStatus = true
    var statusSize: CGFloat = 14
    var statusBorderWidth: CGFloat = 2
    var statusOffset: CGFloat = 1

    var body: some View {
        let user = env.user
        let color = user?.avatarColor ?? .ink
        Avatar(initials: user?.initials ?? "JL", fill: color.background, foreground: color.foreground, size: size,
               fontSize: size * 0.36, fontWeight: .semibold, image: env.profileImage,
               imageURL: env.profileImage == nil ? user?.photoURL : nil,
               statusColor: showsStatus ? (user?.status ?? .open).color : nil,
               statusSize: statusSize, statusBorder: Theme.cream, statusBorderWidth: statusBorderWidth, statusOffset: statusOffset)
    }
}

/// A small colored status dot (friend rows, status buttons).
struct StatusDot: View {
    var color: Color
    var size: CGFloat = 10

    var body: some View {
        Circle().fill(color).frame(width: size, height: size)
    }
}
