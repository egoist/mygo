# JNI reaches these by name.
-keep class dev.mygo.Host { *; }
-keep class dev.mygo.MyGoNative { *; }
-keepclassmembers class * { @android.webkit.JavascriptInterface <methods>; }
