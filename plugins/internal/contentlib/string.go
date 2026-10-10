package contentlib

import "unsafe"

// CString copies a bounded engine-owned NUL-terminated UTF-8 string.
func CString(p unsafe.Pointer) string {
	if p == nil {
		return ""
	}
	var b []byte
	for i := 0; i < 1<<20; i++ {
		c := *(*byte)(unsafe.Add(p, i))
		if c == 0 {
			break
		}
		b = append(b, c)
	}
	return string(b)
}
