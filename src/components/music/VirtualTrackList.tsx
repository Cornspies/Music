"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import type { Track } from "@/lib/types"
import { TrackRow } from "@/components/music/TrackRow"

const OVERSCAN = 10

export function VirtualTrackList({
  tracks,
  followTrackId,
  onRemoveFromPlaylist,
  onDismissAttention,
}: {
  tracks: Track[]
  followTrackId?: string | null
  onRemoveFromPlaylist?: (trackId: string) => void
  onDismissAttention?: (trackId: string) => void
}) {
  const listRef = useRef<HTMLDivElement>(null)
  const rowsRef = useRef<HTMLDivElement>(null)
  const [rowHeight, setRowHeight] = useState(56)
  const [range, setRange] = useState({ start: 0, end: 40 })
  const follow = useRef<{ id: string; smooth: boolean } | null>(null)
  const followed = useRef(false)

  const measure = useCallback(() => {
    const list = listRef.current
    const main = list?.closest("main")
    if (!list || !main) return
    const above = main.getBoundingClientRect().top - list.getBoundingClientRect().top
    const start = Math.max(0, Math.floor(above / rowHeight) - OVERSCAN)
    const end = Math.min(tracks.length, Math.ceil((above + main.clientHeight) / rowHeight) + OVERSCAN)
    setRange((r) => (r.start === start && r.end === end ? r : { start, end }))
  }, [rowHeight, tracks.length])

  useEffect(() => {
    const main = listRef.current?.closest("main")
    if (!main) return
    const frame = requestAnimationFrame(measure)
    main.addEventListener("scroll", measure, { passive: true })
    const resize = new ResizeObserver(measure)
    resize.observe(main)
    return () => {
      cancelAnimationFrame(frame)
      main.removeEventListener("scroll", measure)
      resize.disconnect()
    }
  }, [measure])

  useEffect(() => {
    const rows = rowsRef.current
    if (!rows) return
    const resize = new ResizeObserver(() => {
      const h = rows.firstElementChild?.getBoundingClientRect().height
      if (h) setRowHeight((prev) => (Math.abs(prev - h) > 0.5 ? h : prev))
    })
    resize.observe(rows)
    return () => resize.disconnect()
  }, [])

  useEffect(() => {
    if (!followTrackId) return
    follow.current = { id: followTrackId, smooth: followed.current }
    followed.current = true
  }, [followTrackId])

  useEffect(() => {
    const target = follow.current
    if (!target || tracks.length === 0) return
    const frame = requestAnimationFrame(() => {
      follow.current = null
      const index = tracks.findIndex((t) => t.id === target.id)
      const list = listRef.current
      const main = list?.closest("main")
      if (index < 0 || !list || !main) return
      const top = list.getBoundingClientRect().top - main.getBoundingClientRect().top + main.scrollTop + index * rowHeight
      if (target.smooth && top >= main.scrollTop && top + rowHeight <= main.scrollTop + main.clientHeight) return
      main.scrollTo({
        top: Math.max(0, top - (main.clientHeight - rowHeight) / 2),
        behavior: target.smooth ? "smooth" : "auto",
      })
      measure()
    })
    return () => cancelAnimationFrame(frame)
  }, [followTrackId, tracks, rowHeight, measure])

  return (
    <div ref={listRef} role="list" aria-label="Tracks" className="relative" style={{ height: tracks.length * rowHeight }}>
      <div
        ref={rowsRef}
        className="absolute inset-x-0 top-0 flex flex-col"
        style={{ transform: `translateY(${range.start * rowHeight}px)` }}
      >
        {tracks.slice(range.start, range.end).map((track, i) => (
          <TrackRow
            key={`${track.id}-${range.start + i}`}
            track={track}
            index={range.start + i}
            contextQueue={tracks}
            onRemoveFromPlaylist={onRemoveFromPlaylist}
            onDismissAttention={onDismissAttention}
          />
        ))}
      </div>
    </div>
  )
}
