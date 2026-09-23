"use client"

import { useEffect, useRef, useState } from "react"
import { AudioLines, Loader2, Music2, Pause, Play } from "lucide-react"
import { fetchSharedTrack, sharedCoverUrl, sharedStreamUrl } from "@/lib/api"
import { lyricsGradient } from "@/lib/color"
import { formatDuration } from "@/lib/format"
import type { SharedTrack } from "@/lib/types"
import { Button } from "@/components/ui/button"

export function SharePage({ token }: { token: string }) {
  const [state, setState] = useState<{ track?: SharedTrack; error?: string } | null>(null)

  useEffect(() => {
    let alive = true
    fetchSharedTrack(token)
      .then((track) => alive && setState({ track }))
      .catch((e: Error) => alive && setState({ error: e.message }))
    return () => {
      alive = false
    }
  }, [token])

  return (
    <div
      className="flex min-h-dvh flex-col items-center justify-center px-4 py-10"
      style={{ background: lyricsGradient(state?.track?.coverColor) }}
    >
      {state === null ? (
        <Loader2 className="size-6 animate-spin text-white/70" aria-hidden="true" />
      ) : state.error ? (
        <div className="text-center">
          <Music2 className="mx-auto size-10 text-white/50" aria-hidden="true" />
          <h1 className="mt-3 text-lg font-semibold text-white">This link doesn&apos;t work any more</h1>
          <p className="mt-1 text-sm text-white/60">{state.error}</p>
        </div>
      ) : state.track ? (
        <SharedPlayer token={token} track={state.track} />
      ) : null}

      <a
        href="/"
        className="mt-10 inline-flex items-center gap-2 text-xs text-white/50 transition-colors hover:text-white/80"
      >
        <AudioLines className="size-3.5" aria-hidden="true" />
        Music
      </a>
    </div>
  )
}

function SharedPlayer({ token, track }: { token: string; track: SharedTrack }) {
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const [playing, setPlaying] = useState(false)
  const [position, setPosition] = useState(0)
  const [duration, setDuration] = useState(track.duration)
  const [failed, setFailed] = useState(false)

  const toggle = () => {
    const el = audioRef.current
    if (!el) return
    if (el.paused) el.play().catch(() => setFailed(true))
    else el.pause()
  }

  const seek = (to: number) => {
    const el = audioRef.current
    if (el) el.currentTime = to
    setPosition(to)
  }

  const shown = duration > 0 ? duration : track.duration

  return (
    <div className="w-full max-w-sm">
      <div className="aspect-square w-full overflow-hidden rounded-2xl bg-black/30 shadow-2xl shadow-black/40">
        {track.hasCover ? (
          <img src={sharedCoverUrl(token)} alt="" className="size-full object-cover" />
        ) : (
          <div className="flex size-full items-center justify-center">
            <Music2 className="size-16 text-white/30" aria-hidden="true" />
          </div>
        )}
      </div>

      <h1 className="mt-5 text-center text-xl font-bold text-white">{track.title}</h1>
      <p className="mt-1 text-center text-sm text-white/70">
        {track.artist || "Unknown artist"}
        {track.album ? ` · ${track.album}` : ""}
      </p>

      <input
        type="range"
        min={0}
        max={shown > 0 ? shown : 1}
        step={0.1}
        value={Math.min(position, shown || position)}
        onChange={(e) => seek(parseFloat(e.target.value))}
        aria-label="Seek"
        disabled={shown <= 0}
        className="mt-6 w-full accent-white"
      />
      <div className="flex justify-between text-[11px] text-white/60">
        <span>{formatDuration(position)}</span>
        <span>{formatDuration(shown)}</span>
      </div>

      <div className="mt-4 flex justify-center">
        <Button
          onClick={toggle}
          aria-label={playing ? "Pause" : "Play"}
          className="size-16 rounded-full bg-white text-zinc-900 hover:bg-white/90"
        >
          {playing ? <Pause className="size-7 fill-current" /> : <Play className="size-7 fill-current" />}
        </Button>
      </div>

      {failed ? (
        <p className="mt-4 text-center text-xs text-red-300">
          This browser couldn&apos;t play the file.{" "}
          <a href={sharedStreamUrl(token)} className="underline">
            Open it directly
          </a>
          .
        </p>
      ) : null}

      {track.sharedBy ? (
        <p className="mt-6 text-center text-xs text-white/40">Shared by {track.sharedBy}</p>
      ) : null}

      <audio
        ref={audioRef}
        src={sharedStreamUrl(token)}
        preload="metadata"
        onPlay={() => setPlaying(true)}
        onPause={() => setPlaying(false)}
        onEnded={() => setPlaying(false)}
        onTimeUpdate={(e) => setPosition(e.currentTarget.currentTime)}
        onLoadedMetadata={(e) => {
          if (Number.isFinite(e.currentTarget.duration)) setDuration(e.currentTarget.duration)
        }}
        onError={() => setFailed(true)}
      />
    </div>
  )
}
