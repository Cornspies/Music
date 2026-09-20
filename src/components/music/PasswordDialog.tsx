"use client"

import { useState } from "react"
import { toast } from "sonner"
import { KeyRound, Loader2 } from "lucide-react"
import { currentToken, gw } from "@/lib/api"
import { useAuthStore } from "@/stores/auth"
import { useUIStore } from "@/stores/ui"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

/** Change account password dialog (POST /api/auth/password). */
export function PasswordDialog() {
  const open = useUIStore((s) => s.passwordOpen)
  const setOpen = useUIStore((s) => s.setPasswordOpen)

  const [current, setCurrent] = useState("")
  const [next, setNext] = useState("")
  const [confirm, setConfirm] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const reset = () => {
    setCurrent("")
    setNext("")
    setConfirm("")
    setError(null)
    setBusy(false)
  }

  const submit = async () => {
    if (busy) return
    if (next.length < 10) {
      setError("New password must be at least 10 characters.")
      return
    }
    if (next !== confirm) {
      setError("New passwords do not match.")
      return
    }
    setBusy(true)
    setError(null)
    try {
      // NOTE: raw fetch (not api()) — a 401 here means "wrong current
      // password", NOT an expired session, so it must not log the user out.
      const res = await fetch(gw("/api/auth/password"), {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${currentToken() ?? ""}`,
        },
        body: JSON.stringify({ currentPassword: current, newPassword: next }),
      })
      if (!res.ok) {
        let message = `Request failed (${res.status})`
        try {
          const data = (await res.json()) as { error?: string }
          if (data.error) message = data.error
        } catch {
          // ignore parse errors
        }
        setError(message)
        return
      }
      // Every other session was just revoked; the server hands this one a
      // replacement token so it stays signed in.
      try {
        const data = (await res.json()) as { token?: string }
        if (data.token) useAuthStore.setState({ token: data.token })
      } catch {
        // an older server answers 204; the existing token is still valid then
      }
      toast.success("Password changed", {
        description: "Other devices were signed out.",
      })
      setOpen(false)
      reset()
    } catch {
      setError("Network error — could not reach the server.")
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        setOpen(o)
        if (!o) reset()
      }}
    >
      <DialogContent aria-describedby={undefined} className="border-zinc-800 bg-zinc-950 text-zinc-100 sm:max-w-sm">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-base">
            <KeyRound className="size-4 text-emerald-400" aria-hidden="true" />
            Change password
          </DialogTitle>
        </DialogHeader>

        <form
          className="flex flex-col gap-3.5 pt-1"
          onSubmit={(e) => {
            e.preventDefault()
            submit()
          }}
        >
          <div className="grid gap-1.5">
            <Label htmlFor="pw-current" className="text-xs text-zinc-400">
              Current password
            </Label>
            <Input
              id="pw-current"
              type="password"
              autoComplete="current-password"
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
              required
              className="h-9 border-zinc-800 bg-zinc-900 focus-visible:ring-emerald-500/40"
            />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="pw-new" className="text-xs text-zinc-400">
              New password
            </Label>
            <Input
              id="pw-new"
              type="password"
              autoComplete="new-password"
              value={next}
              onChange={(e) => setNext(e.target.value)}
              required
              className="h-9 border-zinc-800 bg-zinc-900 focus-visible:ring-emerald-500/40"
            />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="pw-confirm" className="text-xs text-zinc-400">
              Confirm new password
            </Label>
            <Input
              id="pw-confirm"
              type="password"
              autoComplete="new-password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              required
              className="h-9 border-zinc-800 bg-zinc-900 focus-visible:ring-emerald-500/40"
            />
          </div>

          {error && (
            <p role="alert" className="rounded-md bg-red-950/60 px-3 py-2 text-xs text-red-300">
              {error}
            </p>
          )}

          <DialogFooter className="gap-2 pt-1">
            <Button
              type="button"
              variant="ghost"
              onClick={() => setOpen(false)}
              className="text-zinc-400 hover:text-zinc-100"
            >
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={busy || !current || !next || !confirm}
              className="bg-emerald-500 text-zinc-950 hover:bg-emerald-400"
            >
              {busy && <Loader2 className="size-4 animate-spin" />}
              Update password
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
