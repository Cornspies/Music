"use client"

import { useState } from "react"
import { toast } from "sonner"
import type { AvatarCrop } from "@/lib/api"
import { playlistCoverUrl } from "@/lib/api"
import type { Playlist } from "@/lib/types"
import { cn } from "@/lib/utils"
import { useLibraryStore } from "@/stores/library"
import { AvatarCropDialog } from "@/components/music/AvatarCropDialog"

const MAX_BYTES = 8 * 1024 * 1024

export function PlaylistCoverImage({
  playlist,
  className,
}: {
  playlist: Pick<Playlist, "id" | "coverVersion">
  className?: string
}) {
  const src = playlistCoverUrl(playlist)
  if (!src) return null
  return (
    <img
      src={src}
      alt=""
      draggable={false}
      loading="lazy"
      className={cn("absolute inset-0 size-full object-cover", className)}
    />
  )
}

let coverInput: HTMLInputElement | null = null
let coverTarget: string | null = null

export function pickPlaylistCover(playlistId: string) {
  if (!coverInput) return
  coverTarget = playlistId
  coverInput.value = ""
  coverInput.click()
}

export async function clearPlaylistCover(playlistId: string) {
  if (await useLibraryStore.getState().setPlaylistCover(playlistId, null)) toast.success("Cover removed")
}

export function PlaylistCoverPicker() {
  const [pending, setPending] = useState<{ id: string; file: File } | null>(null)
  const [busy, setBusy] = useState(false)

  const handleFile = (file: File | undefined) => {
    const id = coverTarget
    if (!file || !id) return
    if (file.size > MAX_BYTES) {
      toast.error("That image is larger than 8 MB.")
      return
    }
    setPending({ id, file })
  }

  const save = async (crop: AvatarCrop) => {
    if (!pending) return
    setBusy(true)
    const ok = await useLibraryStore.getState().setPlaylistCover(pending.id, pending.file, crop)
    setBusy(false)
    if (ok) {
      toast.success("Cover updated")
      setPending(null)
    }
  }

  return (
    <>
      <input
        ref={(el) => {
          coverInput = el
        }}
        type="file"
        accept="image/png,image/jpeg,image/gif"
        className="hidden"
        onChange={(e) => handleFile(e.target.files?.[0])}
      />
      <AvatarCropDialog
        file={pending?.file ?? null}
        busy={busy}
        shape="square"
        onCancel={() => setPending(null)}
        onConfirm={save}
      />
    </>
  )
}
