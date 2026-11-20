package viewgen

import (
	"os"
)

// environ is the environment of the go commands the generator runs:
// the caller's, with the build's own settings applied and cgo left to
// the build the way the CLI's own commands leave it (#1457).
func environ(buildEnv []string) []string {
	env := os.Environ()
	if os.Getenv("CGO_ENABLED") == "" {
		env = append(env, "CGO_ENABLED=0")
	}
	return append(env, buildEnv...)
}
