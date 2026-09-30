package com.music.app

import platform.UIKit.UIActivityViewController
import platform.UIKit.popoverPresentationController

internal actual fun shareText(text: String) {
    val top = topViewController() ?: return
    val sheet = UIActivityViewController(activityItems = listOf(text), applicationActivities = null)
    // An iPad shows the sheet as a popover, which needs somewhere to point.
    sheet.popoverPresentationController?.sourceView = top.view
    top.presentViewController(sheet, animated = true, completion = null)
}
