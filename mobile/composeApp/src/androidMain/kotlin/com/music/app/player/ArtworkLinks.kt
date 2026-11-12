package com.music.app.player

import android.content.Context
import android.graphics.Bitmap
import android.net.Uri
import androidx.annotation.OptIn
import androidx.media3.common.util.BitmapLoader
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DataSourceBitmapLoader
import androidx.media3.session.CacheBitmapLoader
import com.google.common.util.concurrent.ListenableFuture

internal object ArtworkLinks {

    private const val NOTIFICATION_SIZE = "512"
    private const val REMEMBERED = 8

    private val privateUrls = object : LinkedHashMap<Uri, Uri>() {
        override fun removeEldestEntry(eldest: MutableMap.MutableEntry<Uri, Uri>?): Boolean = size > REMEMBERED
    }

    @Synchronized
    fun publish(url: String): Uri {
        val full = Uri.parse(url)
        if (full.scheme != "http" && full.scheme != "https") return full
        val sized = if (full.getQueryParameter("size") == null) {
            full.buildUpon().appendQueryParameter("size", NOTIFICATION_SIZE).build()
        } else {
            full
        }
        val open = sized.buildUpon().clearQuery().apply {
            sized.queryParameterNames.filter { it != "token" }.forEach { name ->
                sized.getQueryParameters(name).forEach { appendQueryParameter(name, it) }
            }
        }.build()
        privateUrls[open] = sized
        return open
    }

    @Synchronized
    fun resolve(open: Uri): Uri = privateUrls[open] ?: open
}

@OptIn(UnstableApi::class)
internal class PrivateArtworkLoader(context: Context) : BitmapLoader {

    private val loader = CacheBitmapLoader(DataSourceBitmapLoader.Builder(context).build())

    override fun supportsMimeType(mimeType: String): Boolean = loader.supportsMimeType(mimeType)

    override fun decodeBitmap(data: ByteArray): ListenableFuture<Bitmap> = loader.decodeBitmap(data)

    override fun loadBitmap(uri: Uri): ListenableFuture<Bitmap> = loader.loadBitmap(ArtworkLinks.resolve(uri))
}
