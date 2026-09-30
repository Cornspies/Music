"use client"

import { useEffect, useState } from "react"
import { AnimatePresence, motion } from "framer-motion"
import {
  ChevronDown,
  Heart,
  ListMusic,
  MicVocal,
  Pause,
  Play,
  Repeat,
  Repeat1,
  Shuffle,
  SkipBack,
  SkipForward,
  Users,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { formatDuration, hashGradient } from "@/lib/format"
import { coverUrl } from "@/lib/api"
import { usePlayerStore } from "@/stores/player"
import { useLibraryStore } from "@/stores/library"
import { useUIStore } from "@/stores/ui"
import { useListenStore } from "@/stores/listen"
import { CoverArt } from "@/components/music/CoverArt"
import { SleepTimerMenu } from "@/components/music/SleepTimerMenu"
import { Button } from "@/components/ui/button"
import { Slider } from "@/components/ui/slider"
import { WaveformSeek } from "@/components/music/WaveformSeek"
import { LyricsPanel } from "@/components/music/LyricsPanel"
import { lyricsGradient } from "@/lib/color"

const LYRICS_PREF = "np-lyrics"

function readLyricsPref(): boolean {
  try {
    return localStorage.getItem(LYRICS_PREF) === "1"
  } catch {
    return false
  }
}

const SEEK_CLASS =
  "[&_[data-slot=slider-track]]:h-1.5 [&_[data-slot=slider-track]]:bg-white/15 [&_[data-slot=slider-range]]:bg-emerald-500 [&_[data-slot=slider-range]]:shadow-[0_0_14px_rgba(16,185,129,0.5)] [&_[data-slot=slider-thumb]]:size-3.5 [&_[data-slot=slider-thumb]]:border-0 [&_[data-slot=slider-thumb]]:bg-white [&_[data-slot=slider-thumb]]:shadow-lg [&_[data-slot=slider-thumb]]:opacity-0 group/seek:[&_[data-slot=slider-thumb]]:opacity-100 [&_[data-slot=slider-thumb]]:transition-opacity"

function GhostControl({
  label,
  active,
  children,
  className,
  ...props
}: React.ComponentProps<typeof Button> & { label: string; active?: boolean }) {
  return (
    <Button
      variant="ghost"
      size="icon"
      aria-label={label}
      title={label}
      className={cn(
        "size-11 rounded-full text-zinc-300 hover:bg-white/10 hover:text-white",
        active && "text-emerald-400 hover:text-emerald-300",
        className
      )}
      {...props}
    >
      {children}
    </Button>
  )
}

export function NowPlayingView() {
  const open = useUIStore((s) => s.nowPlayingOpen)
  const setOpen = useUIStore((s) => s.setNowPlayingOpen)

  const currentTrack = usePlayerStore((s) => s.currentTrack)
  const isPlaying = usePlayerStore((s) => s.isPlaying)
  const position = usePlayerStore((s) => s.position)
  const duration = usePlayerStore((s) => s.duration)
  const shuffle = usePlayerStore((s) => s.shuffle)
  const repeat = usePlayerStore((s) => s.repeat)
  const queue = usePlayerStore((s) => s.queue)
  const queueIndex = usePlayerStore((s) => s.queueIndex)
  const listeners = useListenStore((s) => s.session?.members.length ?? 0)
  const canSkip = useListenStore((s) => s.session?.canControl ?? true)

  const [seekPreview, setSeekPreview] = useState<number | null>(null)
  const [lyricsOpen, setLyricsOpen] = useState(readLyricsPref)
  const toggleLyrics = () => {
    const next = !lyricsOpen
    setLyricsOpen(next)
    try {
      localStorage.setItem(LYRICS_PREF, next ? "1" : "0")
    } catch {}
  }

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation()
        setOpen(false)
      }
    }
    window.addEventListener("keydown", onKey, true)
    return () => window.removeEventListener("keydown", onKey, true)
  }, [open, setOpen])

  const effectiveDuration = duration > 0 ? duration : (currentTrack?.duration ?? 0)
  const displayPosition = Math.min(seekPreview ?? position, effectiveDuration || Infinity)
  const upNext = queue[queueIndex + 1] ?? null
  const coverSrc = currentTrack?.hasCover ? coverUrl({ id: currentTrack.id }, 128) : null

  return (
    <AnimatePresence>
      {open && currentTrack && (
        <motion.div
          key="now-playing"
          role="dialog"
          aria-modal="true"
          aria-label="Now playing"
          data-shortcuts-allowed=""
          initial={{ y: "100%", opacity: 0.6 }}
          animate={{ y: 0, opacity: 1 }}
          exit={{ y: "100%", opacity: 0.6 }}
          transition={{ type: "spring", damping: 32, stiffness: 340 }}
          className="fixed inset-0 z-50 flex flex-col overflow-hidden bg-zinc-950"
        >
          {lyricsOpen ? (
            <div
              aria-hidden="true"
              className="absolute inset-0 transition-[background] duration-500"
              style={{ background: lyricsGradient(currentTrack.coverColor) }}
            />
          ) : coverSrc ? (
            <>
              <img
                src={coverSrc}
                alt=""
                aria-hidden="true"
                className="absolute inset-0 size-full scale-[1.6] object-cover opacity-45 blur-3xl select-none"
              />
              <div aria-hidden="true" className="absolute inset-0 bg-zinc-950/55" />
              <div
                aria-hidden="true"
                className="absolute inset-0 bg-[radial-gradient(120%_90%_at_50%_0%,rgba(255,255,255,0.05),transparent_60%)]"
              />
            </>
          ) : (
            <>
              <div
                aria-hidden="true"
                className={cn("absolute inset-0 bg-linear-to-br", hashGradient(currentTrack?.id ?? "music"))}
              />
              <div
                aria-hidden="true"
                className="absolute -top-32 left-1/2 size-112 -translate-x-1/2 rounded-full bg-emerald-500/10 blur-3xl"
              />
            </>
          )}

          <div className="relative z-10 flex items-center justify-between px-3 pt-3 md:px-6 md:pt-5">
            <Button
              variant="ghost"
              size="icon"
              aria-label="Close now playing"
              onClick={() => setOpen(false)}
              className="size-10 rounded-full text-zinc-300 hover:bg-white/10 hover:text-white"
            >
              <ChevronDown className="size-5" />
            </Button>
            <div className="text-center">
              <p className="text-[10px] font-semibold uppercase tracking-[0.2em] text-zinc-400">
                Now playing
              </p>
              <p className={cn("text-[11px]", listeners ? "text-emerald-400" : "text-zinc-500")}>
                {listeners
                  ? `Listening together · ${listeners} ${listeners === 1 ? "person" : "people"}`
                  : queue.length > 1
                    ? `From queue · ${queueIndex + 1} of ${queue.length}`
                    : "Single track"}
              </p>
            </div>
            <div className="flex items-center gap-1">
              <Button
                variant="ghost"
                size="icon"
                aria-label={lyricsOpen ? "Hide lyrics" : "Show lyrics"}
                aria-pressed={lyricsOpen}
                onClick={toggleLyrics}
                className={cn(
                  "size-10 rounded-full hover:bg-white/10 hover:text-white",
                  lyricsOpen ? "text-emerald-400" : "text-zinc-300"
                )}
              >
                <MicVocal className="size-5" />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                aria-label="Listen together"
                onClick={() => useListenStore.getState().setDialogOpen(true)}
                className={cn(
                  "size-10 rounded-full hover:bg-white/10 hover:text-white",
                  listeners ? "text-emerald-400" : "text-zinc-300"
                )}
              >
                <Users className="size-5" />
              </Button>
              <SleepTimerMenu />
            </div>
          </div>

          <div
            className={cn(
              "relative z-10 flex min-h-0 flex-1 flex-col items-center px-6 pb-6 md:px-10",
              lyricsOpen ? "gap-3 overflow-hidden md:gap-4" : "justify-center gap-5 overflow-y-auto md:gap-8"
            )}
          >
            {lyricsOpen ? (
              <>
                <div className="flex w-full max-w-4xl items-center gap-3 pt-2">
                  <span className="w-12 shrink-0 md:w-14">
                    <CoverArt trackId={currentTrack.id} title={currentTrack.title} hasCover={currentTrack.hasCover} />
                  </span>
                  <span className="min-w-0">
                    <span className="block truncate text-base font-bold text-white md:text-lg">{currentTrack.title}</span>
                    <span className="block truncate text-sm text-white/60">{currentTrack.artist || "Unknown artist"}</span>
                  </span>
                </div>
                <LyricsPanel
                  trackId={currentTrack.id}
                  position={displayPosition}
                  onSeek={(v) => usePlayerStore.getState().seek(v)}
                  size="full"
                  className="min-h-0 w-full max-w-4xl flex-1"
                />
              </>
            ) : (
              <motion.div
                layout
                initial={{ scale: 0.96, opacity: 0 }}
                animate={{ scale: 1, opacity: 1 }}
                transition={{ delay: 0.08, duration: 0.25, ease: "easeOut" }}
                className={cn(
                  "relative w-full max-w-[min(64vw,20rem)] shrink-0 md:max-w-[min(38vw,22rem)]",
                  isPlaying && "nowplaying-glow"
                )}
              >
                {isPlaying && (
                  <span aria-hidden="true" className="eq-pulse absolute inset-0 rounded-2xl" />
                )}
                <CoverArt
                  trackId={currentTrack.id}
                  title={currentTrack.title}
                  hasCover={currentTrack.hasCover}
                  size="full"
                  className="rounded-2xl shadow-2xl shadow-black/60"
                />
              </motion.div>
            )}

            {!lyricsOpen && (
            <div className="w-full max-w-xl text-center">
              <h1 className="truncate text-2xl font-bold tracking-tight text-white md:text-3xl">
                {currentTrack.title}
              </h1>
              <p className="mt-1.5 truncate text-sm text-zinc-400 md:text-base">
                {currentTrack.artist || "Unknown artist"}
                {currentTrack.album ? ` · ${currentTrack.album}` : ""}
                {currentTrack.year ? ` · ${currentTrack.year}` : ""}
              </p>
              {currentTrack.genre ? (
                <span className="mt-2.5 inline-block rounded-full border border-white/10 bg-white/5 px-2.5 py-0.5 text-[11px] text-zinc-300">
                  {currentTrack.genre}
                </span>
              ) : null}
            </div>
            )}

            <div className="group/seek w-full max-w-xl">
              <WaveformSeek
                track={currentTrack}
                position={displayPosition}
                duration={effectiveDuration}
                onPreview={setSeekPreview}
                onSeek={(v) => usePlayerStore.getState().seek(v)}
                height={48}
                className="w-full"
                fallback={
                  <Slider
                    aria-label="Seek"
                    value={[Math.min(displayPosition, effectiveDuration || 1)]}
                    max={effectiveDuration || 1}
                    step={1}
                    onValueChange={([v]) => setSeekPreview(v)}
                    onValueCommit={([v]) => {
                      usePlayerStore.getState().seek(v)
                      setSeekPreview(null)
                    }}
                    className={cn(SEEK_CLASS, "w-full")}
                  />
                }
              />
              <div className="mt-1.5 flex justify-between text-[11px] tabular-nums text-zinc-500">
                <span>{formatDuration(displayPosition)}</span>
                <span>{formatDuration(effectiveDuration)}</span>
              </div>
            </div>

            <div className="flex items-center gap-2 md:gap-4">
              <GhostControl
                label="Shuffle"
                active={shuffle && !listeners}
                disabled={listeners > 0}
                onClick={() => usePlayerStore.getState().toggleShuffle()}
                className="hidden sm:flex"
              >
                <Shuffle className="size-4.5" />
              </GhostControl>
              <GhostControl label="Previous track" disabled={!canSkip} onClick={() => usePlayerStore.getState().prev()}>
                <SkipBack className="size-6 fill-current" />
              </GhostControl>
              <Button
                variant="ghost"
                size="icon"
                aria-label={isPlaying ? "Pause" : "Play"}
                onClick={() => usePlayerStore.getState().togglePlay()}
                className="size-14 rounded-full bg-emerald-500 text-zinc-950 shadow-xl shadow-emerald-950/50 transition-transform hover:scale-105 hover:bg-emerald-400 active:scale-95 md:size-16"
              >
                {isPlaying ? (
                  <Pause className="size-6 fill-current" />
                ) : (
                  <Play className="size-6 translate-x-0.5 fill-current" />
                )}
              </Button>
              <GhostControl label="Next track" disabled={!canSkip} onClick={() => usePlayerStore.getState().next()}>
                <SkipForward className="size-6 fill-current" />
              </GhostControl>
              <GhostControl
                label={repeat === "one" ? "Repeat one track" : repeat === "all" ? "Repeat all" : "Repeat off"}
                active={repeat !== "off" && !listeners}
                disabled={listeners > 0}
                onClick={() => usePlayerStore.getState().cycleRepeat()}
                className="hidden sm:flex"
              >
                {repeat === "one" ? (
                  <Repeat1 className="size-4.5" />
                ) : (
                  <Repeat className="size-4.5" />
                )}
              </GhostControl>
            </div>

            <div className="flex items-center gap-3">
              <button
                type="button"
                onClick={() => useLibraryStore.getState().toggleFavorite(currentTrack.id)}
                aria-label={
                  currentTrack.isFavorite ? "Remove from favorites" : "Add to favorites"
                }
                aria-pressed={currentTrack.isFavorite}
                className={cn(
                  "flex h-10 items-center gap-2 rounded-full border border-white/10 bg-white/5 px-4 text-xs font-medium backdrop-blur transition-colors hover:bg-white/10",
                  currentTrack.isFavorite ? "text-emerald-400" : "text-zinc-300"
                )}
              >
                <Heart className={cn("size-4", currentTrack.isFavorite && "fill-current")} />
                {currentTrack.isFavorite ? "Favorited" : "Favorite"}
              </button>
              <Button
                variant="ghost"
                size="sm"
                aria-label="Open queue"
                onClick={() => {
                  setOpen(false)
                  useUIStore.getState().setQueueOpen(true)
                }}
                className="h-10 rounded-full border border-white/10 bg-white/5 px-4 text-xs font-medium text-zinc-300 backdrop-blur hover:bg-white/10 hover:text-white"
              >
                <ListMusic className="size-4" /> Queue
              </Button>
            </div>

            {upNext && !lyricsOpen && (
              <button
                type="button"
                onClick={() => usePlayerStore.getState().next()}
                className="group flex w-full max-w-xl items-center gap-3 rounded-xl border border-white/10 bg-white/5 p-2.5 text-left shadow-lg shadow-black/30 backdrop-blur-xl transition-colors hover:border-white/20 hover:bg-white/10"
              >
                <span className="w-10 shrink-0">
                  <CoverArt trackId={upNext.id} title={upNext.title} hasCover={upNext.hasCover} />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
                    Up next
                  </span>
                  <span className="block truncate text-sm text-zinc-100">{upNext.title}</span>
                  <span className="block truncate text-xs text-zinc-400">
                    {upNext.artist || "Unknown artist"}
                  </span>
                </span>
                <SkipForward className="mr-2 size-4 shrink-0 text-zinc-400 transition-colors group-hover:text-emerald-400" />
              </button>
            )}
          </div>

          <div aria-hidden="true" className="relative z-10 h-[env(safe-area-inset-bottom)]" />
        </motion.div>
      )}
    </AnimatePresence>
  )
}
