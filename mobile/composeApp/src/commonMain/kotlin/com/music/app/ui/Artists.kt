package com.music.app.ui

private val artistSplit = Regex("""(?i)\s*(?:,|;|&|\s(?:feat\.?|ft\.?|featuring)\s)\s*""")
private val nonWord = Regex("""[^\p{L}\p{N}]+""")

fun artistKey(name: String): String = name.lowercase().replace(nonWord, " ").trim()

fun splitArtists(credit: String): List<String> {
    val clean = credit
        .replace("&amp;", "&")
        .replace("&quot;", "\"")
        .replace("&#39;", "'")
        .trim()
    if (clean.isEmpty()) return emptyList()
    val seen = mutableSetOf<String>()
    val out = clean.split(artistSplit)
        .map { it.trim() }
        .filter { part -> artistKey(part).let { it.isNotEmpty() && seen.add(it) } }
    return out.ifEmpty { listOf(clean) }
}

fun hasArtist(credit: String, name: String): Boolean {
    val want = artistKey(name)
    return want.isNotEmpty() && splitArtists(credit).any { artistKey(it) == want }
}
