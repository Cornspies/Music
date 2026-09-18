"use client"

import { cn } from "@/lib/utils"

/** Tiny animated equalizer bars — the "currently playing" indicator. */
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
