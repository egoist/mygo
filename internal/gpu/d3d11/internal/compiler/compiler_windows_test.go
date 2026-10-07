//go:build windows

package compiler

import (
	"fmt"
	"sync"
	"testing"
)

func TestConcurrentCompilation(t *testing.T) {
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			source := fmt.Sprintf("float4 vs() : SV_Position { return float4(%d, 0, 0, 1); }", i)
			if i%2 != 0 {
				source = "this is not HLSL"
			}
			code, err := Compile(source, "vs", "vs_4_0")
			if i%2 != 0 {
				if err == nil || len(code) != 0 {
					t.Errorf("invalid source compiled: %x, %v", code, err)
				}
			} else if err != nil || len(code) < 4 || string(code[:4]) != "DXBC" {
				t.Errorf("valid source failed: %x, %v", code, err)
			}
		})
	}
	wg.Wait()
}
