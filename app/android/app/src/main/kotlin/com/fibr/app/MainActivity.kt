package com.fibr.app

import android.app.UiModeManager
import android.content.Intent
import android.content.pm.ApplicationInfo
import android.content.res.Configuration
import android.net.Uri
import android.os.Build
import android.os.Bundle
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.MethodChannel

/**
 * Hosts two lightweight platform channels the Dart side relies on
 * (no extra pub dependencies needed):
 *
 *  - "fibr/deeplink" (+ events) — fibr:// import links: the initial link
 *    and links arriving via onNewIntent (launchMode is singleTask).
 *    Also carries config FILES: a .conf opened from Telegram / a file
 *    manager ("Open with Fibr", ACTION_VIEW content://) or shared to the app
 *    (ACTION_SEND) arrives as its raw text — the Dart side tells a link from
 *    a wg-quick config by content.
 *  - "fibr/apps" — installed app list for the split-tunneling screen,
 *    plus "isTv" (UiModeManager → TELEVISION) for the 10-foot UI.
 *  - "fibr/files" — "pickConfig": the system file picker (no pub plugin),
 *    returns the chosen file's text or null when cancelled.
 */
class MainActivity : FlutterActivity() {

    private var initialLink: String? = null
    private var linkSink: EventChannel.EventSink? = null
    private var pickResult: MethodChannel.Result? = null

    private companion object {
        const val PICK_CONFIG = 4711
        const val MAX_CONFIG_BYTES = 256 * 1024
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        initialLink = extractLink(intent)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        extractLink(intent)?.let { link -> linkSink?.success(link) }
    }

    private fun extractLink(intent: Intent?): String? {
        if (intent == null) return null
        return when (intent.action) {
            Intent.ACTION_VIEW -> {
                val data = intent.data ?: return null
                when (data.scheme) {
                    "content", "file" -> readText(data)
                    else -> intent.dataString
                }
            }
            Intent.ACTION_SEND -> {
                intent.getStringExtra(Intent.EXTRA_TEXT)?.takeIf { it.isNotBlank() }
                    ?: streamExtra(intent)?.let { readText(it) }
            }
            else -> null
        }
    }

    @Suppress("DEPRECATION")
    private fun streamExtra(intent: Intent): Uri? =
        if (Build.VERSION.SDK_INT >= 33) {
            intent.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java)
        } else {
            intent.getParcelableExtra(Intent.EXTRA_STREAM) as? Uri
        }

    /** Text of a small file behind a content:// / file:// uri, or null. */
    private fun readText(uri: Uri): String? = try {
        contentResolver.openInputStream(uri)?.use { input ->
            val bytes = input.readBytes()
            if (bytes.size > MAX_CONFIG_BYTES) null else String(bytes, Charsets.UTF_8)
        }
    } catch (e: Exception) {
        null
    }

    // FlutterActivity is a plain android.app.Activity: results come here.
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode != PICK_CONFIG) return
        val result = pickResult ?: return
        pickResult = null
        val uri = data?.data
        if (resultCode != RESULT_OK || uri == null) {
            result.success(null)
            return
        }
        result.success(readText(uri))
    }

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        val messenger = flutterEngine.dartExecutor.binaryMessenger

        MethodChannel(messenger, "fibr/deeplink").setMethodCallHandler { call, result ->
            when (call.method) {
                "getInitialLink" -> result.success(initialLink)
                else -> result.notImplemented()
            }
        }

        EventChannel(messenger, "fibr/deeplink/events").setStreamHandler(
            object : EventChannel.StreamHandler {
                override fun onListen(arguments: Any?, events: EventChannel.EventSink?) {
                    linkSink = events
                }

                override fun onCancel(arguments: Any?) {
                    linkSink = null
                }
            }
        )

        MethodChannel(messenger, "fibr/files").setMethodCallHandler { call, result ->
            when (call.method) {
                "pickConfig" -> {
                    pickResult?.success(null) // a previous picker never returned
                    pickResult = result
                    val pick = Intent(Intent.ACTION_OPEN_DOCUMENT).apply {
                        addCategory(Intent.CATEGORY_OPENABLE)
                        type = "*/*" // .conf has no registered MIME type
                    }
                    try {
                        @Suppress("DEPRECATION")
                        startActivityForResult(pick, PICK_CONFIG)
                    } catch (e: Exception) {
                        pickResult = null
                        result.error("no_picker", e.message, null)
                    }
                }
                else -> result.notImplemented()
            }
        }

        MethodChannel(messenger, "fibr/apps").setMethodCallHandler { call, result ->
            when (call.method) {
                "list" -> result.success(installedApps())
                "isTv" -> result.success(isTelevision())
                else -> result.notImplemented()
            }
        }
    }

    /** Enabled, launchable apps with a display label; system apps flagged. */
    private fun installedApps(): List<Map<String, Any>> {
        val pm = packageManager
        return pm.getInstalledPackages(0).mapNotNull { pi ->
            val app = pi.applicationInfo ?: return@mapNotNull null
            if (!app.enabled) return@mapNotNull null
            val label = pm.getApplicationLabel(app)?.toString() ?: app.packageName
            mapOf(
                "package" to app.packageName,
                "label" to label,
                "system" to ((app.flags and ApplicationInfo.FLAG_SYSTEM) != 0),
            )
        }
    }

    /** Android TV / leanback device: UiModeManager says the current mode is
     *  a television (emulator included; phones and tablets report NORMAL). */
    private fun isTelevision(): Boolean {
        val uiModeManager = getSystemService(UI_MODE_SERVICE) as UiModeManager
        return uiModeManager.currentModeType == Configuration.UI_MODE_TYPE_TELEVISION
    }
}
