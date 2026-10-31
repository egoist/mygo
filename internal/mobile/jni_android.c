// The JNI side of the Android host: dev.mygo.MyGoNative's native methods
// call into Go, and Go's calls reach dev.mygo.Host on the UI thread. Go
// wakes the UI thread through a pipe its looper watches, so that no
// thread of Go's ever attaches to the JVM.

#define _GNU_SOURCE
#include <jni.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <fcntl.h>
#include <android/log.h>
#include <android/looper.h>
#include <android/native_window_jni.h>
#include "_cgo_export.h"

static JavaVM *vm;
static jclass host;
static jmethodID host_call;
static int wake[2] = {-1, -1};

JNIEXPORT jint JNI_OnLoad(JavaVM *v, void *reserved) {
	vm = v;
	JNIEnv *env;
	if ((*v)->GetEnv(v, (void **)&env, JNI_VERSION_1_6) != JNI_OK) return JNI_ERR;
	jclass c = (*env)->FindClass(env, "dev/mygo/Host");
	if (c == NULL) return JNI_ERR;
	host = (*env)->NewGlobalRef(env, c);
	host_call = (*env)->GetStaticMethodID(env, host, "call", "([B)[B");
	return JNI_VERSION_1_6;
}

static JNIEnv *ui_env(void) {
	JNIEnv *env = NULL;
	(*vm)->GetEnv(vm, (void **)&env, JNI_VERSION_1_6);
	return env;
}

void mygo_android_call(uint8_t *msg, size_t len, uint8_t **reply, size_t *reply_len) {
	JNIEnv *env = ui_env();
	*reply = NULL;
	*reply_len = 0;
	jbyteArray a = (*env)->NewByteArray(env, (jsize)len);
	(*env)->SetByteArrayRegion(env, a, 0, (jsize)len, (const jbyte *)msg);
	jbyteArray r = (jbyteArray)(*env)->CallStaticObjectMethod(env, host, host_call, a);
	(*env)->DeleteLocalRef(env, a);
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionDescribe(env);
		(*env)->ExceptionClear(env);
		return;
	}
	if (r == NULL) return;
	jsize n = (*env)->GetArrayLength(env, r);
	*reply = malloc(n > 0 ? n : 1);
	(*env)->GetByteArrayRegion(env, r, 0, n, (jbyte *)*reply);
	*reply_len = n;
	(*env)->DeleteLocalRef(env, r);
}

void mygo_android_post(void) {
	char c = 1;
	write(wake[1], &c, 1);
}

void mygo_android_log(int error, uint8_t *line, size_t len) {
	char *s = malloc(len + 1);
	memcpy(s, line, len);
	s[len] = 0;
	__android_log_write(error ? ANDROID_LOG_ERROR : ANDROID_LOG_INFO, "mygo", s);
	free(s);
}

static int on_wake(int fd, int events, void *data) {
	char buf[64];
	while (read(fd, buf, sizeof buf) > 0) {
	}
	mygo_drain();
	return 1;
}

JNIEXPORT void JNICALL Java_dev_mygo_MyGoNative_start(JNIEnv *env, jclass c) {
	if (wake[0] >= 0) return;
	pipe2(wake, O_NONBLOCK | O_CLOEXEC);
	ALooper_addFd(ALooper_forThread(), wake[0], ALOOPER_POLL_CALLBACK, ALOOPER_EVENT_INPUT, on_wake, NULL);
	mygo_start_android();
}

JNIEXPORT jlong JNICALL Java_dev_mygo_MyGoNative_nativeWindow(JNIEnv *env, jclass c, jobject surface) {
	return (jlong)(intptr_t)ANativeWindow_fromSurface(env, surface);
}

JNIEXPORT void JNICALL Java_dev_mygo_MyGoNative_releaseNativeWindow(JNIEnv *env, jclass c, jlong window) {
	if (window != 0) ANativeWindow_release((ANativeWindow *)(intptr_t)window);
}

JNIEXPORT jbyteArray JNICALL Java_dev_mygo_MyGoNative_event(JNIEnv *env, jclass c, jbyteArray msg, jboolean on_ui) {
	jsize n = (*env)->GetArrayLength(env, msg);
	jbyte *b = (*env)->GetByteArrayElements(env, msg, NULL);
	uint8_t *reply = NULL;
	size_t reply_len = 0;
	mygo_event((uint8_t *)b, (size_t)n, on_ui ? 1 : 0, &reply, &reply_len);
	(*env)->ReleaseByteArrayElements(env, msg, b, JNI_ABORT);
	if (reply == NULL) return NULL;
	jbyteArray r = (*env)->NewByteArray(env, (jsize)reply_len);
	(*env)->SetByteArrayRegion(env, r, 0, (jsize)reply_len, (const jbyte *)reply);
	free(reply);
	return r;
}

JNIEXPORT void JNICALL Java_dev_mygo_MyGoNative_drain(JNIEnv *env, jclass c) { mygo_drain(); }
