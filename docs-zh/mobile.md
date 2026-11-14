# 手机和平板

同一个 MyGo 应用可以在 iPhone 和 iPad 上运行，也可以在 Android 手机和平板上运行：网页窗口显示在系统的 webview 中（iOS 上是 WKWebView，Android 上是 Android 的 WebView），原生 UI 的窗口用 GPU 绘制（iOS 上是 Metal，Android 上是 OpenGL ES），应用的 Go 代码、绑定的服务和事件都不变。`mygo dev` 在你修改代码时把应用运行在模拟器或设备上；`mygo build` 生成 App Store 和 Google Play 接受的产物。

```sh
mygo doctor                    # what is there, and what is missing
mygo devices                   # what -device takes
mygo dev -platform ios         # the booted iOS simulator
mygo dev -platform android     # the device or emulator adb reaches
mygo build -platform ios       # a signed .ipa
mygo build -platform android   # an .apk and an .aab
mygo publish -platform ios     # the .ipa to App Store Connect
mygo publish -platform android # the .aab to a track of Google Play
```

## 你需要什么

- **iOS**：一台装有 Xcode 的 Mac。Go 代码用 Xcode 的 clang 链接（Go 为 iOS 链接时需要 cgo），CLI 编译应用的 Swift 宿主、图标和应用包，并为它签名，不需要 Xcode 项目。在设备上运行和上架 App Store 需要 Apple 开发者团队，以及与应用标识符匹配的描述文件（provisioning profile），可由 Xcode（设置 › 账户）或 developer.apple.com 生成。
- **Android**：任意系统上装有 SDK 和 NDK 的 Android Studio（SDK Manager › SDK Tools › NDK）。Go 代码为每个 ABI 构建成一个共享库，由 NDK 的 clang 链接，再由 Gradle 构建应用；CLI 在 SDK 的常用位置或 `ANDROID_HOME` 中查找 SDK，在 SDK 中或 `ANDROID_NDK_HOME` 中查找 NDK，并使用 Android Studio 的 JDK 或 `JAVA_HOME`。

桌面构建仍然不需要 cgo，也不需要这些工具。

`mygo doctor` 会检查它们。在 Mac 上，它检查 Xcode 及其命令行工具、iOS SDK、模拟器、签名身份和描述文件；在所有系统上，它检查 Android SDK、adb、NDK、JDK、模拟器和已连接的设备。缺少的项目只是一条说明如何获取的警告，从不算失败：只面向桌面的应用一样都不需要。

## 设备

`mygo devices` 按名称和 ID 列出 `-device` 接受的设备：iOS 模拟器及其运行时、iOS 设备、Android 设备和模拟器，以及尚未运行的模拟器虚拟设备（AVD）。`-json` 以脚本可读的形式输出。

```
NAME           ID                                    PLATFORM    KIND       STATE
iPhone 18 Pro  629AC4ED-EFB6-4090-9143-E4B57B3220DA  iOS 27.0    simulator  booted
Jane's iPhone  00008140-000E55192283801C             iOS 27.0.1  device     connected
Pixel_9        emulator-5554                         Android 17  emulator   booted
Tablet         Tablet                                Android     emulator   shutdown
```

它会询问 `xcrun simctl`、`xcrun devicectl`（Xcode 15 及以上）、`adb` 和 `emulator`，问不到的就不列出。

## 开发

`mygo dev -platform ios` 为模拟器构建应用，安装到已启动的模拟器上（或 `-device` 指定的模拟器，按名称或 UDID，它会启动该模拟器）并运行，应用的输出显示在控制台中。Go 代码每次改动后，几秒内就会重新构建并重新启动应用。前端的 `devUrl` 与桌面上一样可用：模拟器能访问 Mac 上的开发服务器。

`mygo dev -platform android` 对 `adb` 能连接的设备或模拟器做同样的事（或 `-device` 指定的序列号），它通过 `adb reverse` 以同一个 URL 访问开发服务器。没有 `devUrl` 时，应用会嵌入 `frontendDist`，因为设备读不到电脑的磁盘。

