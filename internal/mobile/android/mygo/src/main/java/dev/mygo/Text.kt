package dev.mygo

import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.Rect
import android.graphics.Typeface
import android.os.Build
import java.io.File
import java.nio.ByteBuffer
import kotlin.math.abs
import kotlin.math.ceil
import kotlin.math.floor

/**
 * Text of native UI, measured and drawn as a TextView measures and draws
 * it: Minikin lays it out, Skia draws it. Go breaks lines itself and asks
 * how wide words are (getTextRunAdvances) and what a cluster looks like
 * (drawText).
 */
object Text {
    // The faces the app registered under each family, a weight and style each.
    private val registered = HashMap<String, ArrayList<Typeface>>()
    private val faces = HashMap<String, Typeface>()
    private val paint = Paint(Paint.ANTI_ALIAS_FLAG or Paint.SUBPIXEL_TEXT_FLAG)
    private val bounds = Rect()
    private var bitmap: Bitmap? = null

    /** The typeface of a CSS-like list of families, at a weight. */
    private fun typeface(families: String, weight: Long, italic: Boolean): Typeface {
        val key = "$families|$weight|$italic"
        faces[key]?.let { return it }
        var base: Typeface? = null
        for (raw in families.split(',')) {
            val f = raw.trim().trim('"', '\'')
            base = registered[f]?.let { nearest(it, weight, italic) } ?: when (f.lowercase()) {
                "", "system-ui", "sans-serif", "ui-sans-serif", "-apple-system" -> Typeface.SANS_SERIF
                "serif", "ui-serif" -> Typeface.SERIF
                "monospace", "ui-monospace" -> Typeface.MONOSPACE
                else -> Typeface.create(f, Typeface.NORMAL).takeIf { it != Typeface.DEFAULT }
            }
            if (base != null) break
        }
        val face = if (Build.VERSION.SDK_INT >= 28) {
            Typeface.create(base ?: Typeface.SANS_SERIF, weight.toInt().coerceIn(1, 1000), italic)
        } else {
            Typeface.create(base ?: Typeface.SANS_SERIF, (if (weight >= 600) Typeface.BOLD else 0) or (if (italic) Typeface.ITALIC else 0))
        }
        if (faces.size > 256) faces.clear()
        faces[key] = face
        return face
    }

    /**
     * The face of a family nearest a weight and style: Android makes it
     * heavier or slants it as the text asks, which a lighter face drawn
     * bold or a bold one drawn regular would get wrong.
     */
    private fun nearest(faces: List<Typeface>, weight: Long, italic: Boolean): Typeface =
        faces.minBy { (if (it.isItalic == italic) 0 else 10_000) + abs(weightOf(it) - weight.toInt()) }

    private fun weightOf(t: Typeface): Int =
        if (Build.VERSION.SDK_INT >= 28) t.weight else if (t.isBold) 700 else 400

    /**
     * A piece of a word within the word: text [start, end) of the whole,
     * which Go sends as before + sep + piece + sep + after (contextText in
     * internal/text), the separator saying the run's direction. Minikin
     * lays the whole out, so letters that join keep their forms where the
     * word's style, a tajweed color, changes.
     */
    private class Piece(val text: String, val start: Int, val end: Int, val rtl: Boolean)

    private const val CONTEXT_RTL = '\u0001'
    private const val CONTEXT_LTR = '\u0002'

    private fun piece(t: String): Piece? {
        val sep = when {
            t.indexOf(CONTEXT_RTL) >= 0 -> CONTEXT_RTL
            t.indexOf(CONTEXT_LTR) >= 0 -> CONTEXT_LTR
            else -> return null
        }
        val a = t.indexOf(sep)
        val b = t.indexOf(sep, a + 1)
        if (b < 0) return null
        val whole = t.substring(0, a) + t.substring(a + 1, b) + t.substring(b + 1)
        return Piece(whole, a, b - 1, sep == CONTEXT_RTL)
    }

