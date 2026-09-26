import SwiftUI

/// Every design-system piece on one scrolling page (GUI_PLAN.md build step 1).
/// Open it with the Xcode preview below, or launch with `-SQRoute gallery`.
struct DesignGalleryView: View {
    @State private var segment = 0
    @State private var toggle = true
    @State private var chip = "Outdoors"
    @State private var stars = 4
    @State private var email = ""
    @State private var password = "wander2026"
    @State private var reveal = false
    @State private var notes = ""
    @State private var showSheet = false

    private let palette: [(String, Color)] = [
        ("sage", Theme.sage), ("sageInk", Theme.sageInk), ("ink", Theme.ink), ("cream", Theme.cream),
        ("sageTint", Theme.sageTint), ("clay", Theme.clay), ("clayTint", Theme.clayTint), ("transitBg", Theme.transitBg),
        ("text2", Theme.text2), ("text3", Theme.text3), ("line", Theme.line), ("lineStrong", Theme.lineStrong),
        ("danger", Theme.danger), ("success", Theme.success), ("statusOpen", Theme.statusOpen), ("statusFriends", Theme.statusFriends),
    ]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                TabScreenHeader(eyebrow: "DESIGN SYSTEM", title: "Gallery")

                group("Logo") {
                    HStack(spacing: 14) {
                        LogoMark(size: 76)
                        LogoMark(size: 76, variant: .dark)
                        LogoMark(size: 76, variant: .light)
                        LogoLoadingView(size: 40)
                    }
                    Wordmark(size: 40)
                    HStack(spacing: 18) {
                        RouteMarker(kind: .start, size: 26)
                        RouteMarker(kind: .end, size: 26)
                        RouteMarker(kind: .start, size: 28, onMap: true)
                        RouteMarker(kind: .end, size: 28, onMap: true)
                    }
                    .padding(8)
                    .background(Theme.mapBackground, in: RoundedRectangle(cornerRadius: 12))
                }

                group("Motion") {
                    HStack(spacing: 18) {
                        LoadingDots()
                        LoadingDots(color: Theme.sageInk, dotSize: 5)
                        AnimatedCheck().foregroundStyle(Theme.success).frame(width: 22, height: 22)
                    }
                    SkeletonView(layout: .rows(count: 2))
                }