Go 代码的改动大约两秒生效：宿主的 Swift 保持已编译状态，iOS 上把新的可执行文件放进已安装的应用，Android 上不经 Gradle 就把新的库传给可调试的应用。宿主、插件、资源或配置的改动则会重新构建并安装整个应用。

Go 无法替换正在运行的程序中的代码，所以每次改动都会重启应用，而应用会回到原来的位置。滚动位置、`ui.Router` 所在的页面，以及控件用 `ui.Local` 保存的内容会自动恢复。一行代码就能恢复应用自己的值，包括未导出的字段，不需要在类型上加标签或方法：

```go
a := &app{tab: 0}
mygo.KeepState(a)
```

系统在后台结束应用后，它也会让应用这样恢复。它保留的是数据：函数、通道、接口和指向其他包类型的指针保持程序创建时的样子，类型被这次改动修改过的字段也保持新建时的值。需要更多控制时，可以用 `App.OnSaveState` 和 `App.RestoredState`。这些在桌面上的 `mygo dev` 下同样可用。

开发版应用名为“<name> Dev”，标识符为“<identifier>.dev”，与桌面上相同，因此可以与正式版并存。

### 在 iPhone 或 iPad 上

`mygo dev -platform ios -device <name or UDID>` 也接受设备，设备需要开启开发者模式（设置 › 隐私与安全性）。应用为设备构建，并用开发描述文件签名：从已安装的描述文件中选出最匹配“<identifier>.dev”的那个，通常是 Xcode 维护的通配符描述文件，其证书在钥匙串中，并且列出了该设备。`ios.provisioningProfile` 指定的描述文件用于 `ios.distribution`，所以只有当它是 `development` 时，才会用来为开发版签名。

`xcrun devicectl` 负责安装和启动，应用的输出显示在控制台中；如果 Xcode 的 `devicectl` 没有 `--console`，应用照样运行，只是看不到输出。设备不在 Mac 的网络栈上：`localhost` 上的 `devUrl` 会改用 Mac 在网络中的地址加载，开发服务器必须监听该地址（`vite --host`）。开发版允许访问本地网络，并为系统的提示说明原因。

通配符描述文件不能带关联域（Associated Domains）：使用这种描述文件时，开发版会省略 `ios.associatedDomains` 并给出提示。

### Android 模拟器

`-device` 也接受 AVD 的名称。未运行的 AVD 会在 `mygo dev` 之外启动，`mygo dev` 等待其系统启动完成；`mygo dev` 结束后它继续运行，因为再次启动很慢。

## 构建

```sh
mygo build -platform ios             # dist/ios-arm64/<slug>-<version>.ipa, signed
mygo build -platform ios/simulator   # dist/ios-simulator/<name>.app
mygo build -platform android         # dist/android/<slug>-<version>.apk and .aab
mygo build -platform android/arm64   # one ABI: arm64, arm or amd64
```

`.ipa` 用已安装的描述文件中最匹配应用标识符和 `ios.distribution`（默认 `app-store`，TestFlight 也用它）的那个签名，其证书在钥匙串中；它带有 App Store Connect 要求的隐私清单，根据应用代码用到的内容生成，除非项目有自己的 `ios/PrivacyInfo.xcprivacy`。如果构建中引用了 Apple 的私有方法，CLI 会拒绝构建，因为 App 审核会拒绝它。

`.aab` 是 Google Play 接受的格式，`.apk` 供其他应用商店和设备安装。正式版用 `android.keystore` 签名，其密码来自 `MYGO_ANDROID_KEYSTORE_PASSWORD` 和 `MYGO_ANDROID_KEY_PASSWORD`；否则用调试密钥签名，而 Google Play 不接受调试密钥。

其余设置在配置的 `ios` 和 `android` 部分中（见[配置](configuration.md)）：

```ts
export default defineConfig({
  name: "Todo",
  identifier: "dev.example.todo",
  ios: { team: "ABCDE12345", infoPlist: { NSCameraUsageDescription: "Scan receipts." } },
  android: { permissions: ["CAMERA"], keystore: "upload.jks", keyAlias: "upload" },
});
```

## 发布

