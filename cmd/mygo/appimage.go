package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	fetch "github.com/egoist/mygo/internal/download"
	"github.com/egoist/mygo/internal/squashfs"
)

// An AppImage is one executable file holding the app: AppImage's runtime,
// an ELF program, followed by a SquashFS image of the app's directory,
// which the runtime mounts with FUSE and runs AppRun from. mygo build
// writes the image in pure Go (internal/squashfs) and downloads the
// runtime, pinned by its SHA-256, so any system builds it. With updates,
// the AppImage is signed and named in the manifest, and an app running as
// one replaces the file with the new version's (updater.go).

// appImageArch are the architectures of AppImage's runtimes, by GOARCH.
var appImageArch = map[string]string{"amd64": "x86_64", "arm64": "aarch64"}

// appImageRuntime is the runtime mygo build puts at the start of an
// AppImage: the static type 2 runtime of AppImage/type2-runtime, which
// reads SquashFS compressed with zstd, by architecture.
var appImageRuntime = struct {
	Version string
	SHA256  map[string]string
}{
	Version: "20251108",
	SHA256: map[string]string{
		"x86_64":  "2fca8b443c92510f1483a883f60061ad09b46b978b2631c807cd873a47ec260d",
		"aarch64": "00cbdfcf917cc6c0ff6d3347d59e0ca1f7f45a6df1a428a0d6d8a78664d87444",
	},
}

// appImageFileName is "<slug>-<version>-linux-<arch>.AppImage", as
// "my-app-0.1.0-linux-x86_64.AppImage" or "my-app-0.1.0-linux-arm64.AppImage",
// named after the platform as the other files of a release are (#1900).
func appImageFileName(c *Config, goarch string) string {
	arch := goarch
	if goarch == "amd64" {
		arch = "x86_64"
	}
	return c.slug() + "-" + c.Version + "-linux-" + arch + ".AppImage"
}

// appImageRun is the AppDir's AppRun, which the runtime runs: it runs the
// app by its own path, so that its argv[0], and with it the WM_CLASS of
// its windows, is the executable's name, which the desktop entry's
// StartupWMClass names, as a link would not.
const appImageRun = `#!/bin/sh
exec "$(dirname "$(readlink -f "$0")")/%s" "$@"
`

// writeAppImage writes the app in stage, the files of app, as an AppImage
// for goarch, and returns its path. The image holds the app's directory as
// the archive does, the desktop entry and icon at its root as the AppDir
// has them, AppRun, .DirIcon, and the icons in the hicolor theme for the
// tools that add AppImages to the applications menu.
func writeAppImage(c *Config, stage, name, goarch string, app []string) (string, error) {
	arch := appImageArch[goarch]
	runtime, err := appImageRuntimeFile(arch)
	if err != nil {
		return "", err
	}
	rt, err := os.ReadFile(runtime)
	if err != nil {
		return "", err
	}
	out := filepath.Join(stage, appImageFileName(c, goarch))
	f, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o755)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(rt); err != nil {
		return "", err
	}
	img, err := squashfs.NewWriter(f, time.Now())
	if err != nil {
		return "", err
	}
	for _, entry := range app {
		if err := addTree(img, stage, entry); err != nil {
			return "", err
		}
	}
	if err := img.Create("AppRun", 0o755, strings.NewReader(fmt.Sprintf(appImageRun, name))); err != nil {
		return "", err
	}
	icons, err := linuxIcons(c)
	if err != nil {
		return "", err
	}
	if len(icons) > 0 {
		if err := img.Symlink(name+".png", ".DirIcon"); err != nil {
			return "", err
		}
	}
	for _, icon := range icons {
		if err := img.Create(icon.dir()+"/"+name+".png", 0o644, bytes.NewReader(icon.png)); err != nil {
			return "", err
		}
	}
	if err := img.Close(); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	// The mode OpenFile gave it is cut by the umask.
	return out, os.Chmod(out, 0o755)
}

// addTree adds the entry of stage at rel, and what is under it, to img.
func addTree(img *squashfs.Writer, stage, rel string) error {
	return filepath.WalkDir(filepath.Join(stage, rel), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		r, _ := filepath.Rel(stage, p)
		name := filepath.ToSlash(r)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return img.Mkdir(name, 0o755)
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return img.Symlink(filepath.ToSlash(link), name)
		}
		mode := os.FileMode(0o644)
		if info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		src, err := os.Open(p)
		if err != nil {
			return err
		}
		defer src.Close()
		return img.Create(name, mode, src)
	})
}

// appImageRuntimeFile returns the runtime of arch, which it downloads into
// the user's cache directory the first time; tests replace it.
var appImageRuntimeFile = downloadAppImageRuntime

func downloadAppImageRuntime(arch string) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("no cache directory to download the AppImage runtime into: %w", err)
	}
	v := appImageRuntime.Version
	path := filepath.Join(cache, "mygo", "appimage-runtime-"+v, "runtime-"+arch)
	sum := appImageRuntime.SHA256[arch]
	if fetch.Check(path, sum) {
		return path, nil
	}
	url := "https://github.com/AppImage/type2-runtime/releases/download/" + v + "/runtime-" + arch
	t := con.start("Downloading the AppImage runtime " + v + " for " + arch)
	err = fetch.Cached(context.Background(), downloadFile(t, url, sum), path)
	if t.end(err, "Downloaded the AppImage runtime "+v+" for "+arch) != nil {
		return "", fmt.Errorf("downloading the AppImage runtime (or leave appimage out of linux.formats):\n%w", err)
	}
	return path, nil
}
