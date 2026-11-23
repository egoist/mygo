package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runRemoteBuild syncs the project to a remote SSH host, runs `mygo build`
// there with forwarded flags, and pulls the built artifacts back locally.
func runRemoteBuild(host string, rawArgs, posArgs []string, outFlag string) error {
	dir := dirArg(posArgs)
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve project dir: %w", err)
	}
	c, err := loadConfig(absDir)
	if err != nil {
		return err
	}
	localOut := c.Out
	if outFlag != "" {
		localOut = outFlag
	}
	if localOut == "" {
		localOut = "dist"
	}

	moduleRoot := findModuleRoot(absDir)
	relDir, err := filepath.Rel(moduleRoot, absDir)
	if err != nil {
		return fmt.Errorf("resolve relative dir: %w", err)
	}
	remoteRoot := remoteWorkDir(moduleRoot)
	remoteTarget := remoteRoot
	if relDir != "." {
		remoteTarget = remoteRoot + "/" + filepath.ToSlash(relDir)
	}
	forwarded := stripRemoteArgs(rawArgs, posArgs)

	con.startBanner("build")
	t := con.start("Syncing sources to " + host)
	if err := syncToRemote(host, moduleRoot, remoteRoot, localOut); err != nil {
		return t.end(err, "")
	}
	_ = t.end(nil, "Synced sources to "+host)

	tBuild := con.start("Building on " + host)
	if err := execRemoteBuild(host, remoteTarget, forwarded); err != nil {
		return tBuild.end(err, "")
	}
	_ = tBuild.end(nil, "Built on "+host)

	tPull := con.start("Fetching artifacts from " + host)
	localOutDir := localOut
	if !filepath.IsAbs(localOutDir) {
		localOutDir = filepath.Join(absDir, localOut)
	}
	if err := pullFromRemote(host, remoteTarget, localOut, localOutDir); err != nil {
		return tPull.end(err, "")
	}
	_ = tPull.end(nil, "Fetched artifacts to "+localOutDir)
	return nil
}

func findModuleRoot(dir string) string {
	cur := dir
	for {
		if _, err := os.Stat(filepath.Join(cur, "go.mod")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return dir
		}
		cur = parent
	}
}

func remoteWorkDir(absDir string) string {
	sum := sha256.Sum256([]byte(absDir))
	short := hex.EncodeToString(sum[:6])
	base := filepath.Base(absDir)
	if base == "" || base == "." || base == "/" {
		base = "app"
	}
	return fmt.Sprintf("~/.mygo-remote/%s-%s", base, short)
}

// stripRemoteArgs removes -remote / --remote flags and positional dir args
// from rawArgs so the remote host runs `mygo build <flags> .` cleanly.
func stripRemoteArgs(rawArgs, posArgs []string) []string {
	posSet := make(map[string]bool, len(posArgs))
	for _, p := range posArgs {
		posSet[p] = true
	}
	var out []string
	skipNext := false
	for _, a := range rawArgs {
		if skipNext {
			skipNext = false
			continue
		}
		if a == "-remote" || a == "--remote" {
			skipNext = true
			continue
		}
		if strings.HasPrefix(a, "-remote=") || strings.HasPrefix(a, "--remote=") {
			continue
		}
		if posSet[a] {
			continue
		}
		out = append(out, a)
	}
	return out
}

func syncToRemote(host, localDir, remoteDir, outDir string) error {
	mkdirCmd := exec.Command("ssh", host, "mkdir -p "+remoteDir)
	if out, err := mkdirCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ssh mkdir failed: %w\n%s", err, strings.TrimSpace(string(out)))
	}

	args := []string{
		"-az", "--delete",
		"--exclude", ".git",
		"--exclude", "node_modules",
		"--exclude", ".mygo",
		"--exclude", "build",
		"--exclude", "dist",
	}
	if outDir != "" && outDir != "dist" && !filepath.IsAbs(outDir) {
		args = append(args, "--exclude", outDir)
	}
	args = append(args,
		strings.TrimRight(localDir, "/")+"/",
		fmt.Sprintf("%s:%s/", host, remoteDir),
	)
	cmd := exec.Command("rsync", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("rsync to remote failed: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func execRemoteBuild(host, remoteDir string, forwardedArgs []string) error {
	var quoted []string
	for _, a := range forwardedArgs {
		quoted = append(quoted, shellQuote(a))
	}
	quoted = append(quoted, ".")
	remoteCmd := fmt.Sprintf(
		`export PATH="$HOME/go/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"; cd %s && MYGO_REMOTE= mygo build %s`,
		remoteDir,
		strings.Join(quoted, " "),
	)
	cmd := exec.Command("ssh", host, remoteCmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("remote mygo build failed: %w", err)
	}
	return nil
}

func pullFromRemote(host, remoteDir, remoteOut, localOutDir string) error {
	if err := os.MkdirAll(localOutDir, 0o755); err != nil {
		return fmt.Errorf("create local output dir: %w", err)
	}
	src := fmt.Sprintf("%s:%s/%s/", host, remoteDir, strings.Trim(remoteOut, "/"))
	dst := strings.TrimRight(localOutDir, "/") + "/"
	cmd := exec.Command("rsync", "-az", src, dst)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("rsync artifacts from remote failed: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}