`mygo publish` 上传 `mygo build` 为当前版本生成的产物，或 `-file` 指定的文件。

```sh
mygo build -platform ios && mygo publish -platform ios
mygo build -platform android && mygo publish -platform android -track beta
```

**iOS** 用 `xcrun altool` 把 `.ipa` 上传到 App Store Connect，处理完成后会出现在 TestFlight 中。上传需要团队的 API 密钥（App Store Connect › 用户和访问 › 集成），密钥从不出现在命令行中：

| 变量 | |
|---|---|
| `MYGO_ASC_KEY_ID` | 密钥的 ID |
| `MYGO_ASC_ISSUER_ID` | 签发者的 ID |
| `MYGO_ASC_KEY` | 其 `.p8` 文件的路径 |

**Android** 通过 Google Play 的 Android Publisher API 上传 `.aab`：创建一次编辑，上传应用包，在轨道上创建发布，然后提交；任何一步失败都会删除这次编辑。`MYGO_PLAY_SERVICE_ACCOUNT` 是服务账号 JSON 密钥的路径，也可以是 JSON 本身，便于在 CI 的密钥中使用；该账号需要在 Play 管理中心中有应用的访问权限。`-track` 默认是 `internal`，也可以是 `alpha`、`beta`、`production` 或某个封闭测试轨道的名称。应用的第一次上传只能在 Google Play 管理中心中完成；尚未经过审核的应用只接受 `-status draft` 的发布。

上传的目标包就是应用的 `identifier`。

## 链接、图标和启动画面

```ts
export default defineConfig({
  ios: {
    associatedDomains: ["example.com"],
    launchScreen: { background: "#102030", image: "resources/logo.png" },
    backgroundModes: ["audio", "remote-notification"],
  },
  android: {
    appLinks: ["example.com"],
    adaptiveIcon: { foreground: "resources/icon-foreground.png", background: "#102030" },
    splash: "#102030",
  },
});
```

**链接**：你的域名下的 https URL 会打开应用，并传给 `App.OnOpenURL`。`ios.associatedDomains` 让应用带上 `applinks:example.com` 签名（自带服务的值，如 `webcredentials:example.com`，保持原样）；应用 ID 需要“关联域”能力，主机需要提供 `apple-app-site-association` 文件。`android.appLinks` 为每个主机添加一个经过验证的 intent filter（`*.example.com` 包括其子域名），每个主机需要提供列出该应用及其签名密钥的 `/.well-known/assetlinks.json`。

**Android 的图标**：`android.adaptiveIcon` 是 Android 8.0 及以上的启动器图标，一张 432×432 或更大的 PNG，叠在一种颜色上。启动器会把它裁成中间的 72/108，并可能移动它，所以重要的内容要放在那里。旧系统仍显示 `icon`。

**启动画面**：`ios.launchScreen` 是一种颜色、居中显示在其上的一张图片，或两者兼有，不需要 storyboard：CLI 把它们编译进应用的资源目录，并在 `UILaunchScreen` 中引用。图片是 3 倍屏尺寸的 PNG，所以 600×600 像素的标志占 200×200 点。`android.splash` 是应用启动时系统所显示画面的颜色，应用图标显示在它上面。

**后台模式**：`ios.backgroundModes` 是应用的 `UIBackgroundModes`，加上插件要求的那些。

## 标签页和栈

在手机上，用户通过底部的系统标签栏和一层层的屏幕栈在应用中导航，每层屏幕顶部都有一个显示标题和返回按钮的栏。`mygo.NewTabs` 显示标签栏（iOS 上是 UIKit 的，Android 上是 Material 的底部导航），`Window.Push` 把一个窗口显示在另一个之上，放进它所在标签页的栈中：

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

标签页在 iOS 上指定一个 SF Symbol，在 Android 上（以及 iOS 上没有符号时）提供一张 PNG，按栏的颜色作为模板绘制。返回按钮、iOS 上从左边缘的轻扫和 Android 的返回会弹出推入的窗口，并关闭它；`Window.Pop` 也一样。在 iOS 上，栏浮在原生 UI 之上，列表在标签栏的玻璃下继续滚动；在 Android 上，栏使用窗口背景的颜色。在桌面上，这些窗口就是普通窗口：`Tabs.Select` 显示其中一个，推入的窗口也显示为一个窗口。`examples/tabs` 演示了两者，其中一个标签页是网页。

