# SideQuests for iOS — build prompt

You are building **SideQuests**, a native iOS app in **Swift + SwiftUI**, for HackGT 13. A clickable prototype already exists and is the source of truth for how every screen looks and behaves. Your job is to rebuild it natively, **matching the visual style exactly**, and wire it to a real backend through a clean API layer that can also run fully on mock data for the demo.

Read this whole prompt before writing code. Then read the reference artifact (section 1). When this prompt and the prototype disagree on a visual detail, **the prototype wins**. When they disagree on behavior or data, **this prompt wins**.

---

## 1. Reference material (read these first)

### 1.1 The design artifact (source of truth for visuals)
- Artifact: **"SideQuestz Mock UI"** — https://claude.ai/artifact/3Zow298jHCW3y3Vr1Ky4K5
- It is a Design canvas. Read it with your Artifact tool (`action: "read"`, `url` above). Then read individual files with `path`:
  - `project/Main.dc.html` — **the whole clickable prototype in one file** (~2,500 lines). All screens, all copy, every color, size, radius and spacing as inline CSS, plus the full interaction logic in the `<script type="text/x-dc">` block at the bottom (`renderVals()` holds the sample data and state rules). Treat CSS `px` as iOS points: the prototype frame is exactly **390 × 844 pt** (iPhone 15/16).
  - `project/Brand.dc.html` — brand board: logo mark, variants, palette with roles, type samples, UI samples, and a 4-frame storyboard of the opening animation.
  - `project/canvas.json` — lists every artboard. Each gallery artboard is a one-line file that mounts `Main` at a fixed state. Its `dc-import` attributes tell you which state it shows (for example `screen="create" step="4"`).
- Gallery artboards (file → what it shows):
  - Core loop: `Login`, `Home` (itineraries), `Detail` (event sheet), `Calendar`, `CreateWhere`, `CreateWhen`, `CreateVibe`, `CreateReview`, `CreateMore`, `HomePast`, `RatePast`
  - Social + account: `Forum`, `ForumLocation`, `ForumFilter`, `Groups`, `GroupChat`, `GroupAlbum`, `GroupSplits`, `AddExpense`, `Profile`, `Friends`, `DirectMessage`
  - Setup + recovery: `SetupBasics`, `SetupConnect`, `SetupLikes`, `SetupMoney`, `SetupMore`, `ForgotEmail`, `ForgotCode`, `ForgotNew`, `ForgotDone`
  - Brand: `Brand`, `Splash`
- If you can't open the artifact, everything you need is also written out below.

### 1.2 Project docs (in the HackGT13 Claude project)
- `claude/sidequests-ui-decisions.md` — the running decisions log (all UI decisions, in plain language).
- `claude/sidequestz-api-endpoints.md` — backend endpoint list (summarized in section 9).

---

## 2. What the app is

**Pitch:** You have time to kill (a gap between classes, a delayed flight, a free afternoon) but finding something good to do takes too long. SideQuests plans it for you. Tell it where you are, where you need to end up and by when, and what you're in the mood for (typed or by voice). It builds a timed itinerary with transit between stops that gets you back on time. You can go solo, go with friends, or open the plan so nearby people can join.

**Audience:** college students and solo travelers with flexible time. "Type-B fun, planned Type-A."

**Tagline:** "Turn waiting into wandering."

**Core loop:**
1. Plan: + → Where → When → Vibe → Review
2. Pick one of the proposed itineraries and adjust it (reorder stops by dragging; transit re-times itself)
3. Go: timeline on Home, event details, in-app website, agent ticket checkout
4. Share (Forum, Groups, chat, album, split costs)
5. Rate what you did, which improves future suggestions

**Hackathon sponsor tech to show:** Gemini (plan generation and ranking, on the backend), MongoDB (backend store), Visa (agentic checkout and settling group balances). Voice input uses Apple's Speech framework.

---

## 3. Tech stack and architecture

- **iOS 17+, Swift 5.10+, SwiftUI**, Observation (`@Observable`), `NavigationStack`, async/await.
- **No third-party UI libraries.** Build the components in section 5 yourself.
- Networking: `URLSession` + `Codable`. `APIClient` protocol with `LiveAPIClient` and `MockAPIClient`. One `AppEnvironment` decides which one to use; **mock is the default for the demo build**.
- Realtime: `URLSessionWebSocketTask` (`WS /ws`), used for chat messages, join requests, friend status, checkout status and transit delays.
- Auth tokens are stored in the Keychain.
- Maps: MapKit (`Map`, `MKLocalSearch` for place search, reverse geocoding for dropped pins).
- Calendars: Google/Outlook connect through `ASWebAuthenticationSession`, pointed at the backend OAuth URLs. The app never reads event details, only free/busy blocks.
- In-app browser: `SFSafariViewController`, wrapped for SwiftUI and shown as a sheet.
- Photos: `PhotosPicker` for the library, plus a camera wrapper (`UIImagePickerController`, `.camera`).
- Voice: a `VoiceInputService` built on Apple `Speech` (`SFSpeechRecognizer`), on device when supported. Voice turns into text and fills the text field. Nothing else changes.
- Local flags: none persisted for the intro (it plays on every cold launch; `-SQSkipIntro YES` skips it).
- Haptics: `sensoryFeedback` for selection changes, drop-to-reorder and success.

**Suggested folder layout**
```
SideQuestz/
  App/            SideQuestzApp.swift, RootView.swift, AppEnvironment.swift, Router.swift
  DesignSystem/   Colors.swift, Typography.swift, Metrics.swift, Components/*.swift, Logo/*.swift
  Features/
    Intro/        SplashView.swift
    Auth/         LoginView.swift, ForgotPassword/*.swift
    Setup/        SetupFlowView.swift, Steps/*.swift, PhotoSheet.swift
    Home/         HomeView.swift, ItinerariesView.swift, CalendarDaysView.swift, PastView.swift, EventDetailSheet.swift, RateSheet.swift, CheckoutSheet.swift
    Create/       CreateFlowView.swift, WhereStep.swift, WhenStep.swift, VibeStep.swift, ReviewStep.swift, MoreOptionsSheet.swift
    Forum/        ForumView.swift, AreaSheet.swift, FilterSheet.swift
    Groups/       GroupsView.swift, ThreadView.swift, AlbumView.swift, SplitsView.swift, AddExpenseSheet.swift
    Account/      AccountView.swift, FriendsView.swift
  Models/         *.swift (section 8)
  Services/       APIClient.swift, LiveAPIClient.swift, MockAPIClient.swift, MockData.swift, WebSocketService.swift, VoiceInputService.swift, AuthStore.swift
  Resources/      Fonts/JetBrainsMono-{Medium,Bold,ExtraBold}.ttf, Assets.xcassets (AppIcon)
```

---

## 4. Design system (match exactly)

