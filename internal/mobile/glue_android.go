//go:build android

package mobile

/*
#cgo LDFLAGS: -landroid -llog
#include <stdint.h>
#include <stdlib.h>

void mygo_android_call(uint8_t *msg, size_t len, uint8_t **reply, size_t *reply_len);
void mygo_android_post(void);
void mygo_android_log(int error, uint8_t *line, size_t len);
*/
import "C"

import "unsafe"

// cHost is the Kotlin host, through JNI (jni_android.c).
type cHost struct{}

func (cHost) call(msg []byte) []byte {
	var reply *C.uint8_t
	var n C.size_t
	C.mygo_android_call((*C.uint8_t)(unsafe.Pointer(unsafe.SliceData(msg))), C.size_t(len(msg)), &reply, &n)
	if reply == nil {
		return nil
	}
	b := C.GoBytes(unsafe.Pointer(reply), C.int(n))
	C.free(unsafe.Pointer(reply))
	return b
}

func (cHost) post() { C.mygo_android_post() }

func hostLog(isErr bool, line []byte) {
	e := C.int(0)
	if isErr {
		e = 1
	}
	C.mygo_android_log(e, (*C.uint8_t)(unsafe.Pointer(unsafe.SliceData(line))), C.size_t(len(line)))
}

//export mygo_start_android
func mygo_start_android() {
	theHost = cHost{}
	start()
}

//export mygo_event
func mygo_event(msg *C.uint8_t, n C.size_t, onUI C.int, reply **C.uint8_t, replyLen *C.size_t) {
	r := deliver(unsafe.Slice((*byte)(unsafe.Pointer(msg)), int(n)), onUI != 0)
	*reply, *replyLen = nil, 0
	if r != nil {
		p := C.malloc(C.size_t(max(len(r), 1)))
		copy(unsafe.Slice((*byte)(p), len(r)), r)
		*reply, *replyLen = (*C.uint8_t)(p), C.size_t(len(r))
	}
}

//export mygo_drain
func mygo_drain() { drain() }