在 iPad 上，在 `mygo.json` 中设置 `"ios": {"multipleWindows": true}` 后，窗口可以成为系统自己的窗口，与应用的其他窗口并排：

```go
mygo.NewWindow(mygo.WindowOptions{Title: "Draft", OwnScene: true, Content: ui.View(draft)}).Show()
```

在其他地方，它和普通窗口一样：在手机上显示在前一个窗口之上。Android 平板会自行与其他应用分屏。

## 手指

原生 UI 像系统自己的工具包那样处理手指：

- 轻点就是点击。手指能点到它几乎碰到的东西，范围为 14 DIP：滑块的拇指比指尖还窄。
- 移动的手指会锁定到它移动的方向，以及在该方向上滚动的最内层容器：在一个纵向列表中，一行横向照片接收横向的移动，把纵向的移动留给列表。手指抬起时内容继续惯性滑动，手指落下时惯性滑动停止，不会点到任何东西。
- 超过内容的边界后，内容跟随得越来越少，然后弹回，iOS 上弹得远，Android 上弹得近。内容移动时会显示滚动指示器。
- 沿某个方向拖动的元素接收该方向的移动：列表中一个横向的 `Draggable`，即使里面有按钮，也会把纵向的移动留给列表。
- 手指静止按住半秒就是长按：`Element.LongPressed`、上下文菜单、选中文本中的一个词，或拖动一个值，比如 `Reorder` 移动的列表行。设备会给出一下触感反馈。
- 两根手指可以捏合和旋转（`InputEvent` 的 `InputZoom`，以及 `Rotation`）。
- 在 iOS 上，手指从左边缘可以让 `ui.Router` 的页面返回，页面跟随手指移动，就像 UIKit 的栈那样。在 iOS 上轻点状态栏，可滚动的内容会回到顶部。

`Element.PullToRefresh` 让手指把滚动容器拉过顶部：

```go
list := ui.Scroll(c).Fill()
if list.PullToRefresh(&app.refresh) {
	go func() {
		items := fetch()
		win.Update(func() { app.items, app.refresh.Refreshing = items, false })
	}()
}
```

## 文本

键盘通过输入元素的 `TextInputClient` 编辑文本，带有系统的输入建议和输入法。元素会说明它装的是什么内容，由此选择键盘：

```go
ui.TextInput(c, &app.email).
	Keyboard(ui.KeyboardEmail).ReturnKey(ui.ReturnNext).
	Capitalize(ui.CapitalizeNone).Autocorrect(false).Autofill(ui.AutofillEmail)
```

`ReturnNext` 把焦点移到下一个元素；`Password` 是安全输入。键盘弹出时会让正在使用的输入元素保持可见。按住一个词会选中它，并显示系统的操作菜单（`UIEditMenuInteraction` 和 Android 的浮动工具栏）；选区两端的手柄可以拖动，并带有系统的放大镜，外观与各系统一致。轻点插入点也会显示菜单。

## 手机的控件

包 `ui/mobile` 提供手机界面有而桌面界面没有的控件，外观与各系统一致，基于 `ui` 的公开 API。不导入它的应用不会为它付出代价。

| | |
|---|---|
| `Sheet`、`ActionSheet` | 从底部升起的面板，带停靠位置，可以用手指拖走 |
| `Drawer` | 从侧边滑入的面板 |
| `PageView`、`PageDots` | 用手指轻扫切换的页面，在接近时才构建 |
| `SwipeRow` | 用手指轻扫到一边以显示操作的行 |
| `Wheel`、`DatePicker`、`TimePicker` | 可转动的滚轮，选择变化时有触感反馈 |
| `TopBar`、`BottomBar`、`FloatingButton` | 由应用绘制的栏，用于在一个窗口内部路由 |
| `ListRow`、`Section` | 行及行的分组，带按下时的外观 |
| `Ink` | Material 的水波纹 |
| `Switch` | 系统的开关 |
| `Zoomable` | 可以用手指捏合、旋转和拖动的内容，比如照片 |

