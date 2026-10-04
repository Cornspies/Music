"use client"

import { cn } from "@/lib/utils"
import { usePlayerStore } from "@/stores/player"

export function EqualizerIcon({
  playing,
  className,
}: {
  playing: boolean
  className?: string
}) {
  return (
    <span className={cn("flex h-3 items-end gap-0.5", className)} aria-hidden="true">
      {[0, 1, 2, 3].map((i) => (
        <span
          key={i}
          className="eq-bar bg-emerald-400"
          style={{
            animationDelay: `${i * 0.17}s`,
            animationDuration: `${0.85 + i * 0.12}s`,
            animationPlayState: playing ? "running" : "paused",
          }}
        />
      ))}
    </span>
  )
}

export function EqualizerButton({
  playing,
  className,
}: {
  playing: boolean
  className?: string
}) {
  return (
    <button
      type="button"
      aria-label={playing ? "Pause" : "Play"}
      title={playing ? "Pause" : "Play"}
      onClick={(e) => {
        e.stopPropagation()
        usePlayerStore.getState().togglePlay()
      }}
      onDoubleClick={(e) => e.stopPropagation()}
      className={cn(
        "flex size-6 cursor-pointer items-center justify-center rounded-md transition-colors hover:bg-zinc-700/60 focus-visible:ring-2 focus-visible:ring-emerald-400/60 focus-visible:outline-none",
        className
      )}
    >
      <EqualizerIcon playing={playing} />
    </button>
  )
}
