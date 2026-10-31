package dev.mygo

/** Text of native UI, shaped and drawn as Android's TextView does. */
object Text {
    fun shape(c: ShapeCall): ShapeReply = ShapeReply()
    fun draw(c: DrawGlyphsCall): DrawGlyphsReply = DrawGlyphsReply()
}
