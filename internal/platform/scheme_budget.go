package platform

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"sync/atomic"
)

// SchemeBodyLimits caps buffered desktop custom-scheme request payload bytes.
// Limits apply process-wide across every Protocol.Handle scheme and mygo://.
// Zero disables the corresponding limit.
type SchemeBodyLimits struct {
	// MaxRequestBodyBytes is the maximum payload size in bytes for a single
	// custom-scheme request. Exceeding it returns HTTP 413 without invoking
	// the handler. Zero disables the per-request cap.
	MaxRequestBodyBytes int64
	// MaxInFlightRequestBodyBytes is the maximum combined payload size in
	// bytes across all active custom-scheme requests in the process.
	// Exceeding it returns HTTP 503 without invoking the handler. Zero
	// disables the aggregate cap.
	MaxInFlightRequestBodyBytes int64
}

var schemeBudget struct {
	sync.Mutex
	SchemeBodyLimits
	used int64
}

// The setter installs the accounting functions once so apps using the default
// unlimited configuration keep them out of the binary.
var (
	limitsActive    atomic.Bool
	reserveBodyHook func(*SchemeBodyReservation, int64) int
	releaseBodyHook func(*SchemeBodyReservation)
	readBodyHook    func(*SchemeBodyReservation, func([]byte) (int, bool, error)) (io.ReadCloser, int)
)

func SetSchemeBodyLimits(l SchemeBodyLimits) error {
	if l.MaxRequestBodyBytes < 0 || l.MaxInFlightRequestBodyBytes < 0 {
		return errors.New("negative scheme body limit")
	}
	schemeBudget.Lock()
	defer schemeBudget.Unlock()
	schemeBudget.SchemeBodyLimits = l
	if l == (SchemeBodyLimits{}) {
		limitsActive.Store(false)
		return nil
	}
	if reserveBodyHook == nil {
		reserveBodyHook = reserveBody
		releaseBodyHook = releaseBody
		readBodyHook = readLimitedBody
	}
	limitsActive.Store(true)
	return nil
}

type SchemeBodyReservation struct {
	size int64
}

func (r *SchemeBodyReservation) Reserve(n int64) int {
	if !limitsActive.Load() {
		return 0
	}
	return reserveBodyHook(r, n)
}

func reserveBody(r *SchemeBodyReservation, n int64) int {
	schemeBudget.Lock()
	defer schemeBudget.Unlock()
	if schemeBudget.MaxRequestBodyBytes == 0 && schemeBudget.MaxInFlightRequestBodyBytes == 0 {
		return 0
	}
	if n < 0 || schemeBudget.MaxRequestBodyBytes > 0 && n > schemeBudget.MaxRequestBodyBytes-r.size {
		return 413
	}
	if schemeBudget.MaxInFlightRequestBodyBytes > 0 && n > schemeBudget.MaxInFlightRequestBodyBytes-schemeBudget.used {
		return 503
	}
	schemeBudget.used += n
	r.size += n
	return 0
}

func (r *SchemeBodyReservation) Release() {
	if r.size != 0 {
		releaseBodyHook(r)
	}
}

func releaseBody(r *SchemeBodyReservation) {
	schemeBudget.Lock()
	defer schemeBudget.Unlock()
	schemeBudget.used -= r.size
	r.size = 0
}

type bufferedSchemeBody struct {
	io.ReadCloser
	length int
}

func (b bufferedSchemeBody) Len() int { return b.length }

func (r *SchemeBodyReservation) Read(read func([]byte) (int, bool, error)) (io.ReadCloser, int) {
	if limitsActive.Load() {
		return readBodyHook(r, read)
	}
	var out []byte
	buf := make([]byte, 64<<10)
	for {
		n, eof, err := read(buf)
		if err != nil || n < 0 || n > len(buf) || n == 0 && !eof {
			return nil, 500
		}
		out = append(out, buf[:n]...)
		if eof {
			return BytesBody(out), 0
		}
	}
}

// Pack short native reads into full chunks, keeping only the current
// scratch buffer outside the payload reservation.
func readLimitedBody(r *SchemeBodyReservation, read func([]byte) (int, bool, error)) (io.ReadCloser, int) {
	buf := make([]byte, 64<<10)
	var chunks []io.Reader
	used := 0
	total := 0
	for {
		n, eof, err := read(buf[used:])
		if err != nil || n < 0 || n > len(buf)-used || n == 0 && !eof {
			return nil, 500
		}
		if status := reserveBody(r, int64(n)); status != 0 {
			return nil, status
		}
		used += n
		total += n
		if used == len(buf) || eof {
			if used > 0 {
				chunks = append(chunks, bytes.NewReader(bytes.Clone(buf[:used])))
			}
			used = 0
		}
		if eof {
			return bufferedSchemeBody{io.NopCloser(io.MultiReader(chunks...)), total}, 0
		}
	}
}