`mobile.SetLook` 可以不论系统，选择一种设计。`examples/mobile` 演示了所有控件。

来自网络的图片用 `ui/netimage` 代替 `ui.Image`：

```go
netimage.Image(c, user.AvatarURL, netimage.Options{MaxSide: 240}).Size(80, 80).Radius(40)
```

占位符先占住它们的位置，图片加载完成后淡入。只要有空间，它们就留在内存中，并按服务器的缓存头在多次运行之间保存在磁盘上，离线时也能显示。屏幕上的图片先于惯性滑动划过的图片下载，轮到时已经划走的图片根本不会下载。桌面和浏览器也能使用它。

## 设备

| | |
|---|---|
| `mygo.Haptics` | 选择、碰撞或任务结束方式的触感 |
| `mygo.SystemBars` | 系统栏图标的颜色，以及隐藏系统栏 |
| `mygo.SetOrientations` | 屏幕可以如何旋转 |
| `mygo.Share` | 系统的共享面板，共享文本、链接和文件 |
| `mygo.Network` | 是否在线，以及流量是否计费 |
| `mygo.Power.Battery` | 电池电量，以及低电量模式 |
| `mygo.SetBarItems` | 窗口的系统导航栏中的按钮 |
| `Context.SafeArea` | 窗口边缘被什么遮挡，以便在 `Overlay` 中绘制 |

它们在桌面上什么都不做，或者报告 `errors.ErrUnsupported`。

## 官方插件

需要权限或较大依赖的功能是插件，应用只有导入它才会拥有它，其用途说明和权限会加入应用：

| 包（`plugins/...`） | 作用 |
|---|---|
| `maps` | 原生 UI 中带标注的地图：MapKit，以及使用 OpenStreetMap 瓦片的 osmdroid，不需要密钥 |
| `location` | 设备所在的位置，以及持续跟踪位置，通过 Core Location 和 LocationManager |
| `imagepicker` | 系统的照片和视频选择器，不需要权限，以及它的拍照界面 |
| `camera` | 原生 UI 中的实时预览，以及拍照：AVCaptureSession 和 CameraX |
| `videoplayer` | 原生 UI 中的视频：AVPlayer 和 MediaPlayer |
| `biometrics` | 面容 ID、触控 ID、指纹、设备密码 |
| `sensors` | 加速度计、陀螺仪和磁力计 |
| `push` | APNs 和 Firebase 的令牌、消息，以及被轻点的通知（iOS 设备需要描述文件中有推送通知能力，Android 上需要一个 Firebase 项目） |
| `purchases` | App 内购买和订阅：StoreKit 2 和 Google Play 结算 |
| `permissionflow` | 应用能否使用相机、麦克风、照片、通讯录等，请求授权，以及系统的设置 |
| `preferences` | 在多次运行之间保存的小数据，所有平台可用 |
| `sqlite` | 带全文搜索的 64 位 SQLite，编译进应用 |
| `background` | 应用没有打开时、在系统找到时间时运行的 Go 代码：BGTaskScheduler 和 WorkManager |

## 编写插件

手机上的原生代码最好写成[原生模块](native-modules.md)：一个 Go 接口，在 iOS 上用 Swift 实现，在 Android 上用 Kotlin 实现，Go 像调用普通 Go 值一样调用它，带有它的类型、错误、流和回调，`mygo dev` 和 `mygo build` 随应用从源码构建它。同一个模块在 macOS 上可以是 Swift，在其他桌面上可以是 Rust 或 Zig：

```sh
mygo native new -lang swift,kotlin torch
```

```go
//mygo:native
//mygo:mainthread
type Torch interface {
	// Set lights the torch, or puts it out.
	Set(on bool) error
}
```

