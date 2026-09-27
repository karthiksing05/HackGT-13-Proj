import MapKit
import SwiftUI
import UIKit

/// A ticket up close (Your tickets › tap a ticket; Home › Event sheet › "View ticket"): the stop,
/// when, where (with Directions), how many it admits and what it cost, the confirmation code big in
/// the mono face (tap to copy: "Copied"), a QR code made on the phone from the ticket page or the
/// code, "Open ticket page" in the in-app browser, and "Go to sidequest" (Home with the plan
/// selected and this stop open), offered while the stop is ahead and the plan is still yours.
///
/// Everything it shows came with the ticket, so there's nothing to load.
struct TicketDetailSheet: View {
    static let style = SQSheetStyle(height: .fitted(max: 780))

    let ticket: MyTicket
    /// nil hides "Go to sidequest" (the Event sheet is already there).
    var goToSidequest: (() -> Void)? = nil
    let close: () -> Void

    @Environment(AppEnvironment.self) private var env
    @Environment(\.openURL) private var openURL
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var qrCode: UIImage?
    @State private var copied = false
    /// Bumped per copy: the success tap and the "Copied" timer follow it.
    @State private var copies = 0
    @State private var browserLink: HomeBrowserLink?
    /// Where the pass tears: just under its admits / total row.
    @State private var tearY: CGFloat = 78

    private var isPast: Bool { !ticket.isUpcoming(at: env.clock.now) }
    private var qrPayload: String? { TicketQRCode.payload(for: ticket.ticket) }

    var body: some View {
        SheetScaffold {
            header
            Text(ticket.title)
                .sqFont(26, .bold, relativeTo: .title)
                .foregroundStyle(Theme.ink)
                .homeLine(26, 1.15)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityAddTraits(.isHeader)
                .accessibilityIdentifier("ticketDetail.title")
            VStack(alignment: .leading, spacing: 6) {
                timeRow
                if !ticket.place.name.isEmpty { placeRow }
            }
            pass
            planLine
            actions
        }
        .task(id: qrPayload) { qrCode = qrPayload.flatMap { TicketQRCode.image(for: $0) } }
        .task(id: copies) {
            guard copies > 0 else { return }
            try? await Task.sleep(for: .seconds(1.6))
            guard !Task.isCancelled else { return }
            withMotion(Motion.quick) { copied = false }
        }
        .sensoryFeedback(.success, trigger: copies)
        .sheet(item: $browserLink) { link in
            SafariView(url: link.url).ignoresSafeArea()
        }
    }

    // MARK: Header

    /// The stop's kind chip (and "Past", or who booked it when it was the group), then ×.
    private var header: some View {
        HStack(spacing: 8) {
            let palette = ticket.kind.palette
            TagLabel(text: palette.label, fill: palette.background, foreground: palette.text,
                     fontSize: 13, horizontalPadding: 10, verticalPadding: 5, radius: 8)
            if isPast {
                TagLabel(text: "Past", fill: Theme.busyBg, foreground: Theme.text2, fontSize: 13, horizontalPadding: 10, verticalPadding: 5, radius: 8)
            }
            if !ticket.ticket.isMine {
                TagLabel(text: "Booked by the group", fill: Theme.cream, foreground: Theme.text2, fontSize: 13,
                         horizontalPadding: 10, verticalPadding: 5, radius: 8)
            }
            Spacer(minLength: 0)
            CloseCircleButton(action: close)
                .padding(-6)
        }
    }

    private var timeRow: some View {
        HStack(spacing: 8) {
            HomeIcon(glyph: .clock, size: 18)
            Text(TicketTime.full(ticket, format: env.format))
                .sqFont(15)
                .homeLine(15)
        }
        .foregroundStyle(Theme.text2)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(TicketTime.spoken(ticket, format: env.format))
    }

