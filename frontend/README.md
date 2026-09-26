# Front-end (iOS app)

**SideQuests**, the HackGT 13 iPhone app, is built in SwiftUI from [GUI_PLAN.md](GUI_PLAN.md) and the
"SideQuestz Mock UI" design canvas. It's a GUI shell: every screen gets its data through one
API interface, so the backend, AI and database can be plugged in later. See
[API_CONTRACT.md](API_CONTRACT.md) for the backend contract.

By default the app runs fully offline on demo data (Atlanta, Friday Sep 25, user Jordan Lee), so you can try every screen before the backend exists.

## Project layout

```
frontend/
├── SideQuestz.xcodeproj        ← open this in Xcode
├── SideQuestz/
│   ├── App/                    ← entry point, AppEnvironment (mock vs live), Router, root + tab shell
│   ├── DesignSystem/           ← colors, type, metrics, components, logo, preview gallery
│   ├── Features/               ← Intro, Auth, Setup, Home, Create, Forum, Groups, Account
│   ├── Models/                 ← Codable models (the API's JSON shapes)
│   ├── Services/               ← APIClient (the contract), LiveAPIClient (REST), WebSocket, voice, places
│   │   └── Mock/               ← offline demo backend + data (replaced by your server)
│   └── Resources/              ← JetBrains Mono fonts (OFL), asset catalog + app icon
├── SideQuestzTests/            ← unit tests (split math, route timing, JSON contract…)
├── SideQuestzUITests/          ← end-to-end flows driven with real taps, typing and drags
├── HelloWorld.xcodeproj        ← the original starter app
├── API_CONTRACT.md             ← endpoints, JSON conventions, example payloads
└── GUI_PLAN.md                 ← the build spec
```

Any file you add inside `SideQuestz/` becomes part of the app automatically. You don't have to register it anywhere.

## Run it