                group("Color") {
                    LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 8), count: 4), spacing: 8) {
                        ForEach(palette, id: \.0) { name, color in
                            VStack(spacing: 4) {
                                RoundedRectangle(cornerRadius: 10).fill(color).frame(height: 40)
                                    .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.line))
                                Text(name).captionStyle(10)
                            }
                        }
                    }
                }

                group("Type") {
                    Text("Your SideQuests").largeTitleStyle()
                    Text("Let's set up your profile").setupTitleStyle()
                    Text("Where do you start and end?").stepTitleStyle()
                    Text("Active sidequests").sectionStyle()
                    Text("FRIDAY, SEPTEMBER 25").eyebrowStyle()
                    Text("Mini golf, carnival games and skyline views on the roof.").bodyStyle()
                    Text("Today · 1–8 PM").captionStyle(13)
                }

                group("Buttons") {
                    Button("Sign in") {}.buttonStyle(.sqPrimary)
                    Button("Approve purchase") {}.buttonStyle(.sqDark)
                    HStack {
                        Button("Back") {}.buttonStyle(.sq(fill: .white, foreground: Theme.ink, fullWidth: false))
                        Button("Website") {}.buttonStyle(.sqCream)
                    }
                    Button("Regenerate options with these settings") {}.buttonStyle(.sq(fill: .white, foreground: Theme.sageInk, border: Theme.sage, borderWidth: 1.5, height: 46, fontSize: 15))
                    Button("Reset password") {}.buttonStyle(.sqDisabled())
                    HStack {
                        Button("+ Plan") {}.buttonStyle(.sqTintPill)
                        Button("Accept") {}.buttonStyle(.sqPill)
                        Button("Forgot password?") {}.buttonStyle(.sqLink)
                    }
                }

                group("Chips + controls") {
                    FlowLayout {
                        ForEach(["Outdoors", "Food", "Art", "Music"], id: \.self) { tag in
                            SQChip(label: tag, isOn: chip == tag, style: .tag) { chip = tag }
                        }
                    }
                    HStack(spacing: 6) {
                        SQChip(label: "All", isOn: true, style: .dark) {}
                        SQChip(label: "Open plans", isOn: false, style: .dark) {}
                        SQChip(label: "Great people", isOn: true, style: .rateTag) {}
                    }
                    SQSegmentedControl(selection: $segment, options: [(0, "Sidequests"), (1, "Calendar"), (2, "Past")])
                    ToggleRow(title: "Prefer free events", subtitle: "Show free options first when they fit", isOn: $toggle)
                    ProgressDots(total: 4, current: 2)
                    ProgressBar(fraction: 0.4)
                }

                group("Fields") {
                    SQTextField(label: "Email", text: $email, placeholder: "you@school.edu", kind: .email)
                    SQTextField(label: "Password", text: $password, placeholder: "Your password", kind: .password, revealed: $reveal)
                    PasswordRulesView(password: password, confirm: password)
                    SQTextArea(text: $notes, placeholder: "What to bring, where to meet, reminders…")
                    SearchField(text: $email, placeholder: "Search for your start location")
                    ErrorBox(messages: ["Add your name.", "Enter a valid email address."])
                }

                group("People + ratings") {
                    HStack(spacing: 16) {
                        CurrentUserAvatar(size: 44)
                        AvatarStack(people: [MockPeople.me, MockPeople.maya, MockPeople.dev, MockPeople.ava])
                        KindChip(kind: .group)
                        KindChip(kind: .transit)
                    }
                    StarPicker(rating: $stars)
                    StarRow(rating: 4)
                }

                group("States") {
                    LoadingStateView(label: "Loading", minHeight: 80)
                    ErrorStateView(message: "Couldn't load your sidequests.", minHeight: 80) {}
                    SuccessBanner(text: "Added \"Pizza\" · $40.00 split equally 3 ways ($13.33 each). Everyone was notified.") {}
                    Button("Open a sheet") { showSheet = true }.buttonStyle(.sqPrimary)
                }

                group("Slow loading") {
                    DesignGallerySlowLoading()
                }
            }
            .padding(.bottom, 40)
        }
        .background(Theme.cream.ignoresSafeArea())
        .sqSheet(isPresented: $showSheet) {
            SheetScaffold {
                Text("Sort & filter").sqFont(20, .bold)
                Button("Show 5 results") { showSheet = false }.buttonStyle(.sqPrimary)
            }
        }
    }

    private func group<Content: View>(_ title: String, @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Eyebrow(text: title.uppercased())
            content()
        }
        .padding(.horizontal, Metrics.side)
    }
}

/// The loading policy in miniature (`SlowLoading`): a skeleton, then the S after a second here
/// (the app waits `-SQSlowLoadingAfter`, 2 s by default). Replay starts it over.
private struct DesignGallerySlowLoading: View {
    @State private var run = 0

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            SkeletonView(layout: .rows(count: 2))
                .sqSlowLoading(after: .seconds(1), lines: ["Loading your groups…"])
                .id(run)
            HStack(spacing: 10) {
                Text("Skeleton first; the S after 1 s here, \(appThreshold) in the app (-SQSlowLoadingAfter).")
                    .captionStyle(12)
                Spacer(minLength: 0)
                Button("Replay") { run += 1 }
                    .buttonStyle(.sqTintPill)
            }
        }
    }

    /// The app's threshold, for the caption.
    private var appThreshold: String {
        let parts = SlowLoading.threshold.components
        let seconds = Double(parts.seconds) + Double(parts.attoseconds) / 1e18
        return seconds >= 86_400 ? "never" : "\(seconds.formatted(.number.precision(.fractionLength(0...2)))) s"
    }
}

#Preview("Design gallery") {
    DesignGalleryView()
        .environment(AppEnvironment.preview())
        .environment(Router(phase: .main))
}