    private fun set(family: String, size: Double, weight: Long, italic: Boolean, noKern: Boolean = false): Paint {
        paint.typeface = typeface(family, weight, italic)
        paint.textSize = size.toFloat()
        features(noKern, false)
        paint.color = Color.WHITE
        return paint
    }

    /**
     * Go draws clusters alone, which ligatures would join: "fl" would take
     * the advance of both, and draw as an f before a gap. A word of a
     * complex script (a Piece) is shaped whole, and keeps them: fonts set
     * Arabic with them, as Scheherazade puts an ayah's number in its mark.
     */
    private fun features(noKern: Boolean, whole: Boolean) {
        val f = when {
            whole && noKern -> "'kern' 0"
            whole -> ""
            noKern -> "'liga' 0, 'clig' 0, 'kern' 0"
            else -> "'liga' 0, 'clig' 0"
        }
        if (paint.fontFeatureSettings.orEmpty() != f) paint.fontFeatureSettings = f
    }

    @Synchronized
    fun metrics(c: TextMetricsCall): TextMetricsReply {
        val p = set(c.family, c.size, c.weight, c.italic)
        val m = p.fontMetrics
        val r = TextMetricsReply()
        r.ascent = -m.ascent.toDouble()
        r.descent = m.descent.toDouble()
        p.getTextBounds("x", 0, 1, bounds)
        r.xHeight = -bounds.top.toDouble()
        if (Build.VERSION.SDK_INT >= 29) {
            r.underlinePosition = p.underlinePosition.toDouble()
            r.underlineThickness = p.underlineThickness.toDouble()
            r.strikePosition = -p.strikeThruPosition.toDouble()
        } else {
            r.underlinePosition = c.size / 10
            r.underlineThickness = maxOf(c.size / 14, 1.0)
            r.strikePosition = r.xHeight / 2
        }
        return r
    }

    @Synchronized
    fun measure(c: MeasureTextCall): MeasureTextReply {
        val p = set(c.family, c.size, c.weight, c.italic, c.noKern)
        val r = MeasureTextReply()
        r.widths = DoubleArray(c.texts.size) { i ->
            val t = c.texts[i]
            val k = piece(t)
            features(c.noKern, k != null)
            if (k != null) {
                p.getRunAdvance(k.text, k.start, k.end, 0, k.text.length, k.rtl, k.end).toDouble()
            } else {
                p.getRunAdvance(t, 0, t.length, 0, t.length, false, t.length).toDouble()
            }
        }
        return r
    }

    @Synchronized
    fun advances(c: TextAdvancesCall): TextAdvancesReply {
        val p = set(c.family, c.size, c.weight, c.italic, c.noKern)
        val chars = c.text.toCharArray()
        val out = FloatArray(chars.size)
        p.getTextRunAdvances(chars, 0, chars.size, 0, chars.size, c.rtl, out, 0)
        val r = TextAdvancesReply()
        r.advances = DoubleArray(out.size) { out[it].toDouble() }
        return r
    }

