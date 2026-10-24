"use client"

import { useEffect, useState } from "react"
import { toast } from "sonner"
import { AudioLines, KeyRound, Loader2 } from "lucide-react"
import type { PasswordResetCheck, User } from "@/lib/types"
import { apiUrl } from "@/lib/desktop"
import { useAuthStore } from "@/stores/auth"
import { useLibraryStore } from "@/stores/library"
import { usePlayerStore } from "@/stores/player"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

export function resetCodeFromUrl(): string | null {
  if (typeof window === "undefined") return null
  if (!/^\/reset-password\/?$/.test(window.location.pathname)) return null
  return new URLSearchParams(window.location.search).get("code")?.trim() ?? ""
}

export function ResetPasswordView({ code, onDone }: { code: string; onDone: () => void }) {
  const [check, setCheck] = useState<PasswordResetCheck | null>(code ? null : { valid: false })
  const [password, setPassword] = useState("")
  const [confirm, setConfirm] = useState("")
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!code) return
    if (window.location.search) {
      window.history.replaceState(null, "", "/reset-password")
    }
    let cancelled = false
    fetch(apiUrl(`/api/auth/reset/${encodeURIComponent(code)}`))
      .then((r) => (r.ok ? r.json() : { valid: false }))
      .then((c: PasswordResetCheck) => {
        if (!cancelled) setCheck(c)
      })
      .catch(() => {
        if (!cancelled) setCheck({ valid: false })
      })
    return () => {
      cancelled = true
    }
  }, [code])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (password.length < 8) {
      toast.error("Password must be at least 8 characters")
      return
    }
    if (password !== confirm) {
      toast.error("Passwords don't match")
      return
    }
    setBusy(true)
    try {
      const res = await fetch(apiUrl("/api/auth/reset"), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ code, password }),
      })
      const data = (await res.json().catch(() => ({}))) as { token?: string; user?: User; error?: string }
      if (!res.ok || !data.token || !data.user) throw new Error(data.error || `Request failed (${res.status})`)
      if (useAuthStore.getState().user?.id !== data.user.id) {
        usePlayerStore.getState().stop()
        useLibraryStore.getState().reset()
      }
      useAuthStore.setState({ token: data.token, user: data.user })
      toast.success("Password changed")
      window.history.replaceState(null, "", "/")
      onDone()
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="relative flex min-h-dvh items-center justify-center overflow-hidden bg-zinc-950 p-4">
      <div
        aria-hidden="true"
        className="pointer-events-none absolute -top-48 left-1/2 size-140 -translate-x-1/2 rounded-full bg-emerald-500/10 blur-3xl"
      />
      <Card className="relative w-full max-w-sm border-zinc-800/80 bg-zinc-900/80 shadow-2xl shadow-black/50 backdrop-blur">
        <CardHeader className="items-center pb-2 text-center">
          <div className="mb-2 flex size-14 items-center justify-center rounded-2xl bg-linear-to-br from-emerald-400 to-teal-600 shadow-lg shadow-emerald-900/40">
            <AudioLines className="size-7 text-zinc-950" aria-hidden="true" />
          </div>
          <CardTitle className="text-2xl font-bold tracking-tight">Set a new password</CardTitle>
        </CardHeader>
        <CardContent>
          {!check ? (
            <div className="flex justify-center py-6 text-zinc-500">
              <Loader2 className="size-5 animate-spin" />
            </div>
          ) : !check.valid ? (
            <div className="flex flex-col gap-4">
              <p
                role="status"
                className="rounded-lg border border-red-900/60 bg-red-950/40 px-3 py-2.5 text-xs text-red-200"
              >
                This reset link has expired or was already used.
              </p>
              <Button
                variant="ghost"
                onClick={() => {
                  window.history.replaceState(null, "", "/login")
                  onDone()
                }}
                className="text-zinc-300"
              >
                Back to sign in
              </Button>
            </div>
          ) : (
            <form className="flex flex-col gap-4" onSubmit={submit}>
              <p className="flex items-center gap-2 rounded-lg border border-emerald-900/60 bg-emerald-950/40 px-3 py-2.5 text-xs text-emerald-100">
                <KeyRound className="size-4 shrink-0" aria-hidden="true" />
                {check.username}
              </p>
              <input type="text" autoComplete="username" value={check.username ?? ""} readOnly hidden />
              <div className="flex flex-col gap-2">
                <Label htmlFor="reset-password">New password</Label>
                <Input
                  id="reset-password"
                  type="password"
                  autoComplete="new-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  className="border-zinc-700/80 bg-zinc-950/60"
                />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="reset-confirm">Confirm password</Label>
                <Input
                  id="reset-confirm"
                  type="password"
                  autoComplete="new-password"
                  value={confirm}
                  onChange={(e) => setConfirm(e.target.value)}
                  className="border-zinc-700/80 bg-zinc-950/60"
                />
              </div>
              <Button
                type="submit"
                disabled={busy}
                className="mt-2 h-10 bg-emerald-500 font-semibold text-zinc-950 hover:bg-emerald-400"
              >
                {busy ? <Loader2 className="size-4 animate-spin" /> : <KeyRound className="size-4" />}
                Save password
              </Button>
            </form>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
