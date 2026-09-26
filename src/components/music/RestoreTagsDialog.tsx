"use client"

import { useEffect, useState } from "react"
import { Loader2 } from "lucide-react"
import { toast } from "sonner"
import { fetchFileTagChanges, restoreTrackTags } from "@/lib/api"
import { TAG_FIELD_LABELS } from "@/lib/format"
import type { TagChange } from "@/lib/types"
import { useLibraryStore } from "@/stores/library"
import { useUIStore } from "@/stores/ui"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"

export function RestoreTagsDialog() {
  const track = useUIStore((s) => s.restoreTagsTrack)
  const [result, setResult] = useState<{ id: string; changes?: TagChange[]; error?: string } | null>(null)
  const [skipped, setSkipped] = useState<{ id: string; fields: string[] }>({ id: "", fields: [] })
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!track) return
    let alive = true
    fetchFileTagChanges(track.id)
      .then((r) => {
        if (alive) setResult({ id: track.id, changes: r.changes })
      })
      .catch((e: Error) => {
        if (alive) setResult({ id: track.id, error: e.message })
      })
    return () => {
      alive = false
    }
  }, [track])

  const current = track && result?.id === track.id ? result : null
  const changes = current?.changes ?? null
  const skip = skipped.id === track?.id ? skipped.fields : []
  const chosen = changes?.filter((c) => !skip.includes(c.field)) ?? []

  const close = () => useUIStore.getState().setRestoreTagsTrack(null)

  const toggle = (field: string) => {
    if (!track) return
    setSkipped({ id: track.id, fields: skip.includes(field) ? skip.filter((f) => f !== field) : [...skip, field] })
  }

  const restore = async () => {
    if (!track || chosen.length === 0) return
    setBusy(true)
    try {
      const r = await restoreTrackTags(track.id, chosen.map((c) => c.field))
      useLibraryStore.getState().replaceTrack(r.track)
      toast.success(`Restored ${chosen.map((c) => TAG_FIELD_LABELS[c.field].toLowerCase()).join(", ")} from the file`)
      close()
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={track !== null} onOpenChange={(o) => !o && close()}>
      <DialogContent aria-describedby={undefined} className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Restore tags from file</DialogTitle>
        </DialogHeader>
        {!current ? (
          <div className="flex justify-center py-8 text-zinc-500">
            <Loader2 className="size-5 animate-spin" aria-hidden="true" />
          </div>
        ) : current.error ? (
          <p className="text-sm text-red-300">{current.error}</p>
        ) : changes && changes.length === 0 ? (
          <p className="text-sm text-zinc-400">The details already match the file&apos;s own tags.</p>
        ) : (
          <ul className="flex flex-col gap-2">
            {changes?.map((c) => (
              <li key={c.field}>
                <label className="flex cursor-pointer items-start gap-3 rounded-lg border border-zinc-800 bg-zinc-900/50 px-3 py-2">
                  <input
                    type="checkbox"
                    checked={!skip.includes(c.field)}
                    onChange={() => toggle(c.field)}
                    className="mt-1 size-4 accent-emerald-500"
                  />
                  <span className="min-w-0 text-sm">
                    <span className="block text-xs text-zinc-500">{TAG_FIELD_LABELS[c.field]}</span>
                    <span className="block truncate text-zinc-500 line-through">{c.current || "—"}</span>
                    <span className="block truncate text-emerald-300">{c.file}</span>
                  </span>
                </label>
              </li>
            ))}
          </ul>
        )}
        <DialogFooter>
          <Button variant="ghost" onClick={close}>
            {changes && changes.length > 0 ? "Cancel" : "Close"}
          </Button>
          {changes && changes.length > 0 ? (
            <Button
              onClick={restore}
              disabled={busy || chosen.length === 0}
              className="bg-emerald-500 font-semibold text-zinc-950 hover:bg-emerald-400"
            >
              {busy ? <Loader2 className="size-4 animate-spin" aria-hidden="true" /> : null}
              Restore
            </Button>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
