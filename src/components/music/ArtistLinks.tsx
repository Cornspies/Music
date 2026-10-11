"use client"

import { Fragment } from "react"
import { artistParts } from "@/lib/format"
import { useUIStore } from "@/stores/ui"

export function JumpLink({ label, onOpen }: { label: string; onOpen: () => void }) {
  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation()
        onOpen()
      }}
      onDoubleClick={(e) => e.stopPropagation()}
      className="max-w-full truncate text-left hover:text-zinc-200 hover:underline focus-visible:underline focus-visible:outline-none"
    >
      {label}
    </button>
  )
}

export function ArtistLinks({ credit, fallback = "Unknown artist" }: { credit: string; fallback?: string }) {
  const parts = artistParts(credit)
  if (parts.length === 0) return <>{fallback}</>
  return (
    <>
      {parts.map((p, i) =>
        p.artist ? (
          <button
            key={i}
            type="button"
            onClick={(e) => {
              e.stopPropagation()
              useUIStore.getState().navigate("artist", p.text.trim())
            }}
            onDoubleClick={(e) => e.stopPropagation()}
            className="hover:text-zinc-200 hover:underline focus-visible:underline focus-visible:outline-none"
          >
            {p.text}
          </button>
        ) : (
          <Fragment key={i}>{p.text.trim() === "," || p.text.trim() === ";" ? `${p.text.trim()} ` : ` ${p.text.trim()} `}</Fragment>
        )
      )}
    </>
  )
}
