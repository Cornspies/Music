"use client"

import { useRef, useState } from "react"
import { Camera, Loader2 } from "lucide-react"
import { toast } from "sonner"
import type { AvatarCrop } from "@/lib/api"
import { removeAvatar, uploadAvatar } from "@/lib/api"
import { AvatarCropDialog } from "@/components/music/AvatarCropDialog"
import { useAuthStore } from "@/stores/auth"
import { useLibraryStore } from "@/stores/library"
import { UserAvatar } from "@/components/music/UserAvatar"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

const MAX_BYTES = 8 * 1024 * 1024

export function AvatarPicker() {
  const user = useAuthStore((s) => s.user)
  const inputRef = useRef<HTMLInputElement>(null)
  const [busy, setBusy] = useState(false)
  const [pending, setPending] = useState<File | null>(null)

  if (!user) return null

  const handleFile = (file: File | undefined) => {
    if (!file) return
    if (file.size > MAX_BYTES) {
      toast.error("That image is larger than 8 MB.")
      return
    }
    setPending(file)
  }

  const handleCropped = async (crop: AvatarCrop) => {
    const file = pending
    if (!file) return
    setBusy(true)
    try {
      const updated = await uploadAvatar(file, crop)
      useAuthStore.setState({ user: updated })
      useLibraryStore.getState().fetchAll().catch(() => {})
      toast.success("Avatar updated")
      setPending(null)
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
      if (inputRef.current) inputRef.current.value = ""
    }
  }

  const handleRemove = async () => {
    setBusy(true)
    try {
      const updated = await removeAvatar()
      useAuthStore.setState({ user: updated })
      useLibraryStore.getState().fetchAll().catch(() => {})
      toast.success("Avatar removed")
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <input
        ref={inputRef}
        type="file"
        accept="image/png,image/jpeg,image/gif,image/webp"
        className="hidden"
        onChange={(e) => handleFile(e.target.files?.[0])}
      />
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            disabled={busy}
            aria-label="Change your avatar"
            className="group/avatar relative size-8 shrink-0 rounded-full ring-1 ring-zinc-600/60 transition-opacity hover:opacity-90 disabled:opacity-60"
          >
            <UserAvatar
              userId={user.id}
              username={user.username}
              hasAvatar={user.hasAvatar}
              size={32}
              label={user.username}
            />
            <span className="absolute inset-0 flex items-center justify-center rounded-full bg-zinc-950/60 opacity-0 transition-opacity group-hover/avatar:opacity-100">
              {busy ? (
                <Loader2 className="size-3.5 animate-spin text-zinc-200" />
              ) : (
                <Camera className="size-3.5 text-zinc-200" />
              )}
            </span>
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-44">
          <DropdownMenuItem onSelect={() => inputRef.current?.click()}>
            {user.hasAvatar ? "Change picture" : "Upload a picture"}
          </DropdownMenuItem>
          {user.hasAvatar && (
            <DropdownMenuItem onSelect={handleRemove} className="text-red-400">
              Remove picture
            </DropdownMenuItem>
          )}
        </DropdownMenuContent>
      </DropdownMenu>

      <AvatarCropDialog
        file={pending}
        busy={busy}
        onCancel={() => {
          setPending(null)
          if (inputRef.current) inputRef.current.value = ""
        }}
        onConfirm={handleCropped}
      />
    </>
  )
}
