"use client"

import { useEffect, useState } from "react"
import { toast } from "sonner"
import { AudioLines, Loader2, LogIn, Ticket, UserPlus, Users } from "lucide-react"
import type { AuthConfig, InviteCheck } from "@/lib/types"
import { cn } from "@/lib/utils"
import {
  SERVER_PLACEHOLDER,
  SERVER_REFUSES_APP,
  insecureServerWarning,
  isDesktop,
  normalizeServer,
  serverAnswers,
  serverUrl,
  setServerUrl,
  usesDevProxy,
} from "@/lib/desktop"
import { useAuthStore } from "@/stores/auth"
import { pendingListenInvite } from "@/stores/listen"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

type Mode = "login" | "register"

function fromUrl(): { mode: Mode; invite: string } {
  if (typeof window === "undefined") return { mode: "login", invite: "" }
  const path = window.location.pathname.replace(/\/+$/, "")
  const invite = new URLSearchParams(window.location.search).get("invite")?.trim() ?? ""
  return { mode: path === "/register" || invite ? "register" : "login", invite }
}

function pathFor(mode: Mode, invite: string): string {
  if (mode === "register") return invite ? `/register?invite=${encodeURIComponent(invite)}` : "/register"
  return "/login"
}

function timeLeft(iso: string): string {
  const ms = new Date(iso).getTime() - Date.now()
  if (!Number.isFinite(ms) || ms <= 0) return "expired"
  const h = Math.floor(ms / 3_600_000)
  const m = Math.floor((ms % 3_600_000) / 60_000)
  if (h >= 48) return `${Math.floor(h / 24)} days left`
  if (h > 0) return `${h}h ${m}m left`
  return `${m} min left`
}

