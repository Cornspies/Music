"use client"

import { useState } from "react"
import {
  ChevronUp,
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
  SlidersHorizontal,
  Volume1,
  Volume2,
  VolumeX,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { formatDuration } from "@/lib/format"
import { usePlayerStore } from "@/stores/player"
import { useLibraryStore } from "@/stores/library"
import { useUIStore } from "@/stores/ui"
import { CoverArt } from "@/components/music/CoverArt"
import { AddToPlaylistButton } from "@/components/music/AddToPlaylistButton"
import { SleepTimerMenu } from "@/components/music/SleepTimerMenu"
import { TrackMenu } from "@/components/music/TrackMenu"
import { WaveformSeek } from "@/components/music/WaveformSeek"
import { Button } from "@/components/ui/button"
import { Slider } from "@/components/ui/slider"

const SEEK_SLIDER_CLASS =
  "[&_[data-slot=slider-track]]:h-1 [&_[data-slot=slider-track]]:bg-zinc-700/70 [&_[data-slot=slider-range]]:bg-emerald-500 [&_[data-slot=slider-range]]:shadow-[0_0_10px_rgba(16,185,129,0.45)] [&_[data-slot=slider-thumb]]:size-3 [&_[data-slot=slider-thumb]]:border-0 [&_[data-slot=slider-thumb]]:bg-white [&_[data-slot=slider-thumb]]:shadow-md [&_[data-slot=slider-thumb]]:opacity-0 group-hover/seek:[&_[data-slot=slider-thumb]]:opacity-100 [&_[data-slot=slider-thumb]]:transition-opacity"

const VOLUME_SLIDER_CLASS =
  "h-8 cursor-pointer [&_[data-slot=slider-track]]:h-1 [&_[data-slot=slider-track]]:transition-[height] hover:[&_[data-slot=slider-track]]:h-1.5 [&_[data-slot=slider-range]]:bg-zinc-400 hover:[&_[data-slot=slider-range]]:bg-emerald-500 [&_[data-slot=slider-thumb]]:size-3 [&_[data-slot=slider-thumb]]:border-0 [&_[data-slot=slider-thumb]]:bg-zinc-300 [&_[data-slot=slider-thumb]]:cursor-grab"

function LyricsToggle() {
  const open = useUIStore((s) => s.lyricsPanelOpen)
  return (
    <ControlButton
      label={open ? "Hide lyrics" : "Show lyrics"}
      active={open}
      aria-pressed={open}
      onClick={() => useUIStore.getState().setLyricsPanelOpen(!open)}
    >
      <MicVocal className="size-4" />
    </ControlButton>
  )
}

function ControlButton({
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
        "size-9 rounded-full text-zinc-400 hover:text-zinc-100 hover:bg-zinc-800",
        active && "text-emerald-400 hover:text-emerald-400",
        className
      )}
      {...props}
    >
      {children}
    </Button>
  )
}

