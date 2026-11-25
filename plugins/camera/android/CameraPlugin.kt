package dev.mygo

import android.Manifest
import android.content.Context
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.ImageCapture
import androidx.camera.core.ImageCaptureException
import androidx.camera.core.Preview
import androidx.camera.core.UseCase
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.core.content.ContextCompat
import androidx.lifecycle.LifecycleOwner
import com.google.zxing.BinaryBitmap
import com.google.zxing.PlanarYUVLuminanceSource
import com.google.zxing.ReaderException
import com.google.zxing.common.HybridBinarizer
import com.google.zxing.qrcode.QRCodeReader
import org.json.JSONObject
import java.io.File
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors

class CameraPlugin : MyGoPlugin {
    private class Cam(val id: Long, val view: PreviewView) {
        val capture = ImageCapture.Builder().build()
        var bound = false
        var error = "the camera is not running"
        // The thread reading QR codes, while the app asks for them.
        var codes: ExecutorService? = null
        // The code told last, which is told again only after another.
        var lastCode: String? = null
    }

    private val cams = HashMap<Long, Cam>()

    init {
        PluginHost.registerView("camera") { context, id, args ->
            val v = PreviewView(context)
            // A texture, for it to show under the surface that draws over it.
            v.implementationMode = PreviewView.ImplementationMode.COMPATIBLE
            val c = Cam(id, v)
            cams[id] = c
            start(c, args.optBoolean("front"), args.optBoolean("codes"))
            v
        }
    }

    private fun start(c: Cam, front: Boolean, codes: Boolean) {
        PluginHost.requestPermissions(arrayOf(Manifest.permission.CAMERA)) { granted ->
            if (!granted) {
                c.error = "the app may not use the camera"
                return@requestPermissions
            }
            val ctx = PluginHost.context
            val future = ProcessCameraProvider.getInstance(ctx)
            future.addListener({
                val owner = PluginHost.activity as? LifecycleOwner
                if (owner == null) {
                    c.error = "no window to show the camera in"
                    return@addListener
                }
                val preview = Preview.Builder().build().also { it.surfaceProvider = c.view.surfaceProvider }
                val which = if (front) CameraSelector.DEFAULT_FRONT_CAMERA else CameraSelector.DEFAULT_BACK_CAMERA
                val cases = mutableListOf<UseCase>(preview, c.capture)
                if (codes) cases += codeReader(c)
                runCatching {
                    future.get().bindToLifecycle(owner, which, *cases.toTypedArray())
                    c.bound = true
                }.onFailure {
                    c.error = it.toString()
                    android.util.Log.e("mygo", "camera: $it")
                }
            }, ContextCompat.getMainExecutor(ctx))
        }
    }

    /** Reads the QR codes in the camera's frames, from their luminance, as they come. */
    private fun codeReader(c: Cam): ImageAnalysis {
        val analysis = ImageAnalysis.Builder().setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST).build()
        val reader = QRCodeReader()
        val thread = Executors.newSingleThreadExecutor()
        c.codes = thread
        analysis.setAnalyzer(thread) { image ->
            image.use {
                // The first plane of YUV_420_888 is the luminance, rowStride bytes a row.
                val y = it.planes[0]
                val bytes = ByteArray(y.buffer.remaining()).also { b -> y.buffer.get(b) }
                val rows = (bytes.size + y.rowStride - 1) / y.rowStride
                val source = PlanarYUVLuminanceSource(bytes, y.rowStride, rows, 0, 0, it.width, it.height, false)
                val text = try {
                    reader.decode(BinaryBitmap(HybridBinarizer(source))).text
                } catch (e: ReaderException) {
                    null
                } finally {
                    reader.reset()
                }
                if (text != null && text != c.lastCode) {
                    c.lastCode = text
                    PluginHost.emit("camera", "code", JSONObject().put("id", c.id).put("text", text))
                }
            }
        }
        return analysis
    }

    override fun call(method: String, args: JSONObject, reply: PluginReply) {
        val id = args.optLong("id")
        val c = cams[id]
        if (c == null) {
            reply.failure(PluginFailure("NoView"))
            return
        }
        when (method) {
            "takePhoto" -> {
                if (!c.bound) {
                    reply.failure(PluginFailure("NoCamera", c.error))
                    return
                }
                val ctx = PluginHost.context
                val f = File(ctx.cacheDir, "${System.nanoTime()}.jpg")
                c.capture.takePicture(ImageCapture.OutputFileOptions.Builder(f).build(), ContextCompat.getMainExecutor(ctx),
                    object : ImageCapture.OnImageSavedCallback {
                        override fun onImageSaved(r: ImageCapture.OutputFileResults) = reply.success(JSONObject().put("path", f.absolutePath))
                        override fun onError(e: ImageCaptureException) = reply.failure(PluginFailure("Failed", e.toString()))
                    })
            }
            "dispose" -> {
                cams.remove(id)
                c.codes?.shutdown()
                runCatching { ProcessCameraProvider.getInstance(PluginHost.context).get().unbindAll() }
                reply.success(null)
            }
            else -> reply.failure(PluginFailure("NoMethod", method))
        }
    }
}
