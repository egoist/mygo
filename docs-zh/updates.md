# 自动更新

用 MyGo 构建的应用会从已签名的发布版本更新自己，这些发布版本放在 GitHub 或任何 HTTPS 服务器上。`mygo build` 用你的密钥为每个平台的构建签名；应用中的 `mygo.Updater` 下载更新的版本，用编译进应用的公钥校验签名，然后用它替换应用。

## 设置

创建用于签署更新的密钥对：

```sh
mygo keygen
```

它把私钥 `mygo-update.key` 和公钥 `mygo-update.pub` 写到配置目录中的 `mygo/update-keys`，并打印出位置（`-o` 选择另一个目录）。把私钥放在仓库之外，存进密码管理器和 CI 的机密中（见 [GitHub Actions](github-actions.md)）：已安装的应用只接受用它签名的更新，所以丢失它会让这些应用无法更新，而任何持有它的人都能向你的用户发布代码。

把公钥和发布位置加入配置，这里写在 mygo.config.ts 中（在 [mygo.json](configuration.md#mygojson) 中则是同样字段的 JSON 写法）：

```ts
export default defineConfig({
  version: "1.2.0",
  updates: {
    publicKey: "…the contents of mygo-update.pub…",
    github: "you/my-app",
  },
});
```

`github` 是一个公开仓库，更新放在它的发布版本里，标签为 `v1.2.0` 等。应用读取其最新发布版本的清单。

一个仓库里也可以有其他发布版本，例如某个命令行工具的发布版本，而它一直是该仓库的最新发布版本。给应用的发布版本设置自己的 `tagPrefix`，例如用 `desktop-v` 对应 `desktop-v1.2.0` 这样的标签，应用、`install.sh` 和增量更新就会改为通过 GitHub API 查找带该前缀、既不是草稿也不是预发布的最新发布版本：每次检查一个请求，在 API 对每个地址每小时 60 个请求的限额之内。有 `GH_TOKEN` 或 `GITHUB_TOKEN` 时，`mygo build` 会用它们。

对于你自己的服务器或 [S3 存储桶](#发布到-s3)，改为设置 `url`，即一个目录的 HTTPS URL：

```ts
export default defineConfig({
  updates: {
    publicKey: "…",
    url: "https://downloads.example.com/my-app",
  },
});
```

## 构建与发布

把私钥交给 `mygo build`：可以放在 `MYGO_UPDATER_PRIVATE_KEY` 环境变量中，也可以在配置里用 `updates.privateKey` 指定一个文件（一个路径，可以以 `~/` 开头）：

```sh
MYGO_UPDATER_PRIVATE_KEY="$(cat path/to/mygo-update.key)" bun run build        # a web frontend
MYGO_UPDATER_PRIVATE_KEY="$(cat path/to/mygo-update.key)" go tool mygo build   # native UI
```

在每个平台的安装程序旁边，它还会写出：

- `my-app-1.2.0-darwin-arm64.tar.gz`，安装后的应用，以应用的 [`slug`](configuration.md#应用) 命名，
- 从之前的版本生成的[增量更新](#增量更新)，例如 `my-app-1.1.0-to-1.2.0-darwin-arm64.delta`，以及
- `update-darwin-arm64.json`，清单：版本、发布说明、日期，以及归档和每个增量更新的 URL、大小与签名。

没有密钥时，`mygo build` 会构建应用，但不写更新文件；Linux 归档例外，因为 [`install.sh`](distribution.md#安装脚本) 要从它安装。`mygo build -upload` 需要密钥：它会在构建之前停下，因为已安装的应用无法更新到没有清单的发布版本。

发布说明是 `CHANGELOG.md` 中该版本的章节（`updates.changelog` 可以指定另一个文件），位于 `## 1.2.0` 标题下，或如 [Keep a Changelog](https://keepachangelog.com) 那样的 `## [1.2.0] - 2026-09-19`。文件存在时，它必须包含该章节。

把文件发布到 `updates` 指向的位置：

- **GitHub**：`mygo build -upload` 把它们连同安装程序上传到该版本的草稿发布版本。把它正式发布后，它就会成为应用所检查的最新发布版本：它们从 `https://github.com/you/my-app/releases/latest/download/` 读取清单。如果应用有自己的 `tagPrefix`，这个草稿不应当成为最新发布版本，最新发布版本仍应是其他发布版本：用 `gh release edit desktop-v1.2.0 --draft=false --latest=false` 发布它，或取消勾选“设为最新发布版”，应用会按标签找到它。
- **S3**：`mygo build -upload` 把它们连同安装程序上传到 `updates.s3` 的存储桶；见[下文](#发布到-s3)。
- **你自己的服务器**：把归档、增量更新和清单上传到 `url` 目录，连同 `install.sh`、`install.ps1` 和 `SHA256SUMS`（每个平台各有一行），并把较早版本的文件留在那里。最后上传清单，这样应用永远不会看到文件缺失的清单。

每个平台，例如 `darwin-arm64`、`darwin-universal` 或 `windows-amd64`，都有自己的清单，而每次构建只查看自己平台的清单。

### 预发布

GitHub 标记为预发布的发布版本，例如 `1.3.0-beta.1`，永远不会成为最新发布版本，所以已安装的应用会跳过它。带 beta 的应用通过 `mygo.Updater.SetPrereleases(true)` 让选择 beta 的用户看到预发布：它们的检查随后通过 GitHub API 读取版本最高、且不是草稿的发布版本，因此会得到下一个预发布，以及它之后的发布版本。版本按语义化版本规则比较，`1.3.0-beta.2` 在 `1.3.0-beta.10` 之前，`1.3.0-beta.10` 在 `1.3.0` 之前。关闭它不会安装更旧的版本：一旦某个发布版本比应用正在运行的预发布版本更新，应用就会再次更新。这个选择只在应用运行期间有效；应用要自己保存它，例如保存在设置中，并在启动时调用 `SetPrereleases`。

```go
mygo.Updater.SetPrereleases(settings.Beta)
```

发布预发布版本时把它标为预发布，这样它不会成为每个应用都会检查的最新发布版本：`gh release edit v1.3.0-beta.1 --draft=false --prerelease`，或勾选“设为预发布版”。检查会从最新的发布版本往下读，直到找到不是预发布的最新一个，通常只需一个请求，在 API 对每个地址每小时 60 个请求的限额之内，无论有没有 `tagPrefix`。增量更新以已发布的版本为起点，所以运行预发布版本的应用会完整下载下一个版本。从 `url` 更新的应用无论哪种方式都读取服务器上的清单。

### 发布到 S3

如果 Amazon S3 的存储桶，或 Cloudflare R2 等兼容服务的存储桶，直接或通过 CDN 为 `url` 提供内容，就在 `updates.s3` 中写明它：

```ts
export default defineConfig({
  updates: {
    publicKey: "…",
    url: "https://downloads.example.com/my-app",
    s3: {
      bucket: "downloads",
      prefix: "my-app", // the directory in the bucket that url serves
      endpoint: "https://<account>.r2.cloudflarestorage.com", // not for Amazon S3
    },
  },
});
```

`mygo build -upload` 随后把安装程序、归档、增量更新和安装脚本上传到它，最后上传清单，这样就发布了每个平台的更新。它不需要其他工具：凭据来自 `AWS_ACCESS_KEY_ID`、`AWS_SECRET_ACCESS_KEY`，临时凭据还来自 `AWS_SESSION_TOKEN`；区域来自 `s3.region`，否则来自 `AWS_REGION`（或 `AWS_DEFAULT_REGION`），否则是 R2 接受的 `us-east-1`。清单、安装脚本和 `SHA256SUMS` 的名称在各个版本之间保持不变，它们以 `Cache-Control: no-cache` 上传，好让 CDN 重新获取。

在 Amazon S3 上，存储桶名称放在请求的主机名中；使用 `endpoint` 时，则放在请求的路径中。对于只接受在主机名中指定存储桶的服务，把 `pathStyle` 设为 `false`；设为 `true` 则强制使用路径。

## 增量更新

和 Sparkle 一样，应用只下载自当前版本以来发生变化的内容，通常只是整个应用的一小部分：Go 可执行文件的新版本大多只是挪动代码的位置，二进制补丁用很小的空间就能描述这些变化。

`mygo build` 为更新签名时，会读取每个已发布平台的清单，也就是应用所检查的那份，并下载最近 3 个版本的归档：已发布的那个，以及其清单列出的更早版本（`updates.deltas` 改变数量，`0` 表示不生成）。它检查这些归档是否用你的密钥签名，并为每个版本生成到新版本的增量更新：未更改的文件从已安装的应用复制，更改的文件打补丁（[bsdiff](https://www.daemonology.net/bsdiff/)），其余文件则包含在内。附加到 macOS 应用的公证票据（`Contents/CodeResources`）总是完整包含在内：归档中的应用有它，而从磁盘映像复制出来的应用没有。清单列出增量更新，以及供下次构建使用的更早版本的归档。不小于归档的增量更新会被省略。因此，构建时需要下载已发布的更新；如果无法下载，例如第一个版本，构建不会生成增量更新，并说明原因。

如果最新的 GitHub 发布版本没有正在构建的平台的清单，构建会按配置的标签前缀在更早的发布版本中查找该平台的清单，跳过草稿和预发布。这样，在各平台分别发布时，增量更新及其历史仍能保留。已安装的应用仍然检查配置的更新源。

如果应用的版本有对应的增量更新，它会下载这个增量更新，校验签名，并在自己旁边以自身为基础生成新版本，同时检查每个文件的大小和 SHA-256 是否与增量更新给出的值相符，使结果与发布的版本完全一致，包括代码签名。任何一步失败时，例如应用自安装以来被更改，它就改为下载归档。

## 更新窗口

[updater 插件](plugins/updater.md)为应用提供 Mac 用户熟悉的 Sparkle 式更新窗口，而且每个平台都有：它在后台检查是否有更新，显示新版本的发布说明，并提供安装、跳过或稍后提醒的选项。

```go
import "github.com/egoist/mygo/plugins/updater"

mygo.Use(updater.Plugin)
```

它的页面会介绍“检查更新…”菜单项、它的选项、它保存的偏好设置、它支持的语言，以及 `native.Plugin`——为不显示网页的应用提供相同窗口的原生 UI 版本。

## 你自己的更新 UI

插件正是构建在 `mygo.Updater` 之上。如果要做自己的界面，例如应用页面里的横幅，就用它：

```go
// UpdateProgress tells pages how far the download got.
var UpdateProgress = mygo.NewEvent[float64]("update-progress")

func checkForUpdates(ctx context.Context) {
	if !mygo.Updater.Enabled() {
		return
	}
	up, err := mygo.Updater.Check(ctx)
	if err != nil || up == nil {
		return // offline, or up to date
	}
	res, _ := mygo.Dialog.Message(mygo.MessageOptions{
		Message: "Version " + up.Version + " is available.",
		Detail:  up.Notes, // Markdown
		Buttons: []string{"Install and Restart", "Later"},
	})
	if res.Button != 0 {
		return
	}
	err = up.Install(ctx, func(downloaded, total int64) {
		UpdateProgress.Broadcast(float64(downloaded) / float64(total))
	})
	if err == nil {
		mygo.App.Relaunch()
	}
}
```

- `Check` 返回较新的版本，包含 `Version`、`Notes` 和 `Date`；应用已是最新时返回 nil。
- `Install` 下载归档，或运行中版本的增量更新，校验签名，并替换应用。进度回调先收到增量更新的字节；增量更新失败时，则从头改用归档的字节。运行中的应用不受影响：新版本在 `App.Relaunch` 之后运行，或在下次启动时运行。在 macOS 上，新应用包会在应用退出后替换旧应用包，这样 Dock 会保留应用的图标。在 Linux 和 Windows 上，它替换应用文件夹中的文件，并移除运行中版本安装过、新版本不再有的文件，要移除哪些文件由文件夹中的 `.mygo-files` 列出。任何版本都未安装的文件会保留，例如便携副本在它旁边保存的数据；应用运行所用的可执行文件也保留。在 MyGo 写出该列表之前构建的应用没有这个列表，所以它的第一次更新不会移除任何东西。
- `Enabled` 报告应用能否更新自己：它构建时是否带有更新，以及能否写入安装位置。开发构建不能，在开发构建中，`Check` 会返回 `mygo.ErrUpdatesDisabled`。
- `Check` 和 `Install` 会走系统设置中的代理；设置了 `HTTPS_PROXY` 时，则走环境变量指定的代理（见[代理](native.md#代理)）。

## 你自己的更新服务

`mygo.Updater` 读取 mygo.config.ts 中 `updates` 的更新源。如果应用有自己的更新服务、需要用户凭据的端点，或用户在运行时选择的通道，就改用 `mygo.Updater.SetSource` 给更新器指定一个来源。来源返回平台的清单和清单所指的文件；更新器校验它们的方式，和校验更新源的文件一样，所以没有用 `updates.publicKey` 的密钥签名的归档或增量更新会被拒绝，应用保持原样。来源提供的就是 `mygo build` 写出的内容，所以应用仍然要用 `updates` 构建：公钥，以及文件发布到的 `github` 或 `url`。

例如，文件放在一台只向已登录用户提供这些文件的服务器上：把 `updates.url` 设为它的目录，让清单指向这些文件，并在每个请求中带上用户的令牌：

```go
// accountUpdates downloads updates with the user's token.
type accountUpdates struct{ token string }

func (s accountUpdates) Manifest(ctx context.Context, prereleases bool) (io.ReadCloser, error) {
	// The manifest of the platform the app was built for; a universal
	// macOS build reads update-darwin-universal.json.
	return s.Open(ctx, "https://downloads.example.com/my-app/update-"+runtime.GOOS+"-"+runtime.GOARCH+".json")
}

func (s accountUpdates) Open(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp.Body, nil
}

// Once the user has signed in:
mygo.Updater.SetSource(accountUpdates{token: session.Token})
```

之后 `Check` 从来源读取清单，`Install` 通过它的 `Open` 下载归档或增量更新；`prereleases` 是 `SetPrereleases` 设置的值，用于带 beta 通道的来源。URL 来自清单，所以从 `mygo build` 的清单生成自己清单的服务，可以用自己的 ID 命名文件，只要大小和签名保持不变。`SetSource(nil)` 会回到原来的更新源。

## 应用能在哪里更新

应用会替换自己，所以它必须能写入自己的安装位置：

- macOS：应用包，例如管理员安装时在 `/Applications` 中，或在用户的 `~/Applications` 中。
- Windows：[安装程序](distribution.md#安装程序)的当前用户安装，位于 `%LOCALAPPDATA%\Programs`。
- Linux：用户自己的目录，例如 `~/.local/my-app.app`，[`install.sh`](distribution.md#安装脚本) 会把应用安装到这里；也可以是解压后的构建目录。

由包管理器安装的应用，例如 `/opt` 中的 Debian 包，由包管理器更新：对它们，`Enabled` 为 false。
