# Phones and tablets

The same MyGo app runs on iPhones and iPads, and on Android phones and
tablets: windows of web pages show in the system's web view (WKWebView on
iOS, Android's WebView), windows of native UI draw with the GPU (Metal on
iOS, OpenGL ES on Android), and the app's Go code, its bound services and
its events are the same. `mygo dev` runs the app on a simulator, an
emulator or a device as you change it; `mygo build` makes what the App
Store and Google Play take.

```sh
mygo dev -platform ios         # the booted iOS simulator
mygo dev -platform android     # the device or emulator adb reaches
mygo build -platform ios       # a signed .ipa
mygo build -platform android   # an .apk and an .aab
```

## What you need

- **iOS**: a Mac with Xcode. The Go code links with Xcode's clang (cgo,
  which Go needs to link for iOS), and the CLI compiles the app's Swift
  host, its icon and its bundle, and signs it, without an Xcode project.
  Devices and the App Store need an Apple developer team and a
  provisioning profile for the app's identifier, which Xcode makes
  (Settings › Accounts) or developer.apple.com.
- **Android**: Android Studio with the SDK and the NDK (SDK Manager › SDK
  Tools › NDK), on any system. The Go code is a shared library for each
  ABI, linked by the NDK's clang, and Gradle builds the app; the CLI finds
  the SDK in its usual place or `ANDROID_HOME`, the NDK in the SDK or
  `ANDROID_NDK_HOME`, and Android Studio's JDK or `JAVA_HOME`.

Desktop builds still need neither cgo nor these tools.

## Developing

`mygo dev -platform ios` builds the app for the simulator, installs it on
the one booted (or `-device`, a simulator's name or UDID, which it boots)
and launches it, the app's output in the console. Each change of the Go
code rebuilds and relaunches it in a few seconds. A frontend's `devUrl`
works as on the desktop: the simulator reaches the Mac's dev server.

`mygo dev -platform android` does the same with the device or emulator
`adb` reaches (or `-device`, its serial), which reaches the dev server
through `adb reverse`, at the same URL. Without `devUrl`, the app embeds
`frontendDist`, as a device cannot read the computer's disk.

The development app is "<name> Dev" with the identifier
"<identifier>.dev", as on the desktop, so it installs beside the release.

## Building

```sh
mygo build -platform ios             # dist/ios-arm64/<slug>-<version>.ipa, signed
mygo build -platform ios/simulator   # dist/ios-simulator/<name>.app
mygo build -platform android         # dist/android/<slug>-<version>.apk and .aab
mygo build -platform android/arm64   # one ABI: arm64, arm or amd64
```

The `.ipa` is signed with the installed provisioning profile that best
fits the app's identifier and `ios.distribution` (`app-store` by default,
which is also TestFlight's), whose certificate is in the keychain; it
carries the privacy manifest App Store Connect asks for, made from what the
app's code uses, unless the project has its own
`ios/PrivacyInfo.xcprivacy`. Upload it with Transporter or `xcrun altool`.
The CLI refuses a build that names Apple's private methods, which App
Review turns down.

The `.aab` is what Google Play takes, the `.apk` what other stores and
devices install. Release builds are signed with `android.keystore`, its
passwords from `MYGO_ANDROID_KEYSTORE_PASSWORD` and
`MYGO_ANDROID_KEY_PASSWORD`, or else with the debug key, which Google Play
refuses.

The configuration's `ios` and `android` sections set the rest (see
[Configuration](configuration.md)):

```ts
export default defineConfig({
  name: "Todo",
  identifier: "dev.example.todo",
  ios: { team: "ABCDE12345", infoPlist: { NSCameraUsageDescription: "Scan receipts." } },
  android: { permissions: ["CAMERA"], keystore: "upload.jks", keyAlias: "upload" },
});
```

## Tabs and stacks

