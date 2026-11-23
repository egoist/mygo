package dev.mygo

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.graphics.BitmapFactory
import android.media.AudioAttributes
import android.media.AudioDeviceCallback
import android.media.AudioDeviceInfo
import android.media.AudioFocusRequest
import android.media.AudioManager
import android.media.MediaMetadata
import android.media.session.MediaSession
import android.media.session.PlaybackState
import android.os.IBinder
import android.util.Base64
import org.json.JSONObject

private fun lifecycle(kind: String) {
    PluginHost.emit("backgroundaudio", "lifecycle", JSONObject().put("Kind", kind))
}

class BackgroundAudioPlugin : MyGoPlugin {
    private val context get() = PluginHost.context
    private val audio get() = context.getSystemService(Context.AUDIO_SERVICE) as AudioManager
    private var session: MediaSession? = null
    private var focus: AudioFocusRequest? = null
    private var interrupted = false
    private val devices = object : AudioDeviceCallback() {
        override fun onAudioDevicesAdded(devices: Array<AudioDeviceInfo>) {
            if (devices.any { it.isSink }) lifecycle("outputChanged")
        }
        override fun onAudioDevicesRemoved(devices: Array<AudioDeviceInfo>) {
            if (devices.any { it.isSink }) lifecycle("outputRemoved")
        }
    }

    private fun requestFocus() {
        if (focus != null) return
        val request = AudioFocusRequest.Builder(AudioManager.AUDIOFOCUS_GAIN)
            .setAudioAttributes(AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_MEDIA).build())
            .setWillPauseWhenDucked(true)
            .setOnAudioFocusChangeListener { change ->
                interrupted = change == AudioManager.AUDIOFOCUS_LOSS_TRANSIENT || change == AudioManager.AUDIOFOCUS_LOSS_TRANSIENT_CAN_DUCK
                if (change == AudioManager.AUDIOFOCUS_LOSS) abandonFocus()
                lifecycle(when (change) {
                    AudioManager.AUDIOFOCUS_LOSS -> "focusLoss"
                    AudioManager.AUDIOFOCUS_LOSS_TRANSIENT -> "focusTransient"
                    AudioManager.AUDIOFOCUS_LOSS_TRANSIENT_CAN_DUCK -> "duck"
                    else -> "focusGain"
                })
            }.build()
        if (audio.requestAudioFocus(request) == AudioManager.AUDIOFOCUS_REQUEST_GRANTED) focus = request
        else lifecycle("focusLoss")
    }

    private fun abandonFocus() {
        focus?.let { audio.abandonAudioFocusRequest(it) }
        focus = null
        interrupted = false
    }

    private fun createSession(): MediaSession {
        val s = MediaSession(context, "MyGo")
        s.setCallback(object : MediaSession.Callback() {
            fun command(bit: Int, ms: Long = 0) {
                PluginHost.emit("backgroundaudio", "command", JSONObject().put("Command", bit).put("Milliseconds", ms))
            }
            override fun onPlay() = command(1)
            override fun onPause() = command(2)
            override fun onStop() = command(8)
            override fun onSkipToNext() = command(16)
            override fun onSkipToPrevious() = command(32)
            override fun onSeekTo(pos: Long) = command(64, pos)
        })
        audio.registerAudioDeviceCallback(devices, null)
        s.isActive = true
        session = s
        return s
    }

    override fun call(method: String, args: JSONObject, reply: PluginReply) {
        if (method == "clear") {
            session?.let {
                audio.unregisterAudioDeviceCallback(devices)
                it.release()
            }
            session = null
            abandonFocus()
            BackgroundAudioService.update(context, null, false)
            reply.success(null)
            return
        }
        val s = session ?: createSession()
        val title = args.optString("Title")
        val artist = args.optString("Artist")
        val metadata = MediaMetadata.Builder()
            .putString(MediaMetadata.METADATA_KEY_TITLE, title)
            .putString(MediaMetadata.METADATA_KEY_ARTIST, artist)
            .putString(MediaMetadata.METADATA_KEY_ALBUM, args.optString("Album"))
        val duration = args.optLong("Duration") / 1000000
        if (duration > 0) metadata.putLong(MediaMetadata.METADATA_KEY_DURATION, duration)
        if (!args.isNull("Artwork")) {
            val bytes = Base64.decode(args.getString("Artwork"), Base64.DEFAULT)
            metadata.putBitmap(MediaMetadata.METADATA_KEY_ALBUM_ART, BitmapFactory.decodeByteArray(bytes, 0, bytes.size))
        }
        s.setMetadata(metadata.build())
        val mask = args.optInt("Commands")
        var actions = 0L
        for ((bit, action) in listOf(1 to PlaybackState.ACTION_PLAY, 2 to PlaybackState.ACTION_PAUSE,
            8 to PlaybackState.ACTION_STOP, 16 to PlaybackState.ACTION_SKIP_TO_NEXT,
            32 to PlaybackState.ACTION_SKIP_TO_PREVIOUS, 64 to PlaybackState.ACTION_SEEK_TO)) {
            if ((mask and bit) != 0) actions = actions or action
        }
        val state = when (args.optString("State")) {
            "playing" -> PlaybackState.STATE_PLAYING
            "paused" -> PlaybackState.STATE_PAUSED
            else -> PlaybackState.STATE_STOPPED
        }
        val playing = state == PlaybackState.STATE_PLAYING
        s.setPlaybackState(PlaybackState.Builder().setActions(actions)
            .setState(state, args.optLong("Position") / 1000000, if (playing) args.optDouble("Rate", 1.0).toFloat() else 0f).build())
        val manager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        val channel = "mygo.backgroundaudio"
        manager.createNotificationChannel(NotificationChannel(channel, "Playback", NotificationManager.IMPORTANCE_LOW))
        val builder = Notification.Builder(context, channel)
            .setContentTitle(title).setContentText(artist).setSmallIcon(android.R.drawable.ic_media_play)
        // Older versions build the media controls from notification actions.
        fun action(bit: Int, icon: Int, label: String) {
            if ((mask and bit) == 0) return
            val press = Intent(context, BackgroundAudioAction::class.java).putExtra("command", bit)
            val pending = PendingIntent.getBroadcast(context, bit, press, PendingIntent.FLAG_IMMUTABLE)
            builder.addAction(Notification.Action.Builder(android.graphics.drawable.Icon.createWithResource(context, icon), label, pending).build())
        }
        action(32, android.R.drawable.ic_media_previous, "Previous")
        if (playing) action(2, android.R.drawable.ic_media_pause, "Pause")
        else action(1, android.R.drawable.ic_media_play, "Play")
        action(16, android.R.drawable.ic_media_next, "Next")
        action(8, android.R.drawable.ic_delete, "Stop")
        val notification = builder.setStyle(Notification.MediaStyle().setMediaSession(s.sessionToken)).build()
        BackgroundAudioService.update(context, notification, playing, if (playing) ::requestFocus else null)
        if (!playing && (!interrupted || state == PlaybackState.STATE_STOPPED)) abandonFocus()
        reply.success(null)
    }
}

