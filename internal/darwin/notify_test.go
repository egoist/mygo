//go:build darwin && !ios

package darwin

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// TestInterruptionLevel checks the UNNotificationInterruptionLevel each
// level asks for: passive 0, active 1, time-sensitive 2.
func TestInterruptionLevel(t *testing.T) {
	for level, want := range map[platform.NotificationLevel]uintptr{
		platform.NotificationPassive:       0,
		platform.NotificationActive:        1,
		platform.NotificationTimeSensitive: 2,
	} {
		if got := interruptionLevel(level); got != want {
			t.Errorf("interruptionLevel(%v) = %d, want %d", level, got, want)
		}
	}
}