Phones move through an app with the system's bar of tabs at the bottom
and stacks of screens, each with a bar at the top holding its title and a
back button. `mygo.NewTabs` shows a bar of tabs (UIKit's on iOS, Material's
bottom navigation on Android), and `Window.Push` shows a window over
another, on its tab's stack:

```go
mygo.App.WhenReady(func() {
	list := mygo.NewWindow(mygo.WindowOptions{Title: "Fruits", Content: ui.View(app.list), Hidden: true})
	web := mygo.NewWindow(mygo.WindowOptions{Title: "Web", URL: "/", Hidden: true})
	tabs, _ := mygo.NewTabs(
		mygo.TabItem{Title: "Fruits", Symbol: "leaf", Icon: leafPNG, Window: list},
		mygo.TabItem{Title: "Web", Symbol: "globe", Icon: globePNG, Window: web},
	)
	tabs.SetBadge(1, "3")
	app.open = func(fruit string) {
		list.Push(mygo.NewWindow(mygo.WindowOptions{Title: fruit, Content: ui.View(app.detail(fruit)), Hidden: true}))
	}
})
```

A tab names an SF Symbol for iOS and gives a PNG for Android (and for iOS
without a symbol), drawn as a template in the bar's colors. The back
button, iOS's swipe from the left edge and Android's back pop a pushed
window, which closes it; `Window.Pop` does too. On iOS the bars float
over native UI, whose lists scroll on beneath the tab bar's glass; on
Android the bars take the color of the window's background. On desktops
the windows are windows: `Tabs.Select` shows one, and a pushed window
shows as one. `examples/tabs` shows both, with a page in one of its tabs.

## How apps behave on phones

- **One screen**: the window shown last fills the screen, above those
  shown before; hiding or closing it shows the one under it. Window sizes,
  positions and title bars have no say. `Window.Bounds` is the screen's.
- **Native UI lays out inside the safe area**: the system's bars, the
  display's cutout and, while it shows, the keyboard, while the window's
  background fills the edges. Pages lay out inside the bars, and above the
  keyboard on Android.
- **Touch**: a tap is a click; a finger that moves pans what scrolls under
  it and flings it on as it lifts, pressing nothing it started on; two
  fingers pinch (`InputZoom`).
  `Tester.TouchDown`, `TouchMove` and `TouchUp` test views with a finger.
- **The keyboard** shows while an element of native UI takes text, with
  the system's suggestions and input methods (composition included), and
  a hardware keyboard's keys work as on the desktop.
- **Accessibility**: VoiceOver and TalkBack read native UI's elements and
  act on them.
- **Android's back** pops the window pushed last, goes back in a page,
  closes what Escape closes in native UI, goes back in a `ui.Router`, or
  else leaves the app running in the background.
- **Lifecycle**: the app never quits itself. Going to the background is a
  power event `"suspend"` (`Power.OnSuspend`), and coming back `"resume"`
  and `App.OnActivate`. The system may end an app in the background.
- **Deep links** (`urlSchemes`) open the app with `App.OnOpenURL`.
- **Dialogs**: message boxes are the system's alerts; `Dialog.Open` shows
  its document picker and gives copies of the files chosen, in the app's
  temporary files. `Dialog.Save` is not supported: write into the app's
  directories and share the file (`Shell.OpenPath`).
- **Files**: `WatchFiles` watches the app's directories as on the
  desktop.
- **Secrets** go to the Keychain on iOS, and on Android to the app's
  storage, encrypted with a key of Android's keystore.
- **Notifications** ask for the user's permission the first time.
- What phones lack does nothing: menu bars, trays, the Dock, global
  shortcuts, the updater (stores update apps).

Android's web view loads no custom schemes: the app's frontend
(`mygo://localhost`) and its other schemes are served at
`https://<scheme>.localhost`, which pages never notice but in
`location.href`. Requests of `fetch` and `XMLHttpRequest` to the app's
schemes keep their bodies; a form posted to one arrives without its body
on Android, which does not give it, so post forms with `fetch` or through
bound methods.