export function AuthView() {
  const login = useAuthStore((s) => s.login)
  const register = useAuthStore((s) => s.register)

  const [initial] = useState(fromUrl)
  const [mode, setModeState] = useState<Mode>(initial.mode)
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [confirm, setConfirm] = useState("")
  const [inviteCode, setInviteCode] = useState(initial.invite)
  const [busy, setBusy] = useState(false)
  const [authConfig, setAuthConfig] = useState<AuthConfig | null>(null)
  const [inviteCheck, setInviteCheck] = useState<InviteCheck | null>(null)
  const [server, setServer] = useState(serverUrl)
  const [activeServer, setActiveServer] = useState(serverUrl)
  const [acceptInsecure, setAcceptInsecure] = useState(false)
  const [serverProblem, setServerProblem] = useState<string | null>(null)
  const choosesServer = isDesktop && !usesDevProxy
  const normalizedServer = choosesServer ? normalizeServer(server) : null
  const insecureWarning = normalizedServer ? insecureServerWarning(normalizedServer) : null
  const viaLink = Boolean(initial.invite)
  const privacyHref = isDesktop ? (activeServer ? `${activeServer}/privacy` : null) : "/privacy"
  const [listenInvite] = useState(
    () => typeof window !== "undefined" && (/^\/listen\/[^/]+/.test(window.location.pathname) || pendingListenInvite() !== null)
  )

  const commitServer = () => {
    if (!normalizedServer) return
    setServer(normalizedServer)
    if (normalizedServer !== activeServer) {
      setActiveServer(normalizedServer)
      setAuthConfig(null)
      setServerProblem(null)
    }
  }

  const setMode = (next: Mode) => {
    setModeState(next)
    window.history.replaceState(null, "", pathFor(next, next === "register" ? initial.invite : ""))
  }

  useEffect(() => {
    if (!initial.invite || (choosesServer && !activeServer)) return
    let cancelled = false
    fetch(`${activeServer}/api/auth/invite/${encodeURIComponent(initial.invite)}`)
      .then((r) => (r.ok ? r.json() : null))
      .then((check: InviteCheck | null) => {
        if (!cancelled && check) setInviteCheck(check)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [initial.invite, activeServer, choosesServer])

  useEffect(() => {
    if (choosesServer && !activeServer) return
    let cancelled = false
    fetch(`${activeServer}/api/auth/config`)
      .then((r) => (r.ok ? r.json() : null))
      .then((cfg: AuthConfig | null) => {
        if (cancelled) return
        setServerProblem(null)
        if (cfg) setAuthConfig(cfg)
      })
      .catch(async () => {
        if (!isDesktop) return
        const answers = await serverAnswers(activeServer)
        if (!cancelled) setServerProblem(answers ? SERVER_REFUSES_APP : "Can't reach this server.")
      })
    return () => {
      cancelled = true
    }
  }, [activeServer, choosesServer])

  const minPassword = authConfig?.minPasswordLength ?? 8
  const linkValid = viaLink && inviteCheck?.valid !== false
  const registrationOpen = (authConfig?.registrationOpen ?? true) || linkValid
  const inviteRequired = (authConfig?.inviteRequired ?? false) && !linkValid

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    const uname = username.trim()
    if (choosesServer && !normalizedServer) {
      toast.error("Enter the address of your Music server")
      return
    }
    if (insecureWarning && !acceptInsecure) {
      toast.error(insecureWarning)
      return
    }
    if (!uname || !password) {
      toast.error("Please fill in all fields")
      return
    }
    if (password.length < minPassword) {
      toast.error(`Password must be at least ${minPassword} characters`)
      return
    }
    if (mode === "register" && inviteRequired && !inviteCode.trim()) {
      toast.error("An invite code is required to sign up on this server")
      return
    }
    if (mode === "register" && password !== confirm) {
      toast.error("Passwords don't match")
      return
    }
    setBusy(true)
    if (normalizedServer) {
      setServerUrl(normalizedServer)
      commitServer()
    }
    try {
      if (mode === "login") {
        await login(uname, password)
        toast.success(`Welcome back, ${uname}`)
      } else {
        await register(uname, password, inviteCode.trim() || undefined)
        toast.success(`Welcome to Music, ${uname}`)
      }
      window.history.replaceState(null, "", "/")
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
      <div
        aria-hidden="true"
        className="pointer-events-none absolute -bottom-48 -left-32 size-105 rounded-full bg-teal-600/10 blur-3xl"
      />
      <div
        aria-hidden="true"
        className="pointer-events-none absolute -right-32 top-1/3 size-90 rounded-full bg-amber-600/5 blur-3xl"
      />

      <Card className="relative w-full max-w-sm border-zinc-800/80 bg-zinc-900/80 shadow-2xl shadow-black/50 backdrop-blur">
        <CardHeader className="items-center pb-2 text-center">
          <div className="mb-2 flex size-14 items-center justify-center rounded-2xl bg-linear-to-br from-emerald-400 to-teal-600 shadow-lg shadow-emerald-900/40">
            <AudioLines className="size-7 text-zinc-950" aria-hidden="true" />
          </div>
          <CardTitle className="text-2xl font-bold tracking-tight">
            {mode === "login" ? "Sign in" : "Create your account"}
          </CardTitle>
        </CardHeader>

        <CardContent>
          {viaLink && mode === "register" ? (
            <div
              role="status"
              className={cn(
                "mb-4 flex items-start gap-2.5 rounded-lg border px-3 py-2.5 text-xs",
                inviteCheck?.valid === false
                  ? "border-red-900/60 bg-red-950/40 text-red-200"
                  : "border-emerald-900/60 bg-emerald-950/40 text-emerald-100"
              )}
            >
              <Ticket className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
              <div>
                {inviteCheck?.valid === false ? (
                  <>
                    <p className="font-medium">This invite link can&apos;t be used</p>
                    <p className="mt-0.5 opacity-80">
                      {inviteCheck.status === "used"
                        ? "It has already been used to create an account."
                        : inviteCheck.status === "expired"
                          ? "It has expired."
                          : inviteCheck.status === "revoked"
                            ? "It was revoked."
                            : "It isn't a valid link."}
                    </p>
                  </>
                ) : (
                  <>
                    <p className="font-medium">
                      {inviteCheck?.invitedBy ? `${inviteCheck.invitedBy} invited you to Music` : "You've been invited to Music"}
                    </p>
                    {inviteCheck?.expiresAt ? (
                      <p className="mt-0.5 opacity-80">This link works once · {timeLeft(inviteCheck.expiresAt)}.</p>
                    ) : null}
                  </>
                )}
              </div>
            </div>
          ) : listenInvite ? (
            <div
              role="status"
              className="mb-4 flex items-start gap-2.5 rounded-lg border border-emerald-900/60 bg-emerald-950/40 px-3 py-2.5 text-xs text-emerald-100"
            >
              <Users className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
              <p>Sign in to join the listening session you were invited to.</p>
            </div>
          ) : null}
          <form className="flex flex-col gap-4" onSubmit={submit}>
            {choosesServer ? (
              <div className="flex flex-col gap-2">
                <Label htmlFor="auth-server">Server</Label>
                <Input
                  id="auth-server"
                  inputMode="url"
                  autoComplete="url"
                  spellCheck={false}
                  placeholder={SERVER_PLACEHOLDER}
                  value={server}
                  onChange={(e) => {
                    setServer(e.target.value)
                    setAcceptInsecure(false)
                  }}
                  onBlur={commitServer}
                  className="border-zinc-700/80 bg-zinc-950/60"
                />
                {serverProblem ? <p className="text-xs text-red-300">{serverProblem}</p> : null}
                {insecureWarning ? (
                  <div className="rounded-lg border border-amber-900/60 bg-amber-950/40 px-3 py-2.5 text-xs text-amber-100">
                    <p>{insecureWarning}</p>
                    <label className="mt-2 flex items-center gap-2">
                      <input
                        type="checkbox"
                        checked={acceptInsecure}
                        onChange={(e) => setAcceptInsecure(e.target.checked)}
                        className="size-3.5 accent-amber-400"
                      />
                      Connect anyway
                    </label>
                  </div>
                ) : null}
              </div>
            ) : null}
            <div className="flex flex-col gap-2">
              <Label htmlFor="auth-username">Username</Label>
              <Input
                id="auth-username"
                autoComplete="username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                className="border-zinc-700/80 bg-zinc-950/60"
              />
            </div>
            <div className="flex flex-col gap-2">
              <div className="flex items-baseline justify-between gap-2">
                <Label htmlFor="auth-password">Password</Label>
                {mode === "login" ? (
                  <button
                    type="button"
                    onClick={() => toast("Ask an admin of this server for a password reset link.")}
                    className="text-xs text-zinc-500 hover:text-zinc-300 hover:underline focus-visible:underline focus-visible:outline-none"
                  >
                    Forgot password?
                  </button>
                ) : null}
              </div>
              <Input
                id="auth-password"
                type="password"
                autoComplete={mode === "login" ? "current-password" : "new-password"}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className="border-zinc-700/80 bg-zinc-950/60"
              />
            </div>
            {mode === "register" && (
              <div className="flex flex-col gap-2">
                <Label htmlFor="auth-confirm">Confirm password</Label>
                <Input
                  id="auth-confirm"
                  type="password"
                  autoComplete="new-password"
                  value={confirm}
                  onChange={(e) => setConfirm(e.target.value)}
                  className="border-zinc-700/80 bg-zinc-950/60"
                />
              </div>
            )}
            {mode === "register" && !viaLink && (inviteRequired || !(authConfig?.registrationOpen ?? true)) && (
              <div className="flex flex-col gap-2">
                <Label htmlFor="auth-invite">Invite code</Label>
                <Input
                  id="auth-invite"
                  type="text"
                  autoComplete="off"
                  value={inviteCode}
                  onChange={(e) => setInviteCode(e.target.value)}
                  className="border-zinc-700/80 bg-zinc-950/60"
                />
              </div>
            )}

            <Button
              type="submit"
              disabled={busy}
              className="mt-2 h-10 bg-emerald-500 text-zinc-950 font-semibold hover:bg-emerald-400"
            >
              {busy ? (
                <Loader2 className="size-4 animate-spin" />
              ) : mode === "login" ? (
                <LogIn className="size-4" />
              ) : (
                <UserPlus className="size-4" />
              )}
              {mode === "login" ? "Sign in" : "Create account"}
            </Button>
          </form>

          <p className="mt-4 text-center text-sm text-zinc-400">
            {!registrationOpen && mode === "login" ? (
              <>
                Sign-ups are closed on this server.{" "}
                <button
                  type="button"
                  onClick={() => setMode("register")}
                  className="font-medium text-emerald-400 hover:text-emerald-300 hover:underline"
                >
                  Have an invite code?
                </button>
              </>
            ) : (
              <>
            {mode === "login" ? "New to Music?" : "Already have an account?"}{" "}
            <button
              type="button"
              onClick={() => {
                setMode(mode === "login" ? "register" : "login")
                setConfirm("")
                if (!viaLink) setInviteCode("")
              }}
              className={cn(
                "font-medium text-emerald-400 hover:text-emerald-300 hover:underline",
                "focus-visible:outline-none focus-visible:underline"
              )}
            >
              {mode === "login" ? "Create an account" : "Sign in"}
            </button>
              </>
            )}
          </p>

          {privacyHref ? (
            <p className="mt-6 text-center text-xs">
              <a href={privacyHref} className="text-zinc-500 hover:text-zinc-300 hover:underline">
                Privacy policy
              </a>
            </p>
          ) : null}
        </CardContent>
      </Card>
    </div>
  )
}
