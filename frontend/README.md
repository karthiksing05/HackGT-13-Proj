# Front-end (iOS app)

A SwiftUI iPhone app. Right now it is a single "Hello, world!" screen.

## Project layout

```
frontend/
├── HelloWorld.xcodeproj     ← open this in Xcode
└── HelloWorld/              ← all app code lives here
    ├── HelloWorldApp.swift  ← app entry point
    ├── ContentView.swift    ← the first screen
    └── Assets.xcassets      ← app icon, colors, images
```

Any file you add inside `HelloWorld/` becomes part of the app automatically. You don't have to register it anywhere.

## One-time setup

1. **Install Xcode.** Which version depends on your macOS (Apple menu → About This Mac):
   - **macOS 26.6 or later:** install Xcode 27 from the Mac App Store.
   - **macOS 26.2 – 26.5:** Xcode 27 won't install, so use Xcode 26.6. Sign in at [developer.apple.com/download/all](https://developer.apple.com/download/all/?q=Xcode%2026.6) with your Apple Account and download `Xcode_26.6_Apple_silicon.xip`. Double-click it to expand it, then drag `Xcode.app` into Applications.
2. **Open Xcode once.** Agree to the license, enter your Mac password when it installs extra components, and check **iOS** when it asks which platforms to install.

## Run in the Simulator (no Apple account needed)

1. Open `frontend/HelloWorld.xcodeproj`.
2. In the toolbar at the top of the window, choose any iPhone simulator as the run destination.
3. Press **⌘R**. A simulated iPhone opens and shows "Hello, world!".

## Run on your iPhone

Your iPhone needs iOS 17 or later. A free Apple Account (Apple ID) is enough; you don't need the paid developer program.

1. **Sign in to Xcode:** Xcode → Settings → Accounts → **+**, then sign in with your Apple Account.
2. **Choose a signing team:** click the blue **HelloWorld** project icon at the top of the left sidebar, select the **HelloWorld** target, open the **Signing & Capabilities** tab and set **Team** to *Your Name (Personal Team)*.
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
| "Signing for HelloWorld requires a development team" | Do step 2 above. |
| "Failed to register bundle identifier" or "…is not available" | Bundle IDs must be unique across all Apple developers. In **Signing & Capabilities**, change **Bundle Identifier** to one only you would use, such as `com.yourname.HelloWorld`. |
| Your phone isn't in the run destination list | Unlock the phone, reconnect the cable, check that you tapped Trust and that Developer Mode is on. |
| Xcode says your iOS version isn't supported | Update Xcode, and macOS if the App Store requires it. |
| "Untrusted Developer" | See step 6 above. |

## Working as a team

Everyone can run the app in the Simulator without any signing setup. To run it on your own phone, choose your own team in step 2. If Xcode then says the bundle ID is unavailable, change it as described under Troubleshooting. Xcode saves both settings in `HelloWorld.xcodeproj/project.pbxproj`. If signing is the only change in that file, don't commit it; otherwise everyone else's builds switch to your team.