    /// The venue, and Directions in Apple Maps.
    private var placeRow: some View {
        HStack(spacing: 8) {
            HStack(spacing: 8) {
                HomeIcon(glyph: .pin, size: 18)
                Text(ticket.place.name)
                    .sqFont(15)
                    .homeLine(15)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .foregroundStyle(Theme.text2)
            .accessibilityElement(children: .combine)
            Spacer(minLength: 8)
            Button(action: openDirections) {
                HStack(spacing: 5) {
                    Image(systemName: "arrow.triangle.turn.up.right.diamond")
                        .font(.system(size: 13, weight: .semibold))
                    Text("Directions")
                }
            }
            .buttonStyle(.sqPill(fill: Theme.sageTint, foreground: Theme.sageInk, height: 32, fontSize: 13, horizontalPadding: 12))
            .accessibilityLabel("Directions")
            .accessibilityHint("Opens Apple Maps with directions to \(ticket.place.name)")
        }
    }

    // MARK: The pass

    /// The ticket itself: admits and total, a tear line, then the QR code and the confirmation code.
    private var pass: some View {
        let tear = TicketShape.Tear.horizontal(fromTop: tearY)
        return VStack(spacing: 0) {
            HStack(alignment: .top, spacing: 12) {
                fact("ADMITS", "\(ticket.ticket.quantity)", spoken: ticket.admitsLabel)
                Spacer(minLength: 8)
                if let total = ticket.ticket.totalCents {
                    fact(ticket.ticket.isMine ? "TOTAL PAID" : "TOTAL", Money.format(total), alignment: .trailing,
                         spoken: "\(ticket.ticket.isMine ? "Total paid" : "Total") \(Money.format(total))")
                }
            }
            .padding(.top, 16)
            .padding(.bottom, 14)
            .onGeometryChange(for: CGFloat.self, of: { $0.size.height }) { tearY = $0 }
            VStack(spacing: 12) {
                if let qrCode {
                    Image(uiImage: qrCode)
                        .interpolation(.none)
                        .resizable()
                        .scaledToFit()
                        .frame(width: 164, height: 164)
                        .padding(10)
                        .background(.white, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                        .accessibilityLabel("QR code for this ticket")
                        .transition(.opacity)
                }
                if let code = ticket.ticket.confirmation, !code.isEmpty {
                    codeButton(code)
                }
                // Simulated: tickets come from the demo checkout or the sandbox merchant (Stripe test mode).
                Text("Sandbox ticket: no real payment, not valid for entry.")
                    .sqFont(12, relativeTo: .caption)
                    .foregroundStyle(Theme.text3)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .padding(.top, 20)
            .padding(.bottom, 16)
            .frame(maxWidth: .infinity)
            .animation(reduceMotion ? Motion.reduced : Motion.standard, value: qrCode == nil)
        }
        .padding(.horizontal, 18)
        .background(Theme.cream)
        .clipShape(TicketShape(tear: tear, notch: 10, cornerRadius: 18))
        .overlay {
            TicketPerforation(tear: tear, notch: 10)
                .stroke(Theme.mutedBorder, style: TicketPerforation.style)
        }
    }

    /// "ADMITS" over "2" (the mono face, like the card's stub).
    private func fact(_ label: String, _ value: String, alignment: HorizontalAlignment = .leading, spoken: String) -> some View {
        VStack(alignment: alignment, spacing: 2) {
            Text(label)
                .sqFont(11, .bold, relativeTo: .caption2)
                .tracking(0.6)
                .foregroundStyle(Theme.text3)
            Text(value)
                .font(.mono(24, .extraBold, relativeTo: .title2))
                .foregroundStyle(Theme.ink)
                .lineLimit(1)
                .minimumScaleFactor(0.7)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(spoken)
    }

    /// The confirmation code, big: a tap copies it ("Copied" for a moment).
    private func codeButton(_ code: String) -> some View {
        Button { copy(code) } label: {
            VStack(spacing: 3) {
                Text(code)
                    .font(.mono(26, .bold, relativeTo: .title2))
                    .tracking(1.5)
                    .foregroundStyle(Theme.ink)
                    .lineLimit(1)
                    .minimumScaleFactor(0.6)
                HStack(spacing: 4) {
                    if copied {
                        Image(systemName: "checkmark")
                            .font(.system(size: 11, weight: .bold))
                            .transition(.opacity)
                    }
                    Text(copied ? "Copied" : "Tap to copy")
                        .contentTransition(.opacity)
                }
                .sqFont(13, .semibold)
                .foregroundStyle(copied ? Theme.success : Theme.sageInk)
            }
            .padding(.horizontal, 12)
            .frame(minHeight: Metrics.minTouch)
            .contentShape(Rectangle())
        }
        .buttonStyle(.sqPressable)
        .accessibilityLabel("Confirmation code \(code.map(String.init).joined(separator: " "))")
        .accessibilityValue(copied ? "Copied" : "")
        .accessibilityHint("Copies the code")
        .accessibilityIdentifier("ticketDetail.code")
    }

    // MARK: Plan + actions

    @ViewBuilder private var planLine: some View {
        Group {
            if let plan = ticket.itineraryTitle {
                Text("Part of \(Text(plan).fontWeight(.semibold).foregroundStyle(Theme.ink))")
            } else {
                Text("You're no longer on this sidequest. The ticket is still yours.")
            }
        }
        .sqFont(14)
        .foregroundStyle(Theme.text2)
        .homeLine(14)
        .fixedSize(horizontal: false, vertical: true)
    }

    private var actions: some View {
        VStack(spacing: 10) {
            if let url = ticket.ticket.url {
                Button {
                    browserLink = HomeBrowserLink(url: url)
                } label: {
                    HStack(spacing: 7) {
                        Image(systemName: "safari")
                            .font(.system(size: 16, weight: .medium))
                        Text("Open ticket page")
                    }
                }
                .buttonStyle(.sq(fill: Theme.sage, foreground: Theme.ink, height: 48, fontSize: 16))
                .accessibilityHint("Opens the ticket's page in the app")
            }
            if let goToSidequest {
                Button("Go to sidequest", action: goToSidequest)
                    .buttonStyle(.sq(fill: Theme.cream, foreground: Theme.ink, height: 48, fontSize: 16))
                    .accessibilityHint("Shows the plan on Home with this stop open")
            }
        }
        .padding(.top, 2)
    }

    // MARK: Doing things

    private func copy(_ code: String) {
        UIPasteboard.general.string = code
        withMotion(Motion.quick) { copied = true }
        copies += 1
        AccessibilityNotification.Announcement("Copied").post()
    }

    /// Apple Maps with directions: to the pin when the place has one, else to its name.
    private func openDirections() {
        let place = ticket.place
        guard let coordinate = place.coordinate else {
            var components = URLComponents(string: "https://maps.apple.com/")
            components?.queryItems = [URLQueryItem(name: "daddr", value: place.name)]
            if let url = components?.url { openURL(url) }
            return
        }
        let location = CLLocation(latitude: coordinate.lat, longitude: coordinate.lng)
        let item: MKMapItem
        if #available(iOS 26.0, *) {
            item = MKMapItem(location: location, address: nil)
        } else {
            item = MKMapItem(placemark: MKPlacemark(coordinate: location.coordinate))
        }
        item.name = place.name
        item.openInMaps(launchOptions: [MKLaunchOptionsDirectionsModeKey: MKLaunchOptionsDirectionsModeDefault])
    }
}