### 4.1 Color tokens
| Token | Hex | Use |
|---|---|---|
| `sage` | `#899E88` | **Main brand color.** Primary buttons, raised + button, active tab, selected chips, progress, stars, toggles "on", logo tile |
| `sageInk` (deep sage) | `#4A5F49` | Links, icons and any sage-colored **text** on light backgrounds |
| `ink` | `#18211C` | Primary text; **text on sage**; dark buttons (Done) |
| `cream` | `#F2F1EA` | App background |
| `white` | `#FFFFFF` | Cards, sheets, fields |
| `sageTint` | `#899E88` @ 20% (`#899E8833`) | Selected-soft chips, sidequest timeline blocks, info boxes |
| `clay` | `#C77B58` | Groups/people only (group dots, legend) |
| `clayTint` / `clayText` / `clayBorder` | `#F7E9E0` / `#6B3419` / `#E0B49B` | Group timeline blocks, "Group" chips |
| `transitBg` / `transitText` / `transitBorder` | `#EEF3FD` / `#1E3A8A` / `#9DB6EE` | Transit blocks (dashed border), "Free now" chips |
| `text2` | `#3A433D` | Secondary body text |
| `text3` | `#626A63` | Captions, eyebrows, placeholders |
| `line` | `#E3E4DC` | Separators, card borders, hour lines |
| `lineStrong` | `#CFD2C8` | Chip borders, grabbers, inactive dots |
| `field` | `#F8F7F2` | Inset text areas |
| `segmentBg` | `#E4E3DA` | Segmented control track |
| `muted` | `#858D87` / `#A9AFA6` / `#C3C7BD` | Inactive icons / empty stars / inactive borders |
| `danger` / `dangerBg` | `#B91C1C` / `#FDECEC` (text `#991B1B`) | Errors, "You owe", Remove |
| `success` / `successBg` | `#166534` / `#E6F4EA` | "Owes you", Connected, confirmation banners |
| Status | Open `#16A34A`, Online `#1D4ED8`, Not free `#858D87` | Status dots |

**Hard rule:** never put white text on sage (2.9:1). Text on sage is **ink** (5.5:1). Sage-colored text on cream or white uses **sageInk** (6.5:1). Users can't change the app color (there's no theme picker).

Add `Color(hex:)` and a `Theme` namespace with these names.

### 4.2 Typography
- **Headers use a coding font: JetBrains Mono** (bundle the OFL `.ttf` files Medium/Bold/ExtraBold and register them in `UIAppFonts`).
- **Everything people read uses Apple's system font (SF Pro)** through `.system`.
- Tracking values are in points.

| Style | Font | Size / weight | Tracking | Where |
|---|---|---|---|---|
| `largeTitle` | JetBrains Mono | 32 / ExtraBold | −1 | Tab screen titles ("Your SideQuests", "Forum", "Groups") |
| `setupTitle` | JetBrains Mono | 28 / ExtraBold | −0.8 | Setup + forgot-password step titles |
| `stepTitle` | JetBrains Mono | 26 / ExtraBold | −0.4 | Create step titles |
| `section` | JetBrains Mono | 19 / Bold | −0.4 | Section headers ("Active itineraries", "Taste profile") |
| `wordmark` | JetBrains Mono | 40–52 / ExtraBold | −2 | "Side" in sage + "Quests" in ink |
| `sheetTitle` | SF | 26 / Bold (event sheet), 20–22 / Bold (other sheets) | 0 | Sheet titles |
| `eyebrow` | SF | 13 / Semibold, UPPERCASE | +0.4 | "FRIDAY, SEPTEMBER 25", "QUICK PICKS", "HOW FAR WILL YOU GO IN BETWEEN?" |
| `body` | SF | 15 / Regular, line spacing ≈ 1.35× | 0 | Default |
| `bodyStrong` | SF | 15–16 / Semibold | 0 | Row titles, card titles (16–17 Bold for itinerary/option titles) |
| `button` | SF | 17 / Semibold (primary), 14–15 / Semibold (small) | 0 | Buttons |
| `caption` | SF | 12–13 / Regular | 0 | Meta lines, helper text |
| `micro` | SF | 10–11 / Semibold | 0 | Tab labels, hour labels, avatar initials in stacks |
| `code` | JetBrains Mono | 30 / Bold | +10 | 6-digit code field |

Support Dynamic Type for SF styles (`relativeTo:`). Mono headers scale too, but keep `minimumScaleFactor(0.8)` with one line where the layout needs it.

### 4.3 Metrics
- Screen side padding **20**. Tab screens: header top padding 56 (under the status bar), title block to content 12.
- **Card:** white, radius **16** (continuous), padding **14**, no shadow. Rows inside grouped cards: 12–14 vertical padding, 1pt `line` separators.
- **Primary button:** height **52**, radius **14**, sage fill, ink 17 Semibold. **Dark button:** ink fill, white text. **Secondary:** white fill + 1pt `#C3C7BD` border (or cream fill inside sheets). **Link button:** no fill, sageInk Semibold.
- **Small pill button:** height 32–36, radius = height/2, 14–15 Semibold (e.g., "+ Plan", "Accept", "Edit photo").
- **Chips:** height 36 (32 in dense rows), radius 18 for pills or 12 for grid chips, 1pt border.
- **Segmented control:** track `segmentBg`, padding 2, radius 10; segments height 34, radius 8; selected = white + shadow `0 1 3 rgba(0,0,0,.12)` + Semibold; unselected Medium.
- **Toggle:** iOS switch style 52×32, on = sage, off = `#E3E4DC`, white 28pt knob.
- **Sheets:** white, top corner radius **24**, custom grabber 40×5 `lineStrong` radius 3, padding top 8 / sides 20 / bottom 34, backdrop black 35%. Use `.sheet` + `.presentationDetents` + `.presentationCornerRadius(24)` + `.presentationDragIndicator(.hidden)` with your own grabber.
- **Tab bar:** custom, height 84 including home indicator, `white` at 97% opacity, 1pt top border `line`, 5 equal columns, icons 26pt, labels 10pt Semibold. Active item = sage, inactive = `text3`. Center item: **raised + button**, 58pt circle, sage fill, ink plus (26pt, 2.4 stroke), 4pt cream ring, shadow `0 6 16 rgba(0,0,0,.18)`, offset up 24pt. Tabs: Home · Forum · + · Groups · Account.
- **Avatars:** circle, initials Semibold. The user's color comes from 5 options (Ink/white text, Sage/ink, Clay/ink, Forest `#4A5F49`/white, Sand `#D9CBB0`/ink). Status dot at the bottom-right: 14pt (header) or 20pt (Account), with a 2–3pt cream border. **Avatar stacks** overlap by −6 to −7pt, each with a 2pt white border.
- Touch targets ≥ 44pt everywhere.

### 4.4 Icons
Use SF Symbols in `.regular`/`.medium` weight to match the prototype's thin line icons: `house`, `person.2`, `plus`, `bubble.left`, `person.crop.circle`, `calendar`, `clock`, `mappin`, `lock`, `globe`, `figure.walk`, `tram`, `car`, `checkmark`, `xmark`, `mic`, `star`/`star.fill`, `camera`, `photo`, `slider.horizontal.3`, `magnifyingglass`, `chevron.left/right/up/down`, `arrow.up` (send), `envelope`, `key`, `arrow.clockwise`.

The **drag handle** is a custom shape: two horizontal lines (14pt wide, 6pt apart, 2.4pt stroke, round caps) in a 44×44 hit area.

---

## 5. Brand assets

