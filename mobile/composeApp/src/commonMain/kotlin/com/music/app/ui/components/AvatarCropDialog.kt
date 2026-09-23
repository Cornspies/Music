package com.music.app.ui.components

import androidx.compose.foundation.Image
import androidx.compose.foundation.gestures.detectTransformGestures
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Slider
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.graphicsLayer
import com.music.app.decodeImageBitmap
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.unit.dp
import kotlin.math.max
import kotlin.math.min

data class AvatarCrop(val x: Float, val y: Float, val size: Float)

@Composable
fun AvatarCropDialog(
    bytes: ByteArray,
    busy: Boolean,
    onCancel: () -> Unit,
    onConfirm: (AvatarCrop) -> Unit,
) {
    val bitmap: ImageBitmap? = remember(bytes) { decodeImageBitmap(bytes) }

    var frame by remember { mutableStateOf(0f) }
    var zoom by remember(bytes) { mutableStateOf(1f) }
    var offset by remember(bytes) { mutableStateOf(Offset.Zero) }

    fun baseScale(): Float = if (bitmap != null && frame > 0f) {
        frame / min(bitmap.width, bitmap.height).toFloat()
    } else {
        1f
    }

    fun drawnW(z: Float = zoom): Float = (bitmap?.width ?: 0) * baseScale() * z
    fun drawnH(z: Float = zoom): Float = (bitmap?.height ?: 0) * baseScale() * z

    fun clamp(next: Offset, z: Float = zoom) = Offset(
        x = min(0f, max(frame - drawnW(z), next.x)),
        y = min(0f, max(frame - drawnH(z), next.y)),
    )

    fun zoomAround(pivot: Offset, target: Float) {
        val z = target.coerceIn(1f, 5f)
        val ratio = z / zoom
        offset = clamp(pivot - (pivot - offset) * ratio, z)
        zoom = z
    }

    AlertDialog(
        onDismissRequest = { if (!busy) onCancel() },
        title = { Text("Position your picture") },
        text = {
            Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally) {
                if (bitmap == null) {
                    Text(
                        "That file could not be read as an image.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.error,
                    )
                } else {
                    Box(
                        Modifier
                            .padding(top = 12.dp)
                            .size(240.dp)
                            .clip(CircleShape)
                            .onSizeChanged { sz ->
                                if (sz.width.toFloat() != frame) {
                                    frame = sz.width.toFloat()
                                    val b = frame / min(bitmap.width, bitmap.height).toFloat()
                                    offset = Offset(
                                        (frame - bitmap.width * b) / 2f,
                                        (frame - bitmap.height * b) / 2f,
                                    )
                                }
                            }
                            .pointerInput(bytes) {
                                detectTransformGestures { centroid, pan, gestureZoom, _ ->
                                    zoomAround(centroid, zoom * gestureZoom)
                                    offset = clamp(offset + pan)
                                }
                            },
                    ) {
                        Image(
                            bitmap = bitmap,
                            contentDescription = "Your new avatar",
                            contentScale = ContentScale.FillBounds,
                            modifier = Modifier
                                .size(240.dp)
                                .graphicsLayer {
                                    transformOrigin =
                                        androidx.compose.ui.graphics.TransformOrigin(0f, 0f)
                                    scaleX = if (frame > 0f) drawnW() / frame else 1f
                                    scaleY = if (frame > 0f) drawnH() / frame else 1f
                                    translationX = offset.x
                                    translationY = offset.y
                                },
                        )
                    }
                    Slider(
                        value = zoom,
                        onValueChange = { zoomAround(Offset(frame / 2f, frame / 2f), it) },
                        valueRange = 1f..5f,
                        modifier = Modifier
                            .fillMaxWidth()
                            .height(40.dp),
                    )
                }
            }
        },
        confirmButton = {
            TextButton(
                enabled = !busy && bitmap != null && frame > 0f,
                onClick = {
                    val b = bitmap ?: return@TextButton
                    val scale = baseScale() * zoom
                    val shorter = min(b.width, b.height).toFloat()
                    onConfirm(
                        AvatarCrop(
                            x = -offset.x / scale / b.width,
                            y = -offset.y / scale / b.height,
                            size = frame / scale / shorter,
                        ),
                    )
                },
            ) {
                if (busy) {
                    CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                } else {
                    Text("Save picture")
                }
            }
        },
        dismissButton = {
            TextButton(onClick = onCancel, enabled = !busy) { Text("Cancel") }
        },
    )
}
