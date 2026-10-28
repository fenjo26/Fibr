package com.fibr.app

import android.app.UiModeManager
import android.content.Intent
import android.content.pm.ApplicationInfo
import android.content.res.Configuration
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
 *  - "fibr/apps" — installed app list for the split-tunneling screen,
 *    plus "isTv" (UiModeManager → TELEVISION) for the 10-foot UI.
 */
class MainActivity : FlutterActivity() {

    private var initialLink: String? = null
    private var linkSink: EventChannel.EventSink? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        initialLink = extractLink(intent)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        extractLink(intent)?.let { link -> linkSink?.success(link) }
    }

    private fun extractLink(intent: Intent?): String? {
        if (intent?.action != Intent.ACTION_VIEW) return null
        return intent.dataString
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