class BackgroundAudioAction : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        PluginHost.emit("backgroundaudio", "command", JSONObject().put("Command", intent.getIntExtra("command", 0)))
    }
}

class BackgroundAudioService : Service() {
    companion object {
        private const val ID = 2099
        private var active: BackgroundAudioService? = null
        private var current: Notification? = null
        private var starting: Notification? = null
        private var playing = false
        private var onPromoted: (() -> Unit)? = null

        // All accesses are on the UI thread. A queued start must promote the
        // service even if Go paused or cleared before onStartCommand ran.
        fun update(context: Context, notice: Notification?, play: Boolean, promoted: (() -> Unit)? = null) {
            current = notice
            playing = play
            onPromoted = promoted
            val service = active
            if (service != null) service.refresh()
            else if (play && starting == null) {
                context.startForegroundService(Intent(context, BackgroundAudioService::class.java))
                starting = notice
            } else if (!play) publish(context)
        }

        private fun publish(context: Context) {
            val manager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
            val notice = current
            if (notice == null) manager.cancel(ID) else manager.notify(ID, notice)
        }
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        starting?.let { startForeground(ID, it) }
        starting = null
        active = this
        refresh()
        return START_NOT_STICKY
    }

    private fun refresh() {
        val notice = current
        if (playing && notice != null) {
            startForeground(ID, notice)
            // Android 15+ requires a top app or a running foreground service.
            onPromoted?.invoke()
            onPromoted = null
        } else {
            stopForeground(STOP_FOREGROUND_DETACH)
            active = null
            stopSelf()
            publish(this)
        }
    }

    override fun onDestroy() {
        if (active === this) active = null
        super.onDestroy()
    }
}
