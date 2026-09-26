package com.music.app

import androidx.compose.runtime.Composable
import kotlinx.cinterop.ExperimentalForeignApi
import kotlinx.cinterop.addressOf
import kotlinx.cinterop.usePinned
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import platform.AVFoundation.AVURLAsset
import platform.CoreMedia.CMTimeGetSeconds
import platform.Foundation.NSData
import platform.Foundation.NSURL
import platform.Foundation.dataWithContentsOfURL
import platform.UIKit.UIApplication
import platform.UIKit.UIDocumentPickerDelegateProtocol
import platform.UIKit.UIDocumentPickerMode
import platform.UIKit.UIDocumentPickerViewController
import platform.UIKit.UIImage
import platform.UIKit.UIImageJPEGRepresentation
import platform.UIKit.UIImagePickerController
import platform.UIKit.UIImagePickerControllerDelegateProtocol
import platform.UIKit.UIImagePickerControllerOriginalImage
import platform.UIKit.UIImagePickerControllerSourceType
import platform.UIKit.UINavigationControllerDelegateProtocol
import platform.UIKit.UIViewController
import platform.UIKit.UIWindow
import platform.darwin.NSObject
import platform.posix.memcpy

internal actual val canUploadFromDevice: Boolean = true

private var activeDocumentDelegate: NSObject? = null
private var activeImageDelegate: NSObject? = null

private fun topViewController(): UIViewController? {
    val window = UIApplication.sharedApplication.keyWindow
        ?: UIApplication.sharedApplication.windows.firstOrNull { (it as? UIWindow)?.isKeyWindow() == true } as? UIWindow
    var top = window?.rootViewController ?: return null
    while (top.presentedViewController != null) top = top.presentedViewController!!
    return top
}

@Composable
internal actual fun rememberAudioPickerLauncher(
    onPicked: (List<PickedAudio>) -> Unit,
): () -> Unit = {
    val presenter = topViewController()
    if (presenter != null) {
        val delegate = object : NSObject(), UIDocumentPickerDelegateProtocol {
            override fun documentPicker(controller: UIDocumentPickerViewController, didPickDocumentsAtURLs: List<*>) {
                activeDocumentDelegate = null
                val picked = didPickDocumentsAtURLs.filterIsInstance<NSURL>().map { url ->
                    PickedAudio(
                        name = url.lastPathComponent ?: "upload.mp3",
                        sizeBytes = fileSize(url),
                        durationSec = audioDuration(url),
                        handle = url,
                    )
                }
                if (picked.isNotEmpty()) onPicked(picked)
            }

            override fun documentPickerWasCancelled(controller: UIDocumentPickerViewController) {
                activeDocumentDelegate = null
            }
        }
        activeDocumentDelegate = delegate
        val picker = UIDocumentPickerViewController(
            documentTypes = listOf("public.audio"),
            inMode = UIDocumentPickerMode.UIDocumentPickerModeImport,
        )
        picker.allowsMultipleSelection = true
        picker.delegate = delegate
        presenter.presentViewController(picker, animated = true, completion = null)
    }
}

@OptIn(ExperimentalForeignApi::class)
private fun fileSize(url: NSURL): Long =
    runCatching {
        val attrs = platform.Foundation.NSFileManager.defaultManager.attributesOfItemAtPath(url.path ?: "", error = null)
        (attrs?.get(platform.Foundation.NSFileSize) as? platform.Foundation.NSNumber)?.longValue ?: 0L
    }.getOrDefault(0L)

@OptIn(ExperimentalForeignApi::class)
private fun audioDuration(url: NSURL): Double? =
    runCatching {
        val seconds = CMTimeGetSeconds(AVURLAsset(uRL = url, options = null).duration)
        if (seconds.isNaN() || seconds <= 0.0) null else seconds
    }.getOrNull()

internal actual suspend fun PickedAudio.readBytes(): ByteArray {
    val url = handle as? NSURL ?: throw IllegalStateException("missing file reference")
    return withContext(Dispatchers.Default) {
        val data = NSData.dataWithContentsOfURL(url) ?: throw IllegalStateException("cannot read file")
        data.toByteArray()
    }
}

@OptIn(ExperimentalForeignApi::class)
private fun NSData.toByteArray(): ByteArray {
    val size = length.toInt()
    if (size == 0) return ByteArray(0)
    val out = ByteArray(size)
    out.usePinned { pinned -> memcpy(pinned.addressOf(0), bytes, length) }
    return out
}

internal actual val canPickImageFromDevice: Boolean = true

internal actual fun appVersionName(): String? = null

internal actual val unplayableMimeTypes: Set<String> = setOf(
    "audio/ogg",
    "application/ogg",
    "audio/opus",
    "audio/webm",
    "audio/x-ms-wma",
)

@Composable
internal actual fun rememberImagePickerLauncher(
    onPicked: (PickedImage) -> Unit,
): () -> Unit = {
    val presenter = topViewController()
    if (presenter != null) {
        val delegate = object : NSObject(), UIImagePickerControllerDelegateProtocol, UINavigationControllerDelegateProtocol {
            override fun imagePickerController(picker: UIImagePickerController, didFinishPickingMediaWithInfo: Map<Any?, *>) {
                activeImageDelegate = null
                picker.dismissViewControllerAnimated(true, completion = null)
                val image = didFinishPickingMediaWithInfo[UIImagePickerControllerOriginalImage] as? UIImage ?: return
                onPicked(PickedImage(name = "avatar.jpg", mimeType = "image/jpeg", handle = image))
            }

            override fun imagePickerControllerDidCancel(picker: UIImagePickerController) {
                activeImageDelegate = null
                picker.dismissViewControllerAnimated(true, completion = null)
            }
        }
        activeImageDelegate = delegate
        val picker = UIImagePickerController()
        picker.sourceType = UIImagePickerControllerSourceType.UIImagePickerControllerSourceTypePhotoLibrary
        picker.delegate = delegate
        presenter.presentViewController(picker, animated = true, completion = null)
    }
}

internal actual suspend fun PickedImage.readBytes(): ByteArray {
    val image = handle as? UIImage ?: throw IllegalStateException("missing image reference")
    val data = UIImageJPEGRepresentation(image, 0.92) ?: throw IllegalStateException("cannot encode image")
    return data.toByteArray()
}
