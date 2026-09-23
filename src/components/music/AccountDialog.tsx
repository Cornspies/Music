"use client"

import { useState } from "react"
import { toast } from "sonner"
import { KeyRound, Loader2, Trash2, UserRound } from "lucide-react"
import { deleteAccount, renameAccount } from "@/lib/api"
import { useAuthStore } from "@/stores/auth"
import { useUIStore } from "@/stores/ui"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

export function AccountDialog() {
  const open = useUIStore((s) => s.accountOpen)
  const setOpen = useUIStore((s) => s.setAccountOpen)
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      {open ? <AccountBody onClose={() => setOpen(false)} /> : null}
    </Dialog>
  )
}

function AccountBody({ onClose }: { onClose: () => void }) {
  const user = useAuthStore((s) => s.user)

  return (
    <DialogContent aria-describedby={undefined} className="border-zinc-800 bg-zinc-950 text-zinc-100 sm:max-w-md">
      <DialogHeader>
        <DialogTitle className="flex items-center gap-2 text-base">
          <UserRound className="size-4 text-emerald-400" aria-hidden="true" />
          Account
        </DialogTitle>
      </DialogHeader>

      <RenameSection current={user?.username ?? ""} />

      <section className="flex items-center justify-between gap-3 rounded-lg border border-zinc-800/80 bg-zinc-900/40 px-3 py-2.5">
        <p className="text-sm text-zinc-200">Password</p>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            onClose()
            useUIStore.getState().setPasswordOpen(true)
          }}
          className="text-zinc-300"
        >
          <KeyRound className="size-3.5" /> Change…
        </Button>
      </section>

      <DeleteSection onDeleted={onClose} />
    </DialogContent>
  )
}

function RenameSection({ current }: { current: string }) {
  const [name, setName] = useState(current)
  const [password, setPassword] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const changed = name.trim().toLowerCase() !== current && name.trim() !== ""

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!changed || busy) return
    setBusy(true)
    setError(null)
    try {
      const { token, user } = await renameAccount(name.trim(), password)
      useAuthStore.setState({ token, user })
      setPassword("")
      setName(user.username)
      toast.success(`You're now “${user.username}”`, { description: "Use this name to sign in." })
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={save} className="flex flex-col gap-2 rounded-lg border border-zinc-800/80 bg-zinc-900/40 p-3">
      <Label htmlFor="account-name" className="text-sm text-zinc-200">
        Username
      </Label>
      <Input
        id="account-name"
        value={name}
        onChange={(e) => setName(e.target.value)}
        autoComplete="username"
        spellCheck={false}
        className="border-zinc-800 bg-zinc-950/60"
      />
      {changed ? (
        <Input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          placeholder="Current password"
          aria-label="Current password"
          autoComplete="current-password"
          className="border-zinc-800 bg-zinc-950/60"
        />
      ) : null}
      {error ? <p className="text-xs text-red-400">{error}</p> : null}
      {changed ? (
        <Button
          type="submit"
          size="sm"
          disabled={busy || password === ""}
          className="self-end bg-emerald-500 text-zinc-950 hover:bg-emerald-400"
        >
          {busy ? <Loader2 className="size-3.5 animate-spin" /> : null}
          Save
        </Button>
      ) : null}
    </form>
  )
}

function DeleteSection({ onDeleted }: { onDeleted: () => void }) {
  const [confirming, setConfirming] = useState(false)
  const [password, setPassword] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const remove = async (e: React.FormEvent) => {
    e.preventDefault()
    if (busy) return
    setBusy(true)
    setError(null)
    try {
      await deleteAccount(password)
      onDeleted()
      useAuthStore.getState().logout()
      toast.success("Your account was deleted")
    } catch (err) {
      setError((err as Error).message)
      setBusy(false)
    }
  }

  if (!confirming) {
    return (
      <section className="flex items-center justify-between gap-3 rounded-lg border border-zinc-800/80 bg-zinc-900/40 px-3 py-2.5">
        <p className="text-sm text-zinc-200">Delete account</p>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => setConfirming(true)}
          className="text-red-400 hover:bg-red-500/10 hover:text-red-300"
        >
          <Trash2 className="size-3.5" /> Delete…
        </Button>
      </section>
    )
  }

  return (
    <form onSubmit={remove} className="flex flex-col gap-2 rounded-lg border border-red-500/30 bg-red-500/5 p-3">
      <p className="text-sm font-medium text-red-300">Delete your account?</p>
      <p className="text-xs text-zinc-400">
        Your uploads stay in the library. Your playlists, favorites and history are deleted. This can&apos;t
        be undone.
      </p>
      <Input
        type="password"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        placeholder="Current password"
        aria-label="Current password"
        autoComplete="current-password"
        autoFocus
        className="border-zinc-800 bg-zinc-950/60"
      />
      {error ? <p className="text-xs text-red-400">{error}</p> : null}
      <div className="flex justify-end gap-2">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          disabled={busy}
          onClick={() => {
            setConfirming(false)
            setPassword("")
            setError(null)
          }}
          className="text-zinc-300"
        >
          Cancel
        </Button>
        <Button
          type="submit"
          size="sm"
          disabled={busy || password === ""}
          className="bg-red-600 text-white hover:bg-red-500"
        >
          {busy ? <Loader2 className="size-3.5 animate-spin" /> : <Trash2 className="size-3.5" />}
          Delete account
        </Button>
      </div>
    </form>
  )
}