### 5.1 Logo mark (build as a SwiftUI `Shape`/`Canvas`, never an image)
Everything is drawn in a **64×64 grid**; scale by `size / 64`.
- **Tile:** rounded square 64×64, corner radius 18 (continuous), fill **sage**.
- **Main road:** vertical dotted line from (32, 5) to (32, 59), stroke `#B9C7B8`, width 3, round caps, dash `[0.1, 7]`, which renders as dots.
- **S route** (ink `#18211C`, width 5, round caps/joins). **Author the path top → bottom** so trimming draws it the way you write an S:
  `M38 14 H29 C22.5 14 19 17.5 19 22 C19 26.5 22.5 30 29 30 H35 C41.5 30 45 33.5 45 38 C45 42.5 41.5 46 35 46 H17`
- **Pin:** circle center (17, 46), radius 4.5, fill cream, stroke ink 3.
- **Quest marker:** diamond (46,8) → (52,14) → (46,20) → (40,14), fill cream, stroke ink 3, round joins.
- Meaning (for the About screen / pitch): the dotted line is the main road, the plan you already had. The S leaves it and comes back: a side quest is a detour worth taking.
- **Variants:** sage (default and app icon), dark (ink tile, road `#3A433D`, sage route, ink-filled markers with sage outline), light (white tile, road `#CFD2C8`).
- **App icon:** export the sage variant at 1024×1024 without the rounded corners (iOS masks it).

```swift
struct LogoMark: View {
    var size: CGFloat = 84
    var drawProgress: CGFloat = 1      // 0...1, trims the S from the top
    var roadOpacity: Double = 1
    var pinOpacity: Double = 1
    var markerScale: CGFloat = 1
    // draw tile, road, S path (.trim(from: 0, to: drawProgress)), pin, diamond; scale by size/64
}
```

### 5.2 Wordmark
`Text("Side").foregroundStyle(Theme.sage) + Text("Quests").foregroundStyle(Theme.ink)`, JetBrains Mono ExtraBold, tracking −2. It goes beside or under the mark and is never recolored.

---

## 6. Opening animation (every cold launch)

- Plays **every time the app starts cold** (not when it returns from the background), then goes to Login (or Home if signed in).
- Background cream. Centered stack: mark (132pt), 22pt gap, wordmark (40pt) with the tagline under it (15pt `text3`, "Turn waiting into wandering.").
- Timeline (seconds from appear):
  1. **0.08 Rise + spin:** the mark starts at `offset(y: +560)`, `rotationEffect(-540°)`, `scale 0.55`, `opacity 0`. It animates to 0 / 0° / 1 / 1 over **1.1s** with ease-out (`cubic-bezier(0.22, 1, 0.36, 1)`, or `.spring(response: 1.0, dampingFraction: 0.85)`). Opacity uses 0.25s.
  2. **1.25 Draw:** the road fades in (0.4s), then the S trims **0 → 1 over 0.75s** `easeInOut`, drawing **from the top-right down to the pin**.
  3. **2.0 Reveal:** the pin fades in and the diamond pops `scale 0 → 1` with overshoot (0.38s, `cubic-bezier(0.34, 1.56, 0.64, 1)`). The wordmark and tagline fade in and move up 14pt (0.45s).
  4. **3.5:** crossfade to Login.
- Reduce Motion: show the finished logo with a 0.3s fade, then go on.
- Mirror `project/Brand.dc.html` frames 01–04 and the `Splash` artboard.

---

## 7. Screens (layout, copy, behavior)

All copy below is final. Keep it word for word, including sentence case and the curly quotes/em dashes where shown.

### 7.1 Root navigation
- `RootView` routes to: Splash (cold launch) → Auth (Login, Forgot password, Profile setup) → Main.
- Main is a `ZStack` of the current tab + the custom tab bar. **Full-screen flows hide the tab bar:** Create (presented with `fullScreenCover` from +), Thread (group chat, DM), Profile setup, Forgot password.

### 7.2 Login (email + password only)
- Cream background, side padding 28, top 72.
- Logo mark 84 → wordmark 44 → "Turn waiting into wandering." (21 Semibold ink) → "Tell us how long you've got and what you're in the mood for. We'll plan the rest." (16 `text2`).
- Bottom group (12pt gaps):
  - **Email** field and **Password** field. Each is a white card, radius 14, 1pt `line` border, a small grey label on top and 17pt input. Password has a "Show"/"Hide" link in sageInk.
  - Error line in red, if any.
  - "Forgot password?" link, right-aligned, sageInk 14 Semibold.
  - **Sign in** (primary). Return key also signs in.
  - "New here? **Create an account**" (the bold part in sageInk) → Profile setup, carrying the typed email over.
  - "By signing in you agree to the Terms and Privacy Policy." 12 `text3`.
- Validation on submit: "Enter a valid email address." / "Enter your password." Borders turn `#E5A3A3`.
- No Apple/Google buttons.

### 7.3 Forgot password (4 steps)
- Full screen, cream. Header row: "‹ Back" (sageInk 17) + **progress dots** centered (active 22×6 pill sage, done 6×6 sage, future 6×6 `lineStrong`).
- Each step: 56pt rounded-16 tile with `sageTint` background and a sageInk icon, then the `setupTitle`, 16pt body, fields, primary button.
  1. **"Forgot your password?"** "Enter the email you signed up with. We'll send you a 6-digit code." Email is prefilled from Login. **Send code.** "Remembered it? **Sign in**".
  2. **"Check your email"** "We sent a 6-digit code to **{email}**. It expires in 10 minutes."
     - Code field: numeric, `.oneTimeCode`, max 6 digits, JetBrains Mono 30 Bold, tracking 10.
     - **Verify code** button.
     - "Didn't get it? Check spam, or **resend code**". Resending shows green "New code sent."
     - "Use a different email" goes back to step 1.
     - Error: "Enter the 6-digit code from the email."
  3. **"Set a new password"** "Pick something you haven't used here before." New + Confirm fields (one Show/Hide toggles both) and the **password rules checklist** (7.4). **Reset password** stays grey `#C3C7BD` until all rules pass.
  4. Success: 76pt sage circle with an ink checkmark, **"Password updated"**, "You're all set. Sign in with your new password. We also signed you out on other devices." **Back to sign in** returns to Login with the email prefilled and the password empty.
- API: `POST /auth/password/forgot`, `/verify` (→ reset token), `/reset`, `/resend`.

### 7.4 Profile setup (5 steps, after "Create an account")
- Header: "‹ Back", "Step N of 5" (15 Semibold `text3`), "Skip" (from step 2 on). A 4pt progress bar under it (track `segmentBg`, fill sage, width N×20%).
- Footer: a full-width primary button in a cream footer with a top border.
  - Labels: "Continue". On step 2 with nothing connected: "Continue without a calendar". On the last step: "Finish setup".
- **Password rules** (shared with reset):
  - "At least 8 characters", "Includes a number", "Both passwords match".
  - Each rule has an 18pt circle: `lineStrong` when not met, sageInk with a white check when met.
