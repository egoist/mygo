package platform

import (
	"io"
	"testing"
)

func TestSchemeBodyLimits(t *testing.T) {
	const mb = 1 << 20
	if err := SetSchemeBodyLimits(SchemeBodyLimits{5 * mb, 32 * mb}); err != nil {
		t.Fatal(err)
	}
	defer SetSchemeBodyLimits(SchemeBodyLimits{})
	for _, invalid := range []SchemeBodyLimits{{-1, 0}, {0, -1}} {
		if err := SetSchemeBodyLimits(invalid); err == nil {
			t.Fatalf("accepted negative limits %+v", invalid)
		}
	}
	var active [7]SchemeBodyReservation
	for i := range active {
		defer active[i].Release()
	}
	for i := 0; i < 6; i++ {
		if status := active[i].Reserve(5 * mb); status != 0 {
			t.Fatal(status)
		}
		if status := active[i].Reserve(1); status != 413 {
			t.Fatal(status)
		}
	}
	if status := active[6].Reserve(2 * mb); status != 0 {
		t.Fatal(status)
	}
	if status := active[6].Reserve(1); status != 503 {
		t.Fatal(status)
	}
	active[0].Release()
	active[0].Release()
	if status := active[6].Reserve(3 * mb); status != 0 {
		t.Fatal(status)
	}
}

func TestSchemeBodyRead(t *testing.T) {
	for _, tc := range []struct {
		name         string
		limit        SchemeBodyLimits
		size, status int
	}{
		{"exact", SchemeBodyLimits{5 << 20, 32 << 20}, 5 << 20, 0},
		{"request", SchemeBodyLimits{5 << 20, 32 << 20}, 5<<20 + 1, 413},
		{"inflight", SchemeBodyLimits{0, 32 << 20}, 32<<20 + 1, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := SetSchemeBodyLimits(tc.limit); err != nil {
				t.Fatal(err)
			}
			defer SetSchemeBodyLimits(SchemeBodyLimits{})
			var r SchemeBodyReservation
			defer r.Release()
			left := tc.size
			body, status := r.Read(func(buf []byte) (int, bool, error) {
				n := min(len(buf), left)
				left -= n
				return n, left == 0, nil
			})
			if status != tc.status {
				t.Fatalf("status %d", status)
			}
			if body != nil {
				sized, ok := body.(interface{ Len() int })
				if !ok || sized.Len() != tc.size {
					t.Fatalf("buffered body does not report its length of %d", tc.size)
				}
				n, err := io.Copy(io.Discard, body)
				if err != nil || n != int64(tc.size) {
					t.Fatalf("body %d: %v", n, err)
				}
			}
		})
	}
}

func TestSchemeBodyOverlappingLimits(t *testing.T) {
	const chunk = 64 << 10
	if err := SetSchemeBodyLimits(SchemeBodyLimits{2 * chunk, 4 * chunk}); err != nil {
		t.Fatal(err)
	}
	defer SetSchemeBodyLimits(SchemeBodyLimits{})
	var a, b, upload SchemeBodyReservation
	defer a.Release()
	defer b.Release()
	defer upload.Release()
	if status := a.Reserve(2 * chunk); status != 0 {
		t.Fatal(status)
	}
	if status := b.Reserve(chunk); status != 0 {
		t.Fatal(status)
	}
	if status := upload.Reserve(3 * chunk); status != 413 {
		t.Fatal(status)
	}
	left := 3 * chunk
	body, status := upload.Read(func(buf []byte) (int, bool, error) {
		n := min(left, len(buf))
		left -= n
		return n, left == 0, nil
	})
	if body != nil || status != 503 || left != chunk {
		t.Fatalf("stream returned %v, status %d, %d bytes unread", body, status, left)
	}
}

func TestSchemeBodyClearLimitsMidStream(t *testing.T) {
	if err := SetSchemeBodyLimits(SchemeBodyLimits{MaxRequestBodyBytes: 64 << 10}); err != nil {
		t.Fatal(err)
	}
	defer SetSchemeBodyLimits(SchemeBodyLimits{})
	var r SchemeBodyReservation
	defer r.Release()
	chunks := [][]byte{[]byte("hello "), []byte("world")}
	idx := 0
	body, status := r.Read(func(buf []byte) (int, bool, error) {
		if idx == 0 {
			if err := SetSchemeBodyLimits(SchemeBodyLimits{}); err != nil {
				return 0, false, err
			}
		}
		n := copy(buf, chunks[idx])
		idx++
		return n, idx == len(chunks), nil
	})
	if status != 0 || body == nil {
		t.Fatalf("status=%d body=%v", status, body)
	}
	sized, ok := body.(interface{ Len() int })
	if !ok || sized.Len() != len("hello world") {
		t.Fatalf("Len() = %v (ok=%v), want %d", sized, ok, len("hello world"))
	}
	got, err := io.ReadAll(body)
	if err != nil || string(got) != "hello world" {
		t.Fatalf("body=%q err=%v", got, err)
	}
}
