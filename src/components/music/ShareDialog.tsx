"use client"

import { useEffect, useState } from "react"
import { Check, Copy, Link2, Loader2, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { createTrackShare, deleteTrackShare, fetchTrackShare } from "@/lib/api"
import { copyText } from "@/lib/clipboard"
import type { Track, TrackShare } from "@/lib/types"
import { useUIStore } from "@/stores/ui"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"

/**
 * A link to one track that plays for whoever opens it — no account on this
 * server needed. The link is made on demand (nothing is public until it is),
 * the same track always reuses the same link, and "Stop sharing" kills it.
 */
export function ShareDialog() {
  const track = useUIStore((s) => s.shareTrack)
  const close = () => useUIStore.getState().setShareTrack(null)
  return (
    <Dialog open={track !== null} onOpenChange={(o) => !o && close()}>
      {track ? <ShareBody key={track.id} track={track} onClose={close} /> : null}
    </Dialog>
  )
}

function urlFor(share: TrackShare): string {
  return `${window.location.origin}/s/${share.token}`
}

function ShareBody({ track, onClose }: { track: Track; onClose: () => void }) {
  const [share, setShare] = useState<TrackShare | null | undefined>(undefined)
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState(false)

  // An existing link is shown as it is; the first "create" is a deliberate
  // press, so nothing becomes reachable by opening this dialog.
  useEffect(() => {
    let alive = true
    fetchTrackShare(track.id).then((s) => alive && setShare(s))
    return () => {
      alive = false
    }
  }, [track.id])

  const create = async () => {
    setBusy(true)
    try {
      const made = await createTrackShare(track.id)
      setShare(made)
      await copy(made)
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const copy = async (s: TrackShare) => {
    const url = urlFor(s)
    if (await copyText(url)) {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
      toast.success("Link copied")
    } else {
      toast.error(`Couldn't copy automatically. The link is: ${url}`)
    }
  }

  const revoke = async () => {
    setBusy(true)
    try {
      await deleteTrackShare(track.id)
      setShare(null)
      toast.success("Link turned off")
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <DialogContent className="border-zinc-800 bg-zinc-950 text-zinc-100 sm:max-w-lg">
      <DialogHeader>
        <DialogTitle className="flex items-center gap-2 text-base">
          <Link2 className="size-4 text-emerald-400" aria-hidden="true" />
          Share “{track.title}”
        </DialogTitle>
        <DialogDescription className="text-xs text-zinc-500">
          Anyone with the link can play this one track, without an account here.
        </DialogDescription>
      </DialogHeader>

      {share === undefined ? (
        <div className="flex justify-center py-6">
          <Loader2 className="size-5 animate-spin text-zinc-500" />
        </div>
      ) : share === null ? (
        <p className="text-sm text-zinc-400">This track isn&apos;t shared yet.</p>
      ) : (
        <>
          <div className="flex items-center gap-2">
            <input
              readOnly
              value={urlFor(share)}
              onFocus={(e) => e.currentTarget.select()}
              aria-label="Share link"
              className="min-w-0 flex-1 rounded-md border border-zinc-800 bg-zinc-900/60 px-3 py-2 text-xs text-zinc-200 outline-none focus:border-emerald-500/60"
            />
            <Button size="sm" onClick={() => copy(share)} className="bg-zinc-100 text-zinc-900 hover:bg-white">
              {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
              {copied ? "Copied" : "Copy"}
            </Button>
          </div>
          <p className="text-xs text-zinc-500">
            {share.plays > 0 ? `Opened ${share.plays} ${share.plays === 1 ? "time" : "times"}` : "Not opened yet"}
            {share.mine ? "" : " · made by someone else"}
          </p>
        </>
      )}

      <DialogFooter className="gap-2 sm:justify-between">
        <div>
          {share ? (
            <Button
              variant="ghost"
              size="sm"
              disabled={busy}
              onClick={revoke}
              className="text-red-400 hover:bg-red-500/10 hover:text-red-300"
            >
              <Trash2 className="size-3.5" /> Stop sharing
            </Button>
          ) : null}
        </div>
        <div className="flex gap-2">
          <Button variant="ghost" size="sm" onClick={onClose} className="text-zinc-300">
            {share ? "Done" : "Cancel"}
          </Button>
          {share === null ? (
            <Button
              size="sm"
              disabled={busy}
              onClick={create}
              className="bg-emerald-500 text-zinc-950 hover:bg-emerald-400"
            >
              {busy ? <Loader2 className="size-3.5 animate-spin" /> : <Link2 className="size-3.5" />}
              Create link
            </Button>
          ) : null}
        </div>
      </DialogFooter>
    </DialogContent>
  )
}