- Steps:
  1. **"Let's set up your profile"** "This takes about a minute. It helps us pick things you'll actually like."
     - Avatar (72) + "Add a photo" / "Change photo" sage pill → **Photo sheet** (7.10). Caption: "Optional · friends see it on plans and in chats".
     - Fields: Name, Email, **Create a password**, **Confirm password** (+ rules), Username (optional, "@handle"), **Date of birth** (date picker, max today).
     - Age note, a `sageTint` box with a lock icon, text depending on age:
       - none: "Only used to recommend age-appropriate events, like 21+ nights. Never shown to others."
       - under 13: red box, "You need to be 13 or older to use SideQuests."
       - 13–17: "Age N: we'll only suggest all-ages events and hide 18+ and 21+ ones. Never shown to others."
       - 18–20: "Age N: we'll hide 21+ events (bars, some concerts). Never shown to others."
       - 21+: "Age N: 21+ events can show up in your suggestions. Never shown to others."
     - Continue is **blocked** until the name, a valid email and the password rules all pass. On a failed tap, a red box lists the problems: "Add your name." / "Enter a valid email address." / "Your password needs 8+ characters, a number, and both entries must match."
  2. **"Connect your calendar"** "We only read when you're busy or free, so we can plan around your schedule and spot gaps."
     - Rows for **Google Calendar** and **Outlook Calendar**: 40pt tile with a calendar icon, name, sub "Not connected" / "Connected · reading free/busy only", and a pill "Connect" (sage) / "✓ Connected" (success tint).
     - Note box: "We never post to your calendar or read event details without asking. You can disconnect anytime in Account."
  3. **"What do you enjoy?"** "Rate each from 1 (not for me) to 5 (love it). Skip any you're not sure about."
     - One card, one row per type: Outdoors & parks, Food & drinks, Museums & art, Live music, Nightlife, Sports & games, Shopping & markets, Big crowds, Early mornings, Long walks.
     - Each row shows the name + value label on the right ("4 · Like it" in sageInk, or "Not rated"), then a 5-column grid of 36pt buttons 1–5.
       - Selected: sage fill, ink text. Numbers below the selected one: `sageTint`. Others: cream. Tapping the selected number again clears the row.
       - Labels: 1 Not for me, 2 Rather not, 3 Neutral, 4 Like it, 5 Love it.
     - Then "WHO DO YOU USUALLY GO WITH?" Solo / Small group / Big group, and "YOUR USUAL PACE" Chill / Balanced / Packed. Both are single-select grid chips (selected sage).
  4. **"Money preferences"** "So we suggest things you're comfortable paying for. You can change these anytime."
     - "TYPICAL SPEND PER SIDEQUEST": Free only / Under $15 / $15–40 / $40+ (2×2). This becomes the default budget in Create.
     - "IF A GREAT OPTION COSTS A BIT MORE": Stick to my budget / A bit over is OK.
     - "SPLITTING WITH A GROUP": Split equally / Pay my own / Take turns. Note: "Sets the default for new expenses in Groups › Splits."
     - Toggle row: "Prefer free events" / "Show free options first when they fit" (on by default).
     - Card row: "Card for tickets and splits" / "Optional · add later in Account" → "Add Visa card" pill (sage) → "Added" (success) with sub "Visa •••• 4242 · default". Note: "The checkout agent always asks before it spends anything."
  5. **"Tell us more"** "Optional. Type or tap the mic and just talk. The more you share, the better the first picks."
     - Three cards. Each has the question (Semibold), a 40pt mic button on the right (`sageTint` → red `#B91C1C` while listening, with a 6pt red halo), "Listening… tap the mic again to stop" in red while recording, and a text area (radius 12, `field` fill):
       - "Describe your perfect free afternoon." (placeholder "e.g. a long walk somewhere green, then tacos with a couple friends")
       - "What's something you'd never want to do on a sidequest?" ("e.g. anything with huge crowds or long lines")
       - "Anything we should plan around?" ("Budget, dietary needs, accessibility, no car, etc.")
     - Transcripts are appended to the text. Footer "Voice input · Apple Speech".
- On finish: `POST /auth/signup` (if not already done in step 1), `PUT /me/preferences`, then Home. These answers seed the taste profile.

### 7.5 Home
- Header:
  - Eyebrow with today's date ("FRIDAY, SEPTEMBER 25") and the large title **"Your SideQuests"**.
  - Right side: the user's avatar (44) with status dot. Tapping it opens Account.
- Segmented control (3): **Itineraries | Calendar | Past**.

**7.5a Itineraries**
- If there are unrated past events, show a white card button at the top:
  - 40pt `sageTint` tile with a star.
  - "N past events to rate" / "Ratings tune what we suggest next".
  - "Rate" link → switches to Past.
- Section "Active itineraries". A horizontal row of **itinerary cards**:
  - 212pt wide, white, radius 16. Selected ring 2pt sage; others 1pt `line`.
  - Content: title 16 Bold, "Today · 1–8 PM" 13 `text3`, a row of 6pt color bars (one per block, colored by kind), "3 stops · 3 going" 13 Semibold.
  - The row ends with a dashed "+ New sidequest" tile, 120 wide.
- Header row: selected itinerary title (section style) + page dots (7pt; active sage, others `#C3C7BD`).
- **Swipeable timeline carousel:**
  - One card per itinerary, 350 wide, 12pt gaps, snaps per card (`.scrollTargetBehavior(.viewAligned)`, 20pt content margins).
  - Swiping changes the selected itinerary card; tapping a card scrolls to its timeline.
- **Timeline card** (white, radius 16, height ~480):
  - Hours from the itinerary's first to last hour (demo 1–8 PM).
  - **64pt per hour**, top inset 14.
  - Hour labels in a 46pt right-aligned column, 11pt `text3`, with 1pt `line` rules.
  - **Blocks:** left 62, right 12, radius 10, 1pt border, 2pt gap between blocks. Tall blocks (≥50pt) show the title (14 Semibold) and "2:30–4:00 PM · Sidequest" (12, 85%). Short blocks show one line: "Title · time" (12 Semibold).
  - Block kinds:
    - Calendar/busy: `#EFEFF4` bg, `#CFD2C8` border, `text2`
    - Sidequest: `sageTint` bg, sage@40% border
    - Transit: transit colors with a **dashed** border
    - Group: clay tint/border/text, plus an **avatar stack** (22pt) and "3 going · 1 interested"
  - **"Now" line:** 2pt sage with a 10pt sage dot, starting at x=56.
  - Tapping a block opens the **Event sheet** (7.6).
- Footer hint: "Swipe to switch itineraries · tap a block to open it" (12 `text3`, centered).

**7.5b Calendar**
- Day chip strip, horizontally scrollable, starting at Today (demo: Fri 25 → Sun Oct 4).
  - Chips 50×66, radius 14, white. Weekday 11 Semibold (the first chip says "Today"), day number 19 Bold, up to 3 colored 5pt dots (busy grey `#858D87`, sidequest sage, group clay).
  - Selected chip: sage fill, ink text, dots turn white.
- **Swipeable day panels:**
  - 350 wide, snapping. Header: "Friday, Sep 25" (17 Bold) + "Today · 5 on your calendar" (12 `text3`) + "+ Plan" pill (`sageTint` / sageInk).
  - Timeline **6 AM–11 PM at 30pt/hr** (top inset 10), labels every 2 hours (10pt), blocks at left 58 (radius 8, 12pt Semibold "Title · 9:30–10:45 AM"). Group blocks show a 20pt avatar stack.
  - Swiping a panel selects its chip; tapping a chip scrolls to its panel.
