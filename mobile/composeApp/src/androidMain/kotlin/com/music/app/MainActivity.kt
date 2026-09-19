package com.music.app

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.content.ContextCompat

/** Application context, set as soon as the activity exists. */
internal lateinit var appContext: Context

class MainActivity : ComponentActivity() {

    // Android 13+ gates the media notification behind a runtime permission;
    // without it playback still works, but the lock-screen controls and the
    // notification do not appear. Asked once, on first launch.
    private val requestNotifications =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) { /* best-effort */ }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        appContext = applicationContext
        enableEdgeToEdge()
        DeepLinks.handle(intent?.dataString)
        setContent { App() }
        askForNotifications()
    }

    override fun onNewIntent(intent: android.content.Intent) {
        super.onNewIntent(intent)
        DeepLinks.handle(intent.dataString)
    }

    private fun askForNotifications() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return
        val granted = ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) ==
            PackageManager.PERMISSION_GRANTED
        if (!granted) requestNotifications.launch(Manifest.permission.POST_NOTIFICATIONS)
    }
}
