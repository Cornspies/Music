package com.music.app

import platform.UIKit.UIActivityViewController
import platform.UIKit.popoverPresentationController

internal actual fun shareText(text: String) {
    val top = topViewController() ?: return
    val sheet = UIActivityViewController(activityItems = listOf(text), applicationActivities = null)
    sheet.popoverPresentationController?.sourceView = top.view
    top.presentViewController(sheet, animated = true, completion = null)
}