- **No "free time" entries anywhere.**
- **Press and hold to plan** (like Google Calendar):
  - Press and hold empty time in a day panel (~0.4 s, haptic) to create a 1-hour window there, snapped to 15 minutes. Keep holding and drag up or down to stretch it; the minimum is 30 minutes. A quick tap or an ordinary swipe does nothing, so the page still scrolls.
  - While holding, show a box: 2pt sage border, `sageTint` fill, "New sidequest" (12 Bold sageInk) and "2:00–5:00 PM". After release, drag its top or bottom edge to adjust it.
  - On release, show under the box: **"Plan this window"** (sage, 36 tall, radius 10, shadow) + a 36pt ink square with ×.
  - "Plan this window" opens Create with the date, start and end prefilled.
  - Horizontal swipes page days; vertical swipes scroll.
- Footer hint: "Press and hold on empty time to plan a sidequest · swipe for more days".

**7.5c Past**
- Intro: "Events you went to. Tap one to rate it. Ratings update your taste profile."
- Grouped by date (eyebrow "THURSDAY, SEP 24"), in white cards. Each row:
  - 10pt dot (sage, or clay for group events), title Semibold, sub "Tech Square · with 3 others".
  - If rated: 14pt stars on the right, and a tag line under the title ("Would go again · Great people · "note"").
  - If unrated: a sage "Rate" pill.
- **Rate sheet:**
  - "{place · who}" caption, **"How was {title}?"** (22 Bold).
  - 5 stars, 40pt each in 52pt targets (filled sage, empty `#A9AFA6` outline), with a word under them: Tap a star / Not great / Meh / It was fine / Really good / Loved it.
  - "WHAT STOOD OUT? (OPTIONAL)" multi-select chips: Would go again, Great people, Good value, Too crowded, Too pricey, Hard to get to. Selected: `sageTint`, sageInk, sage border.
  - "Anything else? (optional)" text area.
  - **Save rating**, disabled grey "Pick a star rating" until a star is chosen. Saving calls `PUT /ratings/{itemId}`.

