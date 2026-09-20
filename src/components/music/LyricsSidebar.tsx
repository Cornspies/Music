"use client"

import { useEffect } from "react"
import { X } from "lucide-react"
import { lyricsGradient } from "@/lib/color"
import { usePlayerStore } from "@/stores/player"
import { readLyricsPanelPref, useUIStore } from "@/stores/ui"
import { Button } from "@/components/ui/button"
import { CoverArt } from "@/components/music/CoverArt"
import { LyricsPanel } from "@/components/music/LyricsPanel"

/**
 * Lyrics docked to the right of the main area, the way Spotify's desktop app
 * does it: painted in the cover's colour, following the track as it plays,
 * without covering anything. Opened from the mic button in the player bar;
 * the choice sticks per browser. Desktop only — phones use the full-screen
 * player for this.
 */
export function LyricsSidebar() {
  const open = useUIStore((s) => s.lyricsPanelOpen)
  const track = usePlayerStore((s) => s.currentTrack)
  const position = usePlayerStore((s) => s.position)

  // Restore last time's choice once on the client (see readLyricsPanelPref).
  useEffect(() => {
    if (readLyricsPanelPref()) useUIStore.getState().setLyricsPanelOpen(true)
  }, [])

  if (!open) return null

  return (
    <aside
      aria-label="Lyrics"
      className="hidden w-80 shrink-0 flex-col border-l border-black/30 text-white transition-[background] duration-500 md:flex lg:w-96"
      style={{ background: lyricsGradient(track?.coverColor) }}
    >
      <div className="flex items-center gap-3 px-4 pt-4 pb-2">
        {track ? (
          <>
            <span className="w-10 shrink-0">
              <CoverArt trackId={track.id} title={track.title} hasCover={track.hasCover} />
            </span>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm font-bold">{track.title}</span>
              <span className="block truncate text-xs text-white/60">{track.artist || "Unknown artist"}</span>
            </span>
          </>
        ) : (
          <span className="flex-1 text-sm font-semibold">Lyrics</span>
        )}
        <Button
          variant="ghost"
          size="icon"
          aria-label="Hide lyrics"
          onClick={() => useUIStore.getState().setLyricsPanelOpen(false)}
          className="size-8 rounded-full text-white/70 hover:bg-white/10 hover:text-white"
        >
          <X className="size-4" />
        </Button>
      </div>
      {track ? (
        <LyricsPanel
          trackId={track.id}
          position={position}
          onSeek={(v) => usePlayerStore.getState().seek(v)}
          size="side"
          className="min-h-0 flex-1 px-2 pb-3"
        />
      ) : (
        <p className="px-4 py-10 text-center text-sm text-white/60">Play something to see its lyrics.</p>
      )}
    </aside>
  )
}