export function PlayerBar() {
  const currentTrack = usePlayerStore((s) => s.currentTrack)
  const isPlaying = usePlayerStore((s) => s.isPlaying)
  const position = usePlayerStore((s) => s.position)
  const duration = usePlayerStore((s) => s.duration)
  const volume = usePlayerStore((s) => s.volume)
  const muted = usePlayerStore((s) => s.muted)
  const shuffle = usePlayerStore((s) => s.shuffle)
  const repeat = usePlayerStore((s) => s.repeat)
  const queueLength = usePlayerStore((s) => s.queue.length)
  const crossfade = usePlayerStore((s) => s.crossfadeSeconds)
  const normalize = usePlayerStore((s) => s.normalizeVolume)

  const [seekPreview, setSeekPreview] = useState<number | null>(null)

  const effectiveDuration =
    duration > 0 ? duration : (currentTrack?.duration ?? 0)
  const displayPosition = Math.min(seekPreview ?? position, effectiveDuration || Infinity)
  const hasTrack = Boolean(currentTrack)

  const VolumeIcon = muted || volume === 0 ? VolumeX : volume < 0.5 ? Volume1 : Volume2

  return (
    <div
      className="relative z-30 border-t border-zinc-800/70 bg-zinc-950/95 shadow-[0_-8px_30px_rgba(0,0,0,0.45)] backdrop-blur supports-backdrop-filter:bg-zinc-950/80"
      role="region"
      aria-label="Player"
    >
      <div className="px-2 pt-1 md:hidden">
        <Slider
          aria-label="Seek"
          disabled={!hasTrack}
          value={[Math.min(displayPosition, effectiveDuration || 1)]}
          max={effectiveDuration || 1}
          step={1}
          onValueChange={([v]) => setSeekPreview(v)}
          onValueCommit={([v]) => {
            usePlayerStore.getState().seek(v)
            setSeekPreview(null)
          }}
          className={cn(SEEK_SLIDER_CLASS, "w-full")}
        />
      </div>

      <div className="flex h-16 items-center gap-2 px-3 pb-1 md:h-24 md:gap-4 md:px-4 md:pb-0 md:pt-0">
        <div className="flex min-w-0 flex-1 items-center gap-3 md:w-72 md:flex-none lg:w-80 xl:w-96">
          <button
            type="button"
            onClick={() => useUIStore.getState().setNowPlayingOpen(true)}
            disabled={!currentTrack}
            aria-label={currentTrack ? "Open now playing view" : "Nothing playing"}
            className="group/np flex min-w-0 flex-1 items-center gap-3 rounded-lg p-1 text-left transition-colors enabled:hover:bg-zinc-800/60 disabled:cursor-default md:w-auto md:flex-initial md:gap-3"
          >
            <div className="relative shrink-0">
              <div className="size-10 md:size-12">
                {currentTrack ? (
                  <CoverArt
                    trackId={currentTrack.id}
                    title={currentTrack.title}
                    hasCover={currentTrack.hasCover}
                  />
                ) : (
                  <div className="flex aspect-square w-full items-center justify-center rounded-md bg-zinc-800/80">
                    <ListMusic className="size-5 text-zinc-600" aria-hidden="true" />
                  </div>
                )}
              </div>
            </div>
            <div className="min-w-0 flex-1">
              {currentTrack ? (
                <>
                  <div className="truncate text-sm font-medium text-zinc-100 group-hover/np:text-emerald-300">
                    {currentTrack.title}
                  </div>
                  <div className="truncate text-xs text-zinc-500">
                    {currentTrack.artist || "Unknown artist"}
                  </div>
                </>
              ) : (
                <div className="truncate text-sm text-zinc-600">Nothing playing</div>
              )}
            </div>
            {currentTrack && (
              <ChevronUp
                aria-hidden="true"
                className="hidden size-4 shrink-0 text-zinc-600 transition-all group-hover/np:-translate-y-0.5 group-hover/np:text-emerald-400 md:block"
              />
            )}
          </button>
          {currentTrack && (
            <button
              type="button"
              onClick={() => useLibraryStore.getState().toggleFavorite(currentTrack.id)}
              aria-label={
                currentTrack.isFavorite ? "Remove from favorites" : "Add to favorites"
              }
              aria-pressed={currentTrack.isFavorite}
              className={cn(
                "hidden size-8 shrink-0 items-center justify-center rounded-md transition-colors sm:flex",
                currentTrack.isFavorite
                  ? "text-emerald-400"
                  : "text-zinc-500 hover:text-zinc-200"
              )}
            >
              <Heart className={cn("size-4", currentTrack.isFavorite && "fill-current")} />
            </button>
          )}
          {currentTrack && <TrackMenu track={currentTrack} side="top" showPlay={false} />}
        </div>

        <div className="hidden flex-1 flex-col items-center gap-1.5 md:flex">
          <div className="flex items-center gap-1">
            <ControlButton
              label="Shuffle"
              active={shuffle}
              onClick={() => usePlayerStore.getState().toggleShuffle()}
            >
              <Shuffle className="size-4" />
            </ControlButton>
            <ControlButton
              label="Previous track"
              disabled={!hasTrack}
              onClick={() => usePlayerStore.getState().prev()}
            >
              <SkipBack className="size-5 fill-current" />
            </ControlButton>
            <Button
              variant="ghost"
              size="icon"
              aria-label={isPlaying ? "Pause" : "Play"}
              disabled={!hasTrack}
              onClick={() => usePlayerStore.getState().togglePlay()}
              className="size-10 rounded-full bg-emerald-500 text-zinc-950 shadow-lg transition-transform hover:scale-105 hover:bg-emerald-400 active:scale-95 disabled:opacity-40 disabled:hover:scale-100"
            >
              {isPlaying ? (
                <Pause className="size-5 fill-current" />
              ) : (
                <Play className="size-5 fill-current translate-x-px" />
              )}
            </Button>
            <ControlButton
              label="Next track"
              disabled={queueLength === 0}
              onClick={() => usePlayerStore.getState().next()}
            >
              <SkipForward className="size-5 fill-current" />
            </ControlButton>
            <ControlButton
              label={
                repeat === "one" ? "Repeat one track" : repeat === "all" ? "Repeat all" : "Repeat off"
              }
              active={repeat !== "off"}
              onClick={() => usePlayerStore.getState().cycleRepeat()}
            >
              {repeat === "one" ? <Repeat1 className="size-4" /> : <Repeat className="size-4" />}
            </ControlButton>
          </div>
          <div className="group/seek flex w-full max-w-xl items-center gap-2.5">
            <span className="w-10 text-right text-[11px] tabular-nums text-zinc-500">
              {formatDuration(displayPosition)}
            </span>
            {currentTrack ? (
              <WaveformSeek
                track={currentTrack}
                position={displayPosition}
                duration={effectiveDuration}
                onPreview={setSeekPreview}
                onSeek={(v) => usePlayerStore.getState().seek(v)}
                height={26}
                className="flex-1"
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
                    className={cn(SEEK_SLIDER_CLASS, "flex-1")}
                  />
                }
              />
            ) : (
              <Slider
                aria-label="Seek"
                disabled
                value={[0]}
                max={1}
                step={1}
                className={cn(SEEK_SLIDER_CLASS, "flex-1")}
              />
            )}
            <span className="w-10 text-[11px] tabular-nums text-zinc-500">
              {formatDuration(effectiveDuration)}
            </span>
          </div>
        </div>

        <div className="flex shrink-0 items-center gap-0.5 md:hidden">
          <Button
            variant="ghost"
            size="icon"
            aria-label="Open queue"
            onClick={() => useUIStore.getState().setQueueOpen(true)}
            className="size-9 rounded-full text-zinc-400 hover:text-zinc-100"
          >
            <ListMusic className="size-5" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            aria-label={isPlaying ? "Pause" : "Play"}
            disabled={!hasTrack}
            onClick={() => usePlayerStore.getState().togglePlay()}
            className="size-10 rounded-full bg-emerald-500 text-zinc-950 shadow-lg transition-transform hover:scale-105 active:scale-95 disabled:opacity-40 disabled:hover:scale-100"
          >
            {isPlaying ? (
              <Pause className="size-5 fill-current" />
            ) : (
              <Play className="size-5 fill-current translate-x-px" />
            )}
          </Button>
          <Button
            variant="ghost"
            size="icon"
            aria-label="Next track"
            disabled={queueLength === 0}
            onClick={() => usePlayerStore.getState().next()}
            className="size-9 rounded-full text-zinc-400 hover:text-zinc-100"
          >
            <SkipForward className="size-5 fill-current" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            aria-label="Expand now playing"
            disabled={!hasTrack}
            onClick={() => useUIStore.getState().setNowPlayingOpen(true)}
            className="size-9 rounded-full text-zinc-400 hover:text-zinc-100 disabled:opacity-30"
          >
            <ChevronUp className="size-5" />
          </Button>
        </div>

        <div className="hidden items-center justify-end gap-0.5 md:flex md:w-72 lg:w-80 xl:w-96">
          {currentTrack ? <AddToPlaylistButton track={currentTrack} /> : null}
          <ControlButton
            label={`Playback settings${crossfade > 0 ? ` · crossfade ${crossfade}s` : ""}${normalize ? " · normalized" : ""}`}
            active={crossfade > 0 || normalize}
            onClick={() => useUIStore.getState().setPlaybackSettingsOpen(true)}
          >
            <SlidersHorizontal className="size-4" />
          </ControlButton>
          <SleepTimerMenu />
          <LyricsToggle />
          <ControlButton
            label="Open queue"
            onClick={() => useUIStore.getState().setQueueOpen(true)}
          >
            <ListMusic className="size-4" />
          </ControlButton>
          <div className="flex min-w-0 items-center gap-2 pl-2">
            <ControlButton
              label={muted ? "Unmute" : "Mute"}
              onClick={() => usePlayerStore.getState().toggleMute()}
            >
              <VolumeIcon className="size-4" />
            </ControlButton>
            <Slider
              aria-label="Volume"
              value={[Math.round(volume * 100)]}
              max={100}
              step={1}
              onValueChange={([v]) => usePlayerStore.getState().setVolume(v / 100)}
              className={cn(VOLUME_SLIDER_CLASS, "w-28 min-w-12")}
            />
          </div>
        </div>
      </div>
    </div>
  )
}