### 7.6 Event sheet (tap any block)
- Height ~660, scrollable.
- Top row: kind chip ("Sidequest", "Transit", "Calendar", "Group" in that kind's colors) + a 32pt round × button.
- Title (SF 26 Bold). Rows with icons: time ("2:30–4:00 PM") and place.
- Description (15, line spacing 1.45).
- **WHO'S IN** (group events only):
  - A cream box with a horizontal row of 40pt avatars.
  - Each has a 2pt ring (sage for Going, `lineStrong` for Interested), with the name (11) and "Going"/"Interested" (10) under it.
- **GETTING THERE** (not for busy blocks): 3-column choice cards: Walk "18 min · Free", MARTA "12 min · $2.50", Rideshare "8 min · [fare]". Selected: `sageTint` + 2pt sage ring.
- Buttons row:
  - **Website** (cream, globe icon) → in-app Safari sheet.
  - **Get tickets** (sage) → Checkout sheet. Only shown for bookable events.
- **NOTES:** a text area with scope on the right ("Only you" for solo, "Shared with the group" for group events). Placeholder "What to bring, where to meet, reminders…". Saves through `PATCH …/items/{id}`.
- "Rate it after" / "Tunes your future picks" with 5 tappable stars (sage fill).
- **No "Why this" box.**
- **Agent checkout sheet (Visa):**
  - "Agent checkout" + ×.
  - Step list with check circles: "Found tickets on the official site", "Filled in your name and email" (green), "Waiting for your approval" (grey).
  - Cream receipt: "1 × {title} $…", "Fees", "Total" (bold, top rule).
  - Card row: navy `#1A1F71` "VISA" badge, "•••• 4242", "Change".
  - **Approve purchase** (ink). "The agent can't spend anything until you approve."
  - Success: 64pt green check, "Booked", "Your ticket is saved to this itinerary. The group sees it too.", **Done** (sage).
  - API: `POST /checkout/intents` → `…/approve`.

### 7.7 Create flow (+ button, full screen)
- Header: "Cancel" (sageInk 17, left) · "New sidequest" (17 Semibold, center).
- **Stepper:** 4 circles, 28pt, labeled **Where · When · Vibe · Review** (12pt).
  - Done: sage fill with an ink check. Current: sage fill, ink number, bold label. Future: white fill, 1.5pt `#C3C7BD` border, grey number.
  - 2pt connector lines, sage when the step is done.
  - Tapping a step jumps to it.
- Footer (cream, top border): "Back" (white, 110 wide) + primary: "Next", or on Review "Start this sidequest".

**Where**
- **"Where do you start and end?"**
- White card with 2 rows:
  - **A** (the logo's ring pin, `RouteMarker(.start)`, 26pt) "Start" / "Tech Square (current location)".
  - **B** (the logo's diamond quest marker, `RouteMarker(.end)`) "End · where you need to be by the end time" / "Home · North Ave Apts" (or "Same as start").
  - The active row is tinted with an "Editing" label.
  - "End where I start" toggle.
- Search field ("Search for your start/end location") + up to 4 suggestion pills (MKLocalSearch results; the first is "Current location").
- **Map** (350×220, radius 16): tap to drop the active pin. Pin A is the logo's ring and pin B its diamond (`RouteMarker(…, onMap: true)`, 28pt, white halo and shadow). Bottom-left chip: "Tap the map to drop the Start/End pin". **No line between A and B.**
- "HOW FAR WILL YOU GO IN BETWEEN?": Walkable ≤ 15 min / Transit ≤ 30 min / Anywhere (3 cards; selected sage).
- "CAN YOU PROVIDE A RIDE THIS TIME?" (asked for **every** sidequest):
  - **I can drive** (Own car), **I'll cover rides** (Uber / Lyft), **No ride** (Walk + transit).
  - If driving, show an "Open seats" 1–4 selector.
  - Note by choice:
    - "We'll plan driving legs and parking. If the plan is shared, others see you can give rides."
    - "We'll plan rideshare legs and show the estimated cost split per person."
    - "We'll keep it to walking, MARTA and bus. You can still join someone else's ride."

**When**
- **"When are you free?"**
- Fields: Date, then two columns: **Start** and **Back at end by** (native pickers inside white field cards).
- Summary box (`sageTint`, pin icon): "Leave {start place} at 2:10 PM. We plan backwards so you're at {end place} by 6:30 PM."
- **Collapsible "Your calendar · Fri, Sep 25"** row with a chevron. It expands into that day's calendar items (dot, time column 118pt, title), or "Nothing on your calendar this day."
- **No auto-picked slots.**

**Vibe**
- **"What are you in the mood for?"**
- Voice card: 76pt round mic (sage, with an 8pt tint halo; red while listening). Labels: "Tap and say what you want" / "Listening… tap to stop". The live transcript shows in quotes. Footer "Voice input · Apple Speech".
- "Or type it" text area ("e.g. low-key, outdoors, under $20").
- "QUICK PICKS" multi-select pills (36 tall), selected = sage: Outdoors, Food, Art, Music, Chill, Active, Meet people, Nerdy, Nightlife.
- "BUDGET": Free / $ / $$ / $$$. The default comes from money preferences.
- "WHO'S COMING": Just me / Friends only / Open to all, with a note:
  - "Private. Nobody else sees this plan."
  - "Posted to the Forum, but only your friends can see and join it."
  - "Posted to the Forum for anyone nearby to request to join."

**Review — "Pick a sidequest"**
- Summary line: "Fri, Sep 25 · 2:10 PM–6:30 PM · 3 options · drag stops to reorder".
- **Options carousel:**
  - Cards 240 wide. "OPTION A" (12 Bold sageInk) + tag chip ("Best match", "Chill", "Meet people"), name 17 Bold, "Stop → Stop → Stop" 13, meta "~$ · 1.8 mi walking · 2 transit legs". Selected ring 2pt sage.
  - The **last card is a dashed "Load more options"** tile (150 wide): 40pt sage circle with a refresh icon, "Load more options", "2 more that fit your window".
    - Tapping it shows "Finding more…" / "Checking what fits your window", then appends 2 options and selects the first new one.
    - When there are none left: "No more right now" / "Try changing filters in More options" (grey icon).
    - The summary count updates. API: `POST /plans/generate/more`.
- Above the route: "Drag ☰ to reorder stops" (with the two-line glyph) on the left, and a status on the right: "Recalculating transit…" (grey), then "Transit times updated" (green).
- **Route card** (white, radius 16). Vertical rail on the left (2pt `lineStrong` line):
  - **A** point row: the ring marker, start place, "Leave 2:10 PM".
  - Leg rows: hollow 8pt blue ring, "MARTA · 14 min" (13 Medium, transit blue).
  - **Stop** rows: 20pt sage circle with an ink number, title 16 Semibold, "2:24 PM–3:44 PM · Games + views · $", and the **drag handle** on the right. No Swap button.
  - **B** point row: the diamond marker, end place, "Arrive 5:57 PM · back by 6:30 PM ✓", or in red "Arrive 6:45 PM · 15 min past 6:30 PM".
- **Drag to reorder:**
  - Drag the handle. The lifted row gets a white fill, shadow `0 8 24 rgba(0,0,0,.18)` and a 2pt sage ring.
  - Rows swap live when the finger passes a neighbor's midpoint. VoiceOver/keyboard: an adjustable action moves the stop up or down.
  - On drop: every leg shows "Updating transit…" for ~0.9s while calling `POST /plans/route`. Then leg modes and minutes, stop times and arrival all update. The option card's stop list follows the new order.
  - Leg mode rule: ≤0.8 mi walks; otherwise Drive / Uber / MARTA from the ride answer.
- **"More options"** row (white, 50 tall: sliders icon, "More options", "Check your settings", chevron) → a cream sheet, "More options" / "Check everything before you start" with a Done pill:
  - Rows with an **Edit** pill that jumps to the step: Start, End, Date, Time window ("2:10 PM – back by 6:30 PM"), Mood, Budget, Who's coming, Ride ("Driving · 3 open seats").
  - "GETTING AROUND" multi toggles: Walk / MARTA / Rideshare.
  - "PACE": Relaxed / Balanced / Packed.
  - If shared, "GROUP": Lock joining at 1:30 PM, Max group size 6.
  - "Regenerate options with these settings" (outlined sage).
- "Start this sidequest" → `POST /itineraries` → Home, with the new itinerary selected.

### 7.8 Forum
- Title **"Forum"**. Area pill (white, 34 tall): pin icon + "Midtown Atlanta · 2 mi" + chevron → **Area sheet**:
  - "Forum area" / "See posts and plans from this area."
  - Search field, a map preview with a radius circle.
  - Rows: Current location, Midtown Atlanta, Georgia Tech campus, Downtown Atlanta, Decatur (check on the selected one).
  - Radius chips 1/2/5/10 mi, Done.
- Segmented: **Everyone nearby | Friends**.
- Post-your-status card:
  - Not posted: "Bored right now?" / "Post that you're free. Choose who sees it." with two buttons: **Friends only** (outlined) and **Everyone nearby** (sage).
  - Posted: blue `#EEF3FD` card, "YOUR POST · LIVE", "Free until 6:30 PM near Tech Square", "Visible to your friends only" / "Visible to everyone within 2 mi of Midtown Atlanta", "Take down".
- Chips row: All / Open plans / Free now (selected = ink fill, white text) + a right-aligned **Filter** pill (sliders icon). With filters active it reads "Filter · 2" (tinted).
- Results line: "4 results · sorted by closest" + "Clear filters".
- **Sort & filter sheet:**
  - "Sort & filter" + "Clear all".
  - SORT BY list with a check: Soonest, Closest, Most spots left, Newest.
  - WHEN: Any / Now / Today / Weekend.
  - DISTANCE: Any / ≤ 1 mi / ≤ 2 mi / ≤ 5 mi.
  - COST (ANY IF NONE PICKED): Free / $ / $$ / $$$ (multi).
  - INTERESTS (multi): Outdoors, Food, Art, Music, Active, Games, Shopping.
  - Toggle "Only plans with open spots" / "Hides free-now posts and full plans".
  - Button: "Show N results".
- **Post cards** (white, radius 16, padding 14):
  - Header: 36pt avatar, name + "Friend" badge (cream pill), meta "Hosting · 0.4 mi away", right chip "Open plan" (`sageTint`/sageInk) or "Free now" (blue).
  - **Open plan:** title 17 Bold; "Today · 5:30–8 PM"; route line; avatar stack (26pt) + "4 going · 2 interested"; spots progress bar (6pt, sage on `line`); "2 of 6 spots left" · lock icon "Locks 5:00 PM".
    - Button: **Request to join** (sage), then "Requested · waiting on host" (cream).
  - **Free now:** the text of the post. Button: **Plan together** (outlined sage), then "Message sent", which opens a DM.
  - Friends-only posts add the note "Shared with friends only".

### 7.9 Groups, threads, album, splits
- **Groups list:**
  - Eyebrow "CHATS, ALBUMS, SPLITS", title "Groups".
  - One white card of rows: 2-avatar overlap (32pt), name, time, last message (1 line), chips ("3 people", "You owe $9", "9 photos").
  - Footer note: "A group is created when someone joins your plan. Each one has a chat, a shared photo album and a running tab of who owes whom."
- **Thread** (full screen): white header with "‹ Back", centered title + sub ("3 people · Today 6 PM"). Groups also get a segmented **Chat | Album | Splits**.
  - **Chat:**
    - Bubbles radius 18, max 260 wide. Mine: sage with ink text, on the right. Others: white, on the left, with the sender name (11) above for groups.
    - "Today" divider.
    - Composer bar (white, 84 tall): rounded field "Message" + 38pt sage send button (arrow up). Return sends.
  - **Album:** "Krog St dinner · Sep 25" / "9 photos · 3 people" + "Add photos" pill. 3-column square grid (4pt gaps, radius 6), each with a small "by {name}" label. Upload through PhotosPicker.
  - **Splits (equal split only):**
    - Balance card: "Your balance in this group" → "You owe $9.00" (red) / "You're owed $X" (green) / "All settled", with "N expenses · all split equally" under it.
    - Per-person rows: avatar, "Maya owes you" (green $2.50) / "You owe Dev" (red $11.50) / "Settled with …".
    - "EXPENSES · SPLIT EQUALLY" list: each row has the title, "Dev paid · split equally, 3 people", the total, and share chips ("You $14.00" highlighted in `sageTint`, others cream).
    - Buttons: **+ Add an expense** (sage), and **Settle up $X with Visa •••• 4242** (ink, only when you owe).
- **Add expense sheet** (verified before saving):
  - Fields: What was it for? · Total amount ($ with 26pt Bold input) · PAID BY (member chips) · SPLIT BETWEEN (member toggles with a check) · "How it's split: Equally · Total ÷ N people".
  - **CHECK THE SPLIT** card (border turns green when valid): "$13.33 each", a row per person ("Your share (you paid) $13.34", "Maya owes you $13.33"…).
    - Rounding note: "Rounded to the cent: the first person pays 1¢ more so it adds up exactly."
    - "Shares add up to $40.00 of $40.00".
  - Errors after a failed save: "Add what the expense was for." / "Enter an amount above $0." / "Pick at least one person to split with."
  - Button: grey "Add expense" → sage "Add $40.00 · split 3 ways".
  - After saving, a green banner: 'Added "Pizza" · $40.00 split equally 3 ways ($13.33 each). Everyone was notified.'
  - Split math: `base = floor(cents / n)`, and the first `cents mod n` people pay +1¢. The server is authoritative (`POST /groups/{id}/expenses`); the client computes the same numbers for the preview.
- **DM:** same Thread view without tabs. The sub-line shows the friend's status.

### 7.10 Account
- Centered header:
  - Avatar 84 (photo or initials) with a 20pt status dot.
  - "Edit photo" pill (camera icon) → **Photo sheet**.
  - Name 24 Bold, "@jordanlee · Georgia Tech" 14 `text3`.
- **YOUR STATUS** card: 3 segment-like buttons, each with a 10pt dot:
  - **Open** (green): "Free and down for plans. Friends see you in the Forum and can invite you."
  - **Online** (blue): "Around and reachable, but not looking for plans right now."
  - **Not free** (grey): "Hidden from the Forum and from friends' free lists."
  - Selected: white fill + 2pt ring in the dot's color.
- Segmented **Me | Friends**.
- **Me:**
  - "Taste profile" bars (label 84pt column + 8pt bar in sage: Outdoors, Food, Art, Social, Nightlife) + "Starts from your setup answers, then learns from your ratings. Each new sidequest mixes this with the mood you describe." + "Redo setup questions" pill (→ setup step 3).
  - "Connected" rows: Google Calendar · Connected, Location · While planning, Visa •••• 4242 · Agent checkout on, Voice input · On.
  - "Past sidequests" + "See all & rate" (→ Home › Past), with rows showing 14pt stars.
  - Sign out (white, red text).
  - **No stats row and no app-color option.**
- **Friends:**
  - Search field ("Name or @handle") + "Invite" (sage).
  - REQUESTS card: "Chris N." / "Met on Stone Mountain sunrise" + Accept.
  - "N FRIENDS" list rows: 40pt avatar with a status dot, name, status line ("Free until 8 PM", "On a sidequest · Thrift crawl", "Busy until 5 PM"), and a 40pt `sageTint` message button → DM.
- **Photo sheet:**
  - "Profile photo" (Mono 22 ExtraBold). 120pt preview.
  - **Take photo** (sage) / **Choose photo** (cream).
  - "OR USE YOUR INITIALS": 5 × 52pt colored circles; the selected one gets a double ring.
  - "Remove photo" (red, only when a photo is set). **Done** (ink).
  - Upload: `POST /me/photo`.

---

## 8. Data models (Swift)

```swift
struct User: Codable, Identifiable { let id: String; var name: String; var username: String?; var email: String
  var photoURL: URL?; var avatarColor: AvatarColor; var status: PresenceStatus; var ageBracket: AgeBracket }
enum PresenceStatus: String, Codable { case open, online, notFree }
enum AgeBracket: String, Codable { case under13, teen, under21, adult }
struct Preferences: Codable { var ratings: [TripType: Int]; var company: Company; var pace: Pace
  var spend: SpendTier; var flexibility: Flexibility; var splitStyle: SplitStyle; var preferFree: Bool; var answers: [String: String] }
struct Place: Codable, Hashable { var name: String; var coordinate: Coordinate }
struct Itinerary: Codable, Identifiable { let id: String; var title: String; var date: Date; var start: Date; var backBy: Date
  var startPlace: Place; var endPlace: Place; var visibility: Visibility; var lockAt: Date?; var maxGroupSize: Int?; var items: [ItineraryItem] }
struct ItineraryItem: Codable, Identifiable { let id: String; var kind: BlockKind; var title: String; var place: Place?
  var start: Date; var end: Date; var description: String?; var websiteURL: URL?; var bookable: Bool; var priceCents: Int?
  var people: [PersonRef]; var interested: [PersonRef]; var notes: String?; var rating: Rating? }
enum BlockKind: String, Codable { case busy, sidequest, transit, group }
struct PlanRequest: Codable { var start: Place; var end: Place; var date: Date; var startTime: Date; var backBy: Date
  var range: TravelRange; var ride: RideChoice; var openSeats: Int?; var moodText: String; var tags: [String]
  var budget: Int; var who: Visibility; var pace: Pace; var modes: Set<TravelMode> }
struct PlanOption: Codable, Identifiable { let id: String; var name: String; var tag: String; var meta: String; var stops: [PlanStop] }
struct RouteResult: Codable { var legs: [Leg]; var stopTimes: [DateInterval]; var arrival: Date; var minutesLate: Int }
struct Leg: Codable { var mode: TravelMode; var minutes: Int }
struct ForumPost: Codable, Identifiable { /* type: plan|freeNow, author, isFriend, friendsOnly, title/text, when, route, distanceMi,
  priceTier, tags, spotsLeft, capacity, lockAt, going: [PersonRef], interested: [PersonRef], postedAt */ }
struct Thread: Codable, Identifiable { /* isGroup, title, subtitle, members, lastMessage, unread */ }
struct Message: Codable, Identifiable { let id: String; var senderId: String; var text: String; var sentAt: Date }
struct Expense: Codable, Identifiable { let id: String; var what: String; var amountCents: Int; var payerId: String; var splitAmong: [String] }
struct Balance: Codable { var userId: String; var netCents: Int }   // + = they owe you
struct Rating: Codable { var stars: Int; var tags: [String]; var note: String? }
struct CheckoutIntent: Codable, Identifiable { /* steps, subtotalCents, feesCents, totalCents, cardLast4, state */ }
```
Store money as `Int` cents and format with `FormatStyle.Currency(code: "USD")`.

---

## 9. API (backend contract)

REST + JSON, bearer token (except `/auth/*`), money in cents, ISO 8601 dates with time zones, cursor pagination. Full list: project doc `claude/sidequestz-api-endpoints.md`.

- **Auth:** `POST /auth/signup` · `/auth/login` · `/auth/refresh` · `/auth/logout` · `/auth/password/forgot` · `/auth/password/verify` · `/auth/password/reset` · `/auth/password/resend`
- **Me:** `GET/PATCH /me` · `POST/DELETE /me/photo` · `PATCH /me/avatar` · `GET/PUT /me/preferences` · `GET /me/taste-profile` · `POST /me/devices`
- **Integrations/payments:** `POST /integrations/{google|outlook}/connect` · callback · `DELETE /integrations/{provider}` · `GET/POST/DELETE /me/payment-methods`
- **Calendar/places/events:** `GET /calendar/days?from&to` · `GET /places/search` · `GET /places/reverse` · `GET /events` · `GET /events/{id}`
- **Planning:** `POST /plans/generate` (3 options + cursor) · `POST /plans/generate/more` · `POST /plans/route` (reorder → legs, times, late flag) · `POST /itineraries`
- **Itineraries:** `GET /itineraries?status=active` · `GET/PATCH/DELETE /itineraries/{id}` · `PATCH /itineraries/{id}/items/{itemId}` (notes) · `GET …/items/{itemId}/transit`
- **Past/ratings:** `GET /me/past-events?unrated=` · `PUT /ratings/{itemId}`
- **Checkout (Visa agent):** `POST /checkout/intents` · `GET /checkout/intents/{id}` · `POST …/approve` · `POST …/cancel`
- **Forum:** `GET /forum/posts?lat&lng&radius&scope&type&when&max_dist&cost&tags&open_only&sort` · `POST /forum/posts` · `DELETE /forum/posts/{id}` · `POST /forum/posts/{id}/join-requests` · `POST /forum/posts/{id}/plan-together`
- **Threads:** `GET /threads` · `GET/POST /threads/{id}/messages` · `POST /threads/dm`
- **Album:** `GET/POST /groups/{id}/photos` · `DELETE /groups/{id}/photos/{photoId}`
- **Splits:** `GET/POST /groups/{id}/expenses` · `DELETE …/{expenseId}` · `GET /groups/{id}/balances` · `POST /groups/{id}/settle`
- **Friends:** `GET /friends` · `GET /users/search?q` · `GET/POST /friends/requests` · `POST …/{id}/accept|decline` · `DELETE /friends/{id}` · `POST /invites`
- **Realtime:** `WS /ws` events: `message.new`, `join.request`, `friend.status`, `forum.update`, `checkout.status`, `transit.delay`

**The server owns:** plan generation (Gemini), transit re-timing, the equal-split math, age filtering of events, join limits and lock times, and taste-profile updates. **The client** previews splits and validates forms, but always shows the server's result.

---

## 10. Mock data (so the demo runs offline)

Set the demo in **Atlanta, Friday Sep 25**, with user **Jordan Lee (@jordanlee, JL)**. Copy the exact sample data from `renderVals()` in `project/Main.dc.html`. Key pieces:

- **Itinerary "Free Friday afternoon" · Today · 1–8 PM · 3 going:**
  - CS 3510 lecture 1:00–1:50 (busy)
  - Transit to Ponce City Market 2:00–2:25
  - **Skyline Park rooftop** 2:30–4:00 (bookable)
  - Walk the Eastside Trail 4:00–4:20
  - **Krog Street Tunnel murals** 4:20–5:30
  - **Group dinner, Krog Street Market** 6:00–7:30 (group: JL, Maya, Dev going; Ava interested)
- **"Saturday reset" · Sat · 1–7:30 PM · Solo:**
  - Atlanta Botanical Garden 1:00–2:45
  - Walk to the Active Oval
  - Frisbee meetup 3:00–4:30 (group)
  - Call with family 5:00–5:30 (busy)
  - Sunset at Jackson Street Bridge 7:00–7:30
- **People:** Maya R. (MR `#1D4ED8`), Dev P. (DP `#0F766E`), Ava K. (AK `#B45309`), Sam T. (ST `#BE185D`), Chris N. (CN `#4338CA`), Priya K. (PK `#9D174D`).
- **Forum:**
  - Maya: "Sunset + tacos on the BeltLine", 2 of 6 left, locks 5:00 PM
  - Dev: free now near Tech Square (friends only)
  - GT Outdoors Club: "Stone Mountain sunrise hike", Sat 6–10 AM
  - Priya: layover at ATL
  - Sam: "Thrift crawl in Little Five Points" (friends only)
- **Groups:**
  - "Krog St dinner crew": Dumplings $42 paid by Dev, MARTA fares $7.50 paid by you, 3 people → you owe Dev $11.50, Maya owes you $2.50, net **You owe $9.00**
  - "Stone Mountain sunrise"
  - "Thrift crawl"
- **Review options:**
  - A "Rooftop + murals" (Best match)
  - B "Park + food hall" (Chill)
  - C "Downtown loop" (Meet people)
  - Load-more batches: D "Books + park", E "Games + views", then F "Gardens loop", G "Downtown sights"
- **Past:**
  - Board game café, Eastside Trail walk (unrated)
  - Jazz night in Decatur (5★)
  - Dinner before the show (unrated)
  - Atlanta Food Walk (4★)
  - Chattahoochee paddle (3★)
- **Calendar days:** Sep 25 → Oct 4 (classes, club meeting, midterm, trivia night, Stone Mountain hike, thrift crawl).
- Any **unknown real price** stays a visible placeholder like `$[price]`, `[fees]`, `[fare]`. Never invent one.

---

## 11. Accessibility and polish
- Contrast: follow the color rules in 4.1. The active tab uses sage as in the prototype. If an accessibility audit flags the 10pt labels, switch the active tab to sageInk; don't swap in another color.
- Every icon-only button gets an `accessibilityLabel` ("Open profile", "New sidequest", "Send message", "Reorder {title}"…).
- Drag-to-reorder and drag-to-plan need non-drag alternatives: `accessibilityAdjustableAction` for stops, and "+ Plan" for calendar windows.
- Reduce Motion: no spin or slide; use crossfades.
- Haptics: light selection on chips, segments and stars; medium on drop; success on booking, expense added and password updated.
- Empty, loading and error states for every network call. Loading: a small logo with the S drawing in on a loop. Error: a short sentence + "Try again".
- Keyboard: Return advances through fields; forms scroll the focused field above the keyboard.

---

## 12. Build order and done checklist

1. Design system (colors, fonts, metrics, all components), LogoMark + wordmark, preview gallery.
2. Splash animation → Login → Forgot password (4) → Profile setup (5) + Photo sheet. Mock auth.
3. Main shell: custom tab bar, raised +, routing, full-screen flows.
4. Home: itineraries (cards + swipe timelines + event sheet + browser + checkout), Calendar (chips, day pager, press and hold to plan), Past + rate sheet.
5. Create: Where (map + pins + ride), When, Vibe (voice), Review (options, load more, drag reorder + re-time, More options).
6. Forum (+ area and filter sheets), Groups (chat, album, splits, add expense), DMs, Account (status, Me, Friends).
7. Swap in `LiveAPIClient` screen by screen, then add the WebSocket.

**Done means:**
- [ ] Side by side with the artifact boards, each screen matches spacing, radii, colors, type and copy.
- [ ] Intro plays on every cold launch, draws the S from the top, then goes to Login; Reduce Motion is respected.
- [ ] No white text on sage anywhere.
- [ ] Headers are JetBrains Mono; body text is SF Pro.
- [ ] All validation messages above appear exactly as written.
- [ ] The split preview always adds up to the total, to the cent.
- [ ] Reordering stops re-times the route and flags lateness.
- [ ] The whole demo runs offline on mock data.

## 13. Defaults for undecided questions (use these unless told otherwise)
- Joining an open plan: requests auto-accept until the lock time or max size is reached (no host approval screen yet).
- Swap/remove/add stop on Review: not in v1; reorder only.
- Status doesn't switch automatically; the user sets it.
- Splits are equal only (no custom amounts).
- "Change password" in Account: not in v1 (users can reset from Login).
- The brand is "SideQuests" (Home title "Your SideQuests"); in other UI copy the word is spelled "sidequest(s)". The Xcode project, target and bundle ID keep the old internal name `SideQuestz`.