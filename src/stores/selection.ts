"use client"

import { create } from "zustand"
import type { Track } from "@/lib/types"

export type SelectMode = "replace" | "toggle" | "range" | "extend"

interface SelectionState {
  ids: Set<string>
  context: Track[]
  anchor: number | null
  remove: ((trackId: string) => void) | null
  select: (
    track: Track,
    index: number | undefined,
    context: Track[],
    mode: SelectMode,
    remove?: (trackId: string) => void
  ) => void
  selectAll: () => void
  clear: () => void
}

const EMPTY = new Set<string>()

function indexIn(context: Track[], track: Track, index: number | undefined): number {
  if (index !== undefined && context[index]?.id === track.id) return index
  return context.findIndex((t) => t.id === track.id)
}

export const useSelectionStore = create<SelectionState>()((set, get) => ({
  ids: EMPTY,
  context: [],
  anchor: null,
  remove: null,

  select: (track, index, context, mode, remove) => {
    const state = get()
    const sameList =
      state.context === context || (state.ids.size > 0 && [...state.ids].every((id) => context.some((t) => t.id === id)))
    const at = indexIn(context, track, index)
    const base = mode === "replace" || !sameList ? EMPTY : state.ids
    let ids: Set<string>
    let anchor = state.anchor
    if ((mode === "range" || mode === "extend") && sameList && anchor !== null && at >= 0) {
      ids = new Set(mode === "extend" ? base : [])
      const [from, to] = anchor < at ? [anchor, at] : [at, anchor]
      for (let i = from; i <= to; i++) if (context[i]) ids.add(context[i].id)
    } else if (mode === "toggle" && sameList) {
      ids = new Set(base)
      if (ids.has(track.id)) ids.delete(track.id)
      else ids.add(track.id)
      anchor = at
    } else {
      ids = new Set([track.id])
      anchor = at
    }
    set({ ids: ids.size ? ids : EMPTY, context, anchor, remove: remove ?? null })
  },

  selectAll: () => {
    const { context } = get()
    if (context.length === 0) return
    set({ ids: new Set(context.map((t) => t.id)) })
  },

  clear: () => {
    if (get().ids.size === 0 && get().context.length === 0) return
    set({ ids: EMPTY, context: [], anchor: null, remove: null })
  },
}))

export function selectedTracks(): Track[] {
  const { ids, context } = useSelectionStore.getState()
  const seen = new Set<string>()
  const out: Track[] = []
  for (const t of context) {
    if (!ids.has(t.id) || seen.has(t.id)) continue
    seen.add(t.id)
    out.push(t)
  }
  return out
}
