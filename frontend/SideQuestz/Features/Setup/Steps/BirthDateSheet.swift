import SwiftUI

/// Setup › basics › "Date of birth": month / day / year wheels that start on the date already
/// picked (or 20 years back). Done keeps the date the wheels show, so the one they start on can be
/// chosen as it is; Cancel, the backdrop or dragging the sheet down keep the old date.
///
/// Present with `sqSheet`. The wheels run in the app's time zone and calendar, in the US order and
/// month names every date in the app is written in.
struct BirthDateSheet: View {
    /// The latest date the wheels allow (today).
    let latest: Date
    let done: (Date) -> Void
    let cancel: () -> Void

    @Environment(AppEnvironment.self) private var env
    @State private var shown: Date

    init(start: Date, latest: Date, done: @escaping (Date) -> Void, cancel: @escaping () -> Void) {
        self.latest = latest
        self.done = done
        self.cancel = cancel
        _shown = State(initialValue: min(start, latest))
    }

    /// Month names and wheel order (month, day, year), like `TimeFormat`.
    static let locale = Locale(identifier: "en_US")

    /// "June 14, 2003": the field's value, in the app's time zone.
    static func text(_ date: Date, clock: AppClock) -> String {
        let formatter = DateFormatter()
        formatter.locale = locale
        formatter.timeZone = clock.timeZone
        formatter.setLocalizedDateFormatFromTemplate("MMMMdyyyy")
        return formatter.string(from: date)
    }

    var body: some View {
        SheetScaffold(spacing: 10) {
            Text("Date of birth")
                .font(.mono(22, .extraBold, relativeTo: .title2))
                .foregroundStyle(Theme.ink)
                .authLineHeight(1.35, size: 22, mono: true)
                .accessibilityAddTraits(.isHeader)
            DatePicker("Date of birth", selection: $shown, in: ...latest, displayedComponents: .date)
                .datePickerStyle(.wheel)
                .labelsHidden()
                .frame(maxWidth: .infinity)
                .environment(\.timeZone, env.clock.timeZone)
                .environment(\.calendar, env.clock.calendar)
                .environment(\.locale, Self.locale)
            HStack(spacing: 8) {
                Button("Cancel", action: cancel)
                    .buttonStyle(.sq(fill: Theme.cream, foreground: Theme.ink, height: 50, fontSize: 16))
                    .accessibilityHint("Keeps your date of birth as it was")
                    .accessibilityIdentifier("setup.birthDate.cancel")
                Button("Done") { done(shown) }
                    .buttonStyle(.sq(fill: Theme.ink, foreground: .white, height: 50, fontSize: 16))
                    .accessibilityLabel("Done")
                    .accessibilityHint("Sets your date of birth to the date shown")
                    .accessibilityIdentifier("setup.birthDate.done")
            }
            .padding(.top, 4)
        }
    }
}