1. Open `frontend/SideQuestz.xcodeproj` in Xcode.
2. Choose an iPhone simulator, or your phone (see [Run on your iPhone](#run-on-your-iphone)), in the toolbar.
3. Press **⌘R**. Every cold launch plays the opening animation, then shows the sign-in screen. Any valid email with any password signs you in to the demo. Demo calls take 5 seconds on purpose (`SQMockDelay` in Info.plist) so you can see the loading animations. Pull down on any main screen to reload everything from the API.

To see the intro again, long-press the logo on the sign-in screen.

### Demo shortcuts (launch arguments)

In Xcode, go to Product → Scheme → Edit Scheme → Run → **Arguments** and add these under "Arguments Passed On Launch":

| Argument | What it does |
| --- | --- |
| `-SQRoute home/calendar` | Opens straight to a screen, signed in. Also: `login`, `forgot/2`, `setup/3`, `home`, `home/past`, `home/sheet/a3`, `home/rate/x1`, `create/1`…`create/4`, `create/4/more`, `forum`, `forum/filter`, `groups`, `thread/g1/splits`, `thread/dm-maya`, `account/friends`, `gallery` (the design-system gallery) |
| `-SQSkipIntro YES` | Skips the opening animation, which otherwise plays on every cold launch |
| `-SQMockDelay 1` | Seconds each demo call waits (Info.plist `SQMockDelay`, 5 for now, so loading animations show). Delete the Info.plist key for realistic per-call timings |
| `-SQMockLatency 0` | Removes the demo backend's fake network delay |
| `-SQMockFail forum,itineraries` | Makes those demo endpoints fail, to see error states |
| `-SQVoiceDemo YES` | Voice buttons return sample transcripts instead of using the mic |
| `-SQAPIMode live` | Uses your real backend (details in [API_CONTRACT.md](API_CONTRACT.md#pointing-the-app-at-your-server)) |

### Tests

Press **⌘U** to run both suites (about 3 minutes, most of it the UI tests).

- **Unit tests** (`SideQuestzTests`, a few seconds) check that the equal-split preview always adds up to the total, to the cent; that reordering stops re-times the route and flags lateness, matching the prototype's numbers; the validation copy; time formatting; the demo backend's forum filters; and that every model round-trips through the API's JSON format.
- **UI tests** (`SideQuestzUITests`) run seven flows on the demo backend:
  - create an account through all five Setup steps;
  - reset a password and sign in with it;
  - plan and start a sidequest;
  - drag a stop to re-time the route;
  - press and hold on the calendar to plan a window;
  - "Plan together" into a DM;
  - add a $40 expense split three ways.

  Tab bar buttons have the accessibility identifiers `tab.home`, `tab.forum`, `tab.plan`, `tab.groups` and `tab.account`.

To run one suite from Terminal, pass `-only-testing:SideQuestzTests` (or `SideQuestzUITests`) to
`xcodebuild test -project SideQuestz.xcodeproj -scheme SideQuestz -destination 'platform=iOS Simulator,name=iPhone 17e'`.

## Hooking up the backend

- The contract is [`SideQuestz/Services/APIClient.swift`](SideQuestz/Services/APIClient.swift). The REST wrappers that call your server are in [`LiveAPIClient.swift`](SideQuestz/Services/LiveAPIClient.swift).
- Plan generation, ranking, transit re-timing, split math, age filtering and the taste profile belong to the server. The app shows whatever the server returns.
- Loading animations follow the real requests: skeletons and loaders show for exactly as long as your server takes, and the demo's 5-second delay disappears in live mode.
- Pull to refresh reloads every screen that's open (plus the profile) in parallel. A screen joins in with `.sqReloadable("key") { … }` and gets the gesture with `.sqPullToRefresh()` (see `App/PullToRefresh.swift`).
- [API_CONTRACT.md](API_CONTRACT.md) lists every endpoint with example JSON, the realtime WebSocket events, and how voice input works (Apple's Speech framework, on device).

---

## One-time setup

1. **Install Xcode.** Which version depends on your macOS (Apple menu → About This Mac):
   - **macOS 26.6 or later:** install Xcode 27 from the Mac App Store.
   - **macOS 26.2 – 26.5:** Xcode 27 won't install, so use Xcode 26.6. Sign in at [developer.apple.com/download/all](https://developer.apple.com/download/all/?q=Xcode%2026.6) with your Apple Account and download `Xcode_26.6_Apple_silicon.xip`. Double-click it to expand it, then drag `Xcode.app` into Applications.
2. **Open Xcode once.** Agree to the license, enter your Mac password when it installs extra components, and check **iOS** when it asks which platforms to install.

## Run on your iPhone

Your iPhone needs iOS 17 or later. A free Apple Account (Apple ID) is enough; you don't need the paid developer program.

1. **Sign in to Xcode:** Xcode → Settings → Accounts → **+**, then sign in with your Apple Account.
2. **Choose a signing team:** click the blue **SideQuestz** project icon at the top of the left sidebar, select the **SideQuestz** target, open the **Signing & Capabilities** tab and set **Team** to *Your Name (Personal Team)*.
3. **Connect your phone** with a USB cable. Unlock it and tap **Trust** when it asks about this computer.
   If Xcode shows a **Pair** button, click it. To see your devices, open the run destination menu and choose **Manage Devices…** (Xcode 27) or **Manage Run Destinations…** (Xcode 26).
   No cable? With Xcode 27 and iOS 27 you can pair over Wi-Fi: in Device Hub click **+** → **Pair Nearby Device…**.
4. **Turn on Developer Mode** on the phone: Settings → Privacy & Security → **Developer Mode**, then tap **Restart**. After the restart, tap **Enable** and enter your passcode.
   The switch only appears after you've started pairing the phone with Xcode.
5. **Run:** choose your iPhone as the run destination and press **⌘R**. The first run can take several minutes while Xcode prepares the phone.
   If macOS asks to let `codesign` use your keychain, enter your Mac password and click **Always Allow**.
6. **Trust yourself as a developer (first install only):** if the phone says "Untrusted Developer", go to Settings → General → VPN & Device Management, tap the entry under **Developer App**, tap **Trust** and then open the app again.

### Limits of a free Apple Account

- The app stops opening after 7 days. Run it from Xcode again to renew it.
- At most 3 apps installed this way can be on a phone at once.
- Push notifications, iCloud, Sign in with Apple and TestFlight need the paid Apple Developer Program ($99/year).

## Troubleshooting

| Problem | Fix |
| --- | --- |
| "Signing for SideQuestz requires a development team" | Do step 2 above. |
| "Failed to register bundle identifier" or "…is not available" | Bundle IDs must be unique across all Apple developers. In **Signing & Capabilities**, change **Bundle Identifier** to one only you would use, such as `com.yourname.SideQuestz`. |
| Your phone isn't in the run destination list | Unlock the phone, reconnect the cable, check that you tapped Trust and that Developer Mode is on. |
| Xcode says your iOS version isn't supported | Update Xcode, and macOS if the App Store requires it. |
| "Untrusted Developer" | See step 6 above. |
| The live backend is unreachable from your phone | `127.0.0.1` only works in the Simulator. Use your Mac's LAN IP or a deployed URL in `SQAPIBaseURL`. |
| "Use Strong Password?" pops up while creating an account | That's iOS offering a generated password for the sign-up form. Close it (×) to type your own, or tap "Fill Strong Password". |

## Working as a team

Everyone can run the app in the Simulator without any signing setup. To run it on your own phone, choose your own team in step 2. If Xcode then says the bundle ID is unavailable, change it as described under Troubleshooting. Xcode saves both settings in `SideQuestz.xcodeproj/project.pbxproj`. If signing is the only change in that file, don't commit it; otherwise everyone else's builds switch to your team.