```kotlin
fun makeTorch(): Torch = CameraTorch()

private class CameraTorch : Torch {
    private val camera = Native.context.getSystemService(CameraManager::class.java)

    override fun set(on: Boolean) {
        val id = camera.cameraIdList.firstOrNull { camera.getCameraCharacteristics(it).get(CameraCharacteristics.FLASH_INFO_AVAILABLE) == true }
            ?: throw NativeUnsupportedException()
        camera.setTorchMode(id, on)
    }
}
```

它的 `Info.plist`、权限和 Gradle 依赖会加入应用（[原生模块](native-modules.md#手机)）。SQLite 插件就是这样一个模块：在 iOS 上是 Swift，在 Android 上是带 C 的 Kotlin，在桌面上是 Zig。

插件也可以带上 Go 通过 JSON 调用的 Swift 和 Kotlin，上面的官方插件就是这样写的：一个 Go 包，旁边是它的 Swift 和 Kotlin，由它的 `mygo-plugin.json` 列出。`mygo build` 在应用导入的包中找到它，把它的代码编译进宿主并注册：

```json
{
  "mobile": {
    "id": "torch",
    "ios": {
      "sources": ["ios/*.swift"],
      "class": "TorchPlugin",
      "frameworks": ["AVFoundation"],
      "infoPlist": {"NSCameraUsageDescription": "Lights the torch."}
    },
    "android": {
      "sources": ["android/*.kt"],
      "class": "TorchPlugin",
      "permissions": ["CAMERA"],
      "dependencies": ["androidx.camera:camera-core:1.6.2"],
      "application": ["<service android:name=\"dev.mygo.TorchService\" />"]
    }
  }
}
```

Go 用 JSON 调用它，并接收它的事件：

```go
func On(ctx context.Context, on bool) error {
	return mygo.Native("torch").Call(ctx, "set", map[string]bool{"on": on}, nil)
}
```

Swift 放在宿主的模块中，Kotlin 放在包 `dev.mygo` 中，在 UI 线程上运行。它们在任意时刻回复一次，`PluginHost.emit` 发送事件（`mygo.Native("torch").On`）：

```swift
final class TorchPlugin: MyGoPlugin {
    func call(_ method: String, _ args: [String: Any], _ reply: @escaping PluginReply) {
        switch method {
        case "set": reply(.success(nil))
        default: reply(.failure(PluginFailure("NoMethod", method)))
        }
    }
}
```

```kotlin
package dev.mygo

class TorchPlugin : MyGoPlugin {
    override fun call(method: String, args: JSONObject, reply: PluginReply) {
        when (method) {
            "set" -> reply.success(null)
            else -> reply.failure(PluginFailure("NoMethod", method))
        }
    }
}
```

`PluginHost` 提供它们需要的宿主功能：在 iOS 上是用来弹出界面的 `presenter`，以及传给应用委托的事件 `applicationEvent`；在 Android 上是 `activity`、`startForResult` 和 `requestPermissions`。错误的代码以 `*mygo.NativeError` 到达 Go。

### C 代码

C 通过原生模块进入手机：模块的 Kotlin 用 CMake 构建 C（在它的 `build.gradle.kts` 中用 `externalNativeBuild`），使用 NDK，并为应用所构建的 ABI 构建；它的 Swift 包可以带 C target，或以二进制 framework 引入一个库。SQLite 插件的模块用 SQLite 的 amalgamation 同时做了这两件事，并用哈希固定版本：`plugins/sqlite/internal/sqlitecore`。

### 原生视图

`PluginHost.registerView` 在插件的初始化方法中注册一种视图，`mygo.NewPluginView` 为显示原生 UI 的窗口创建它，由 `ui.WebView` 放置。它显示在内容之下、内容帧中留出的空洞里，与 webview 一样：在它之后绘制的内容显示在它之上，落在这些内容上的手指仍归内容所有。视图的 `ID` 在有关它的调用中指代它。`plugins/videoplayer` 是一个简短的示例，可用于地图或其他由系统绘制的东西。

## 测试

单元测试用 `ui.Tester` 运行视图，推进它的时钟，并使用 `TouchDown`、`TouchMove`、`TouchUp` 和 `SetInsets`。包 `ui/golden` 把绘制结果与测试旁边的图片比较：

```go
func TestCard(t *testing.T) {
	golden.Match(t, ui.NewTester(card, 320, 200), "testdata/card.png")
}
```

用 `MYGO_UPDATE_GOLDEN=1` 运行即可写入这些图片，要在运行测试的平台上写入：不同平台的文本渲染不同。

`MYGO_FRAME_STATS=1` 记录慢于 8 毫秒的帧，以及耗时所在，在手机上也一样：模拟器用 `SIMCTL_CHILD_MYGO_FRAME_STATS=1`，Android 上用 `adb shell am start ... --es mygo.env MYGO_FRAME_STATS=1`。

端到端测试从另一个进程驱动运行中的应用，在模拟器和设备上都一样，使用[自动化插件](plugins/automation.md#on-phones)：读取屏幕上的元素，对它们操作，发送手指和文本，截取屏幕，并等待画面稳定。

在开发版中用四根手指轻点，可以打开原生 UI 的检查器：它显示在手机底部，包括元素、样式、帧时间和问题，就像在桌面上按 F12 打开的那样。

## 应用在手机上的行为

- **一个屏幕**：最后显示的窗口占满屏幕，位于之前显示的窗口之上；隐藏或关闭它会显示它下面的窗口。窗口的大小、位置和标题栏不起作用。`Window.Bounds` 是屏幕的范围。
- **原生 UI 在安全区域内布局**：避开系统栏、屏幕的挖孔，以及显示时的键盘，而窗口背景铺满边缘。网页在系统栏之内布局，在 Android 上还位于键盘之上。
- **触摸**见上文（[手指](#手指)）；`Tester.TouchDown`、`TouchMove` 和 `TouchUp` 用手指测试视图。
- **键盘**在原生 UI 的元素接收文本时显示，带有系统的输入建议和输入法（包括组字），硬件键盘的按键与桌面上一样可用（[文本](#文本)）。
- **辅助功能**：VoiceOver 和 TalkBack 能朗读原生 UI 的元素并对它们操作。
- **Android 的返回**会弹出最后推入的窗口、在网页中后退、关闭原生 UI 中 Escape 会关闭的东西、在 `ui.Router` 中后退，否则让应用在后台继续运行。系统会提前知道这些，以支持预测性返回：`ui.Router` 的页面跟随手势移动，没有可返回的内容时，系统显示应用离开的动画。
- **生命周期**：应用从不自己退出。进入后台是电源事件 `"suspend"`（`Power.OnSuspend`），回到前台是 `"resume"` 和 `App.OnActivate`。两者之间帧会停止。系统可能在后台结束应用：`App.OnSaveState` 保存恢复所需的内容，下次启动时可以在 `App.RestoredState` 中找到。
- **内存**：系统内存不足时会调用 `App.OnMemoryWarning`。原生 UI 这时会释放渲染器的纹理和文本缓存，下一帧再重新创建。
- **深度链接**（`urlSchemes`）通过 `App.OnOpenURL` 打开应用。
- **对话框**：消息框是系统的警告框；`Dialog.Open` 显示系统的文稿选择器，并在应用的临时文件中提供所选文件的副本。`Dialog.Save` 提供一个用于写入的临时文件；`Dialog.SaveCopy` 让用户选择应用生成的文件存放到哪里。
- **文件**：`WatchFiles` 与桌面上一样监视应用的目录。
- **密钥**在 iOS 上存入钥匙串，在 Android 上存入应用的存储，并用 Android 密钥库中的密钥加密。
- **通知**第一次使用时会请求用户的许可。
- 手机没有的功能什么都不做：菜单栏、托盘、程序坞、全局快捷键、更新器（应用由应用商店更新）。

Android 的 webview 不加载自定义协议：应用的前端（`mygo://localhost`）及其其他协议在 `https://<scheme>.localhost` 上提供，网页除了在 `location.href` 中之外察觉不到。对应用协议发出的 `fetch` 和 `XMLHttpRequest` 请求保留请求体；在 Android 上，提交到应用协议的表单不带请求体到达，因为 Android 不提供它，所以请用 `fetch` 或绑定的方法提交表单。
