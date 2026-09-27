# Front-end (iOS app)

**SideQuests**, the HackGT 13 iPhone app, is built in SwiftUI from [GUI_PLAN.md](GUI_PLAN.md) and the
"SideQuestz Mock UI" design canvas. It's a GUI shell: every screen gets its data through one
API interface, so the backend, AI and database can be plugged in later. See
[API_CONTRACT.md](API_CONTRACT.md) for the backend contract.

By default the app talks to the live SideQuests server (`https://api.sidequestz.tech`). Launch it with `-SQAPIMode mock` to run fully offline on demo data (Atlanta, Friday Sep 25, user Jordan Lee) and try every screen without a server.

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
3. Press **⌘R**. Every cold launch plays the opening animation, then shows the sign-in screen. Sign in with an account on the server, or tap **Use the demo account** (Sandy Byte; the link shows when the build knows the demo password, see [Pointing the app at a server](#pointing-the-app-at-a-server)). In the offline demo (`-SQAPIMode mock`), any valid email with any password signs you in. Loading states last exactly as long as the requests do. Pull down on any main screen to reload everything from the API.

To see the intro again, long-press the logo on the sign-in screen.

### Demo shortcuts (launch arguments)

In Xcode, go to Product → Scheme → Edit Scheme → Run → **Arguments** and add these under "Arguments Passed On Launch":

| Argument | What it does |
| --- | --- |
| `-SQAPIMode mock` | Runs fully offline on the demo backend (Atlanta, Friday Sep 25, user Jordan Lee; any valid email with any password signs in). Without it the app uses the live server from Info.plist |
| `-SQRoute home/calendar` | Opens straight to a screen, signed in as the demo user; needs `-SQAPIMode mock` (in live mode a signed-out launch lands on Login). Also: `login`, `forgot/2`, `setup/3`, `home`, `home/past`, `home/sheet/a3`, `home/rate/x1`, `home/edit/itin-fri` (Edit sidequest), `home/map/itin-fri` (that sidequest on its map), `create/1`…`create/4`, `create/4/more`, `create/4/swap` (the swap sheet for stop 2), `create/4/stop` (the details pane for stop 1), `forum`, `forum/filter`, `forum/friends` (Friends with People for you), `profile/u-mr` (someone's profile over the Forum; `u-jl` is yours), `groups`, `thread/g1/splits`, `thread/dm-maya`, `account/friends`, `account/facebook` (connects the demo Facebook and opens its sheet), `gallery` (the design-system gallery) |
| `-SQSkipIntro YES` | Skips the opening animation, which otherwise plays on every cold launch |
| `-SQDemoPassword …` | The demo account's password for this launch, so Login shows "Use the demo account" (live mode only; wins over the `SQ_DEMO_PASSWORD` build setting) |
| `-SQAPIBaseURL http://127.0.0.1:8080` | Uses another server for this launch, with `-SQWebSocketURL ws://127.0.0.1:8080/ws` for realtime (see [Pointing the app at a server](#pointing-the-app-at-a-server)) |
| `-SQSlowLoadingAfter 0` | Seconds a first load shows its skeleton before the S loader takes over (default 2; `0` shows the S at once, a large number never) |
| `-SQMockLatency 0` | Removes the demo backend's fake network delay (each demo call otherwise takes about as long as a real one, 0.1–0.9 s) |
| `-SQMockFail forum,itineraries` | Makes those demo endpoints fail, to see error states (also `facebook`, `me`, `friends`, `profile`, `tickets`, `splits`, `album`, …) |
| `-SQMockFriends none` | Starts the demo account with no friends or friend requests yet, like a new account (an empty Forum › Friends under People for you) |
| `-SQMockTickets none` | Starts the demo without tickets (Account › Your tickets' empty state). The list itself is `-SQRoute account/tickets`, one ticket `tickets/tkt_5b1f0c9a2e7d4a13`, and `-SQMockFail tickets` its error state |
| `-SQVoiceDemo YES` | Voice buttons return sample transcripts instead of using the mic |

### Pointing the app at a server

`SideQuestz/Info.plist` names the server: `SQAPIMode` (`live`), `SQAPIBaseURL` (`https://api.sidequestz.tech`), `SQWebSocketURL` (`wss://api.sidequestz.tech/ws`) and `SQDemoPassword` (`$(SQ_DEMO_PASSWORD)`, empty unless a build supplies it). A launch argument with the same name overrides each one (`-SQAPIBaseURL …`, `-SQWebSocketURL …`, `-SQDemoPassword …`, `-SQAPIMode mock`).

- **A backend on your Mac** (the local dev loop in [docs/DEPLOY.md](../docs/DEPLOY.md)): `-SQAPIBaseURL http://127.0.0.1:8080 -SQWebSocketURL ws://127.0.0.1:8080/ws -SQDemoPassword demo`. `127.0.0.1` only works in the Simulator; on a phone use the Mac's LAN IP. Plain `http://` is allowed for local networks only (`NSAllowsLocalNetworking`).
- **The demo account** (Sandy Byte, `demo@sidequestz.tech`) is one tap away on Login, "Use the demo account", whenever the app knows the password. Give it to the build (`xcodebuild … SQ_DEMO_PASSWORD='…'`, or the `SQ_DEMO_PASSWORD` build setting of the SideQuestz target in Xcode) or to a launch (`-SQDemoPassword …`, which wins). The setting is empty in the repo, so no password is ever committed, and the offline demo never shows the link.
- **Demo accounts run on the server's demo date.** When `GET /me` carries `demo_date`, the app's clock moves to that day at the real time of day (New York time, like the server), so "Today", the calendar and Create's default date match the demo's events. Every other account uses the real date.
- What the server has to implement is in [API_CONTRACT.md](API_CONTRACT.md).

### Tests

Press **⌘U** to run both suites (about 3 minutes, most of it the UI tests).

- **Unit tests** (`SideQuestzTests`, a few seconds) check that the equal-split preview always adds up to the total, to the cent; that reordering stops re-times the route and flags lateness, matching the prototype's numbers; the validation copy; time formatting; the demo backend's forum filters, sidequest edits and stop alternatives (same kind, never already in the plan); must-see picks (the catalog search's rules, `must_include` on the plan request, why a batch can come back empty, and the demo planner putting every pick in every option); how the app reads Facebook's sign-in result and merges suggested likes (never replacing one you picked in Setup); how the demo password is read (a launch argument beats Info.plist, blank means none) and that a `-SQRoute` deep link never skips sign-in in live mode; where a sidequest stands at a given time and place (the progress strip's stages, how full each stop is, "You're here" within 150 m and "far away" past 50 km), how distances and countdowns read, the order of the map's route, and each sidequest keeping its own Timeline | Map; and that every model round-trips through the API's JSON format.
- **UI tests** (`SideQuestzUITests`) run fifteen flows on the demo backend, plus one launch check against the live configuration:
  - create an account through all five Setup steps;
  - reset a password and sign in with it;
  - plan and start a sidequest;
  - search for must-see spots on Vibe, pick an event and a place, and find both in every option ("Your pick");
  - rename a sidequest, and delete one;
  - drag a stop to re-time the route;
  - swap a stop for something similar, remove one and undo;
  - press and hold on the calendar to plan a window;
  - "Plan together" into a DM;
  - add a $40 expense split three ways;
  - connect Facebook in Setup and see step 3 filled in;
  - review Facebook's suggestions in Account and save them;
  - switch a sidequest to its map, open a stop from the map, and swipe to the next sidequest, which keeps its timeline;
  - open a sidequest's map from its deep link;
  - in live mode with a demo password, the app starts on Login and shows "Use the demo account" (nothing is tapped, so no request leaves the simulator).

  Tab bar buttons have the accessibility identifiers `tab.home`, `tab.forum`, `tab.plan`, `tab.groups` and `tab.account`.

To run one suite from Terminal, pass `-only-testing:SideQuestzTests` (or `SideQuestzUITests`) to
`xcodebuild test -project SideQuestz.xcodeproj -scheme SideQuestz -destination 'platform=iOS Simulator,name=iPhone 17e'`.

### Sidequest maps and your location

Home › Sidequests shows each sidequest's progress (a bar per stop, and what's on now or next) above its timeline. The pill next to the page dots switches that sidequest to a map of its route and stops; tap it or swipe across it, and each sidequest remembers its own choice. The progress line and the map use your location while Home is on screen, but never ask for it on their own: iOS asks when you tap **Show my location** on a map (or pick Current location in Create). The offline demo places you at Tech Square.

### Facebook

People can connect Facebook in Setup (step 2, "Fill in your likes") or later in Account › Connected. The import
suggests ratings for "What do you enjoy?" from the Pages they like. The demo backend connects instantly, with no
Facebook page, and returns sample suggestions (48 Pages, two friends on SideQuests). In live mode your server
runs Facebook Login and the Graph API. The steps are in
[API_CONTRACT.md › Backend work: Facebook connector](API_CONTRACT.md#backend-work-facebook-connector).
There's no Facebook SDK in the app.

## Hooking up the backend

- The contract is [`SideQuestz/Services/APIClient.swift`](SideQuestz/Services/APIClient.swift). The REST wrappers that call your server are in [`LiveAPIClient.swift`](SideQuestz/Services/LiveAPIClient.swift).
- Plan generation, ranking, transit re-timing, split math, age filtering and the taste profile belong to the server. The app shows whatever the server returns.
- Loading animations follow the real requests: skeletons and loaders show for exactly as long as your server takes (a first load that runs past 2 s cross-fades its skeleton to the S loader; `-SQSlowLoadingAfter` tunes that).
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
| Login has no "Use the demo account" link | The app doesn't know the demo password: pass `SQ_DEMO_PASSWORD='…'` to `xcodebuild` (or set the build setting) or `-SQDemoPassword …` at launch. The offline demo (`-SQAPIMode mock`) never shows it; any login works there. |
| "Use Strong Password?" pops up while creating an account | That's iOS offering a generated password for the sign-up form. Close it (×) to type your own, or tap "Fill Strong Password". |

## Working as a team

Everyone can run the app in the Simulator without any signing setup. To run it on your own phone, choose your own team in step 2. If Xcode then says the bundle ID is unavailable, change it as described under Troubleshooting. Xcode saves both settings in `SideQuestz.xcodeproj/project.pbxproj`. If signing is the only change in that file, don't commit it; otherwise everyone else's builds switch to your team.