    @Synchronized
    fun draw(c: DrawTextCall): DrawTextReply {
        val r = DrawTextReply()
        val k = piece(c.text)
        if ((k?.text?.substring(k.start, k.end) ?: c.text).isBlank()) return r
        // The font at the size text was measured at, the canvas scaled, as
        // the measurements were made.
        val p = set(c.family, c.size, c.weight, c.italic)
        features(false, k != null)
        // A piece draws from its own pen, where the whole word would draw
        // from shift: the ink of the whole, there, holds the piece's.
        var shift = 0f
        if (k != null) {
            p.getTextBounds(k.text, 0, k.text.length, bounds)
            val n = k.text.length
            shift = if (k.rtl) {
                -(p.getRunAdvance(k.text, 0, n, 0, n, true, n) - p.getRunAdvance(k.text, 0, n, 0, n, true, k.end))
            } else {
                -p.getRunAdvance(k.text, 0, n, 0, n, false, k.start)
            }
            bounds.offset(floor(shift).toInt(), 0)
            bounds.right += 1
        } else {
            p.getTextBounds(c.text, 0, c.text.length, bounds)
        }
        val s = c.scale
        val pad = 2
        var left = floor(bounds.left * s + c.dx).toInt() - pad
        val right = ceil(bounds.right * s + c.dx).toInt() + pad
        val top = floor(bounds.top * s).toInt() - pad
        val bottom = ceil(bounds.bottom * s).toInt() + pad
        var w = right - left
        val h = bottom - top
        if (w <= 2 * pad || h <= 2 * pad || w > 2048 || h > 2048) return r
        var bm = bitmap
        if (bm == null || bm.width < w || bm.height < h) {
            bm?.recycle()
            bm = Bitmap.createBitmap(maxOf(w, bm?.width ?: 0, 64), maxOf(h, bm?.height ?: 0, 64), Bitmap.Config.ARGB_8888)
            bitmap = bm
        }
        bm!!.eraseColor(Color.TRANSPARENT)
        val canvas = Canvas(bm)
        canvas.translate((c.dx - left).toFloat(), (-top).toFloat())
        canvas.scale(s.toFloat(), s.toFloat())
        if (k != null) {
            canvas.drawTextRun(k.text, k.start, k.end, 0, k.text.length, 0f, 0f, k.rtl, p)
        } else {
            canvas.drawText(c.text, 0f, 0f, p)
        }
        val buf = ByteBuffer.allocate(bm.byteCount)
        bm.copyPixelsToBuffer(buf)
        val px = buf.array()
        val stride = bm.width * 4
        // A piece's bitmap holds the whole word's room: only the columns
        // the piece inked, and the pad around them, are kept.
        var x0 = 0
        if (k != null) {
            var lo = w
            var hi = -1
            for (y in 0 until h) for (x in 0 until w) {
                if (px[y * stride + x * 4 + 3].toInt() != 0) {
                    if (x < lo) lo = x
                    if (x > hi) hi = x
                }
            }
            if (hi < 0) return r
            x0 = maxOf(lo - pad, 0)
            w = minOf(hi + pad + 1, w) - x0
            left += x0
        }
        // White text is as covered as it is opaque; other colors are an
        // emoji's.
        var color = false
        loop@ for (y in 0 until h) for (x in 0 until w) {
            val i = y * stride + (x0 + x) * 4
            val a = px[i + 3].toInt() and 0xff
            if (a > 32) {
                val far = { v: Int -> v < a - 24 }
                if (far(px[i].toInt() and 0xff) || far(px[i + 1].toInt() and 0xff) || far(px[i + 2].toInt() and 0xff)) {
                    color = true
                    break@loop
                }
            }
        }
        r.left = left.toLong()
        r.top = top.toLong()
        r.width = w.toLong()
        r.height = h.toLong()
        r.color = color
        r.pixels = if (color) {
            ByteArray(w * h * 4).also { out -> for (y in 0 until h) System.arraycopy(px, y * stride + x0 * 4, out, y * w * 4, w * 4) }
        } else {
            ByteArray(w * h) { i -> px[(i / w) * stride + (x0 + i % w) * 4 + 3] }
        }
        return r
    }

    @Synchronized
    fun register(c: RegisterFontCall): RegisterFontReply {
        val r = RegisterFontReply()
        try {
            val f = File.createTempFile("font", ".ttf", HostImpl.app.cacheDir)
            f.writeBytes(c.data)
            val face = Typeface.createFromFile(f)
            f.delete()
            val family = c.family.ifEmpty { "font${registered.size + 1}" }
            registered.getOrPut(family) { ArrayList() }.add(face)
            faces.clear()
            r.family = family
        } catch (e: Exception) {
            r.error = "mygo: ${e.message}"
        }
        return r
    }
}
