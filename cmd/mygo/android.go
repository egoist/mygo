package main

import (
	"context"
	"errors"
	"os/exec"
)

func buildAndroid(c *Config, opts buildOptions, arch string) ([]string, error) {
	return nil, errors.New("android: not yet")
}

func androidDevice(id string) (*mobileDevice, error) { return nil, errors.New("android: not yet") }

func (s *devSession) buildAndroidDev(ctx context.Context, c, dc *Config, t *task, opts buildOptions, dir string) (string, error) {
	return "", errors.New("android: not yet")
}

func (s *devSession) launchAndroid(dc *Config, app string, env []string) (*exec.Cmd, func(), error) {
	return nil, nil, errors.New("android: not yet")
}

// reachDevURL returns the URL at which the device reaches the dev server
// at url: the simulator shares the Mac's network.
func (d *mobileDevice) reachDevURL(url string) (string, error) { return url, nil }
