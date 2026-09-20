"use client"

import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"
import {
  Ban,
  Check,
  Copy,
  Link2,
  Loader2,
  LogOut,
  Plus,
  Shield,
  ShieldCheck,
  Ticket,
  Trash2,
  UserCog,
  Users,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { admin, inviteLink } from "@/lib/api"
import { formatFileSize, formatRelativeDate } from "@/lib/format"
import type { AdminSettings, AdminUser, Invite } from "@/lib/types"
import { useAuthStore } from "@/stores/auth"
import { useLibraryStore } from "@/stores/library"
import { UserAvatar } from "@/components/music/UserAvatar"
import { EmptyState } from "@/components/music/EmptyState"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

function SettingRow({
  title,
  description,
  children,
}: {
  title: string
  description?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <div className="flex items-start justify-between gap-4 rounded-xl border border-zinc-800/80 bg-zinc-900/50 px-4 py-3">
      <div className="min-w-0">
        <p className="text-sm font-medium text-zinc-100">{title}</p>
        {description ? <p className="mt-0.5 text-xs text-zinc-500">{description}</p> : null}
      </div>
      <div className="shrink-0 pt-0.5">{children}</div>
    </div>
  )
}

const EXPIRY_OPTIONS: { hours: number; label: string }[] = [
  { hours: 24, label: "24 hours" },
  { hours: 72, label: "3 days" },
  { hours: 168, label: "1 week" },
]

function inviteExpiry(iso: string): string {
  const ms = new Date(iso).getTime() - Date.now()
  if (!Number.isFinite(ms) || ms <= 0) return "expired"
  const h = Math.floor(ms / 3_600_000)
  if (h >= 48) return `expires in ${Math.floor(h / 24)} days`
  if (h >= 1) return `expires in ${h}h`
  return `expires in ${Math.max(1, Math.floor(ms / 60_000))} min`
}

/**
 * Single-use invite links: one per friend, valid for a day by default, with
 * the record of who used which. Copy the link and send it; the sign-up page
 * fills the code in itself.
 */
function InvitesSection() {
  const [invites, setInvites] = useState<Invite[] | null>(null)
  const [note, setNote] = useState("")
  const [hours, setHours] = useState(24)
  const [creating, setCreating] = useState(false)
  const [copied, setCopied] = useState<string | null>(null)
  const [busy, setBusy] = useState<string | null>(null)

  const load = useCallback(
    () =>
      admin
        .invites()
        .then(setInvites)
        .catch((e: Error) => toast.error(e.message)),
    []
  )
  useEffect(() => {
    load()
  }, [load])

  const copy = async (code: string) => {
    const url = inviteLink(code)
    try {
      await navigator.clipboard.writeText(url)
      setCopied(code)
      setTimeout(() => setCopied((c) => (c === code ? null : c)), 2000)
      toast.success("Invite link copied")
    } catch {
      // Clipboard blocked (http, permissions): show it so it can be selected.
      toast(url, { duration: 10_000 })
    }
  }

  const create = async () => {
    setCreating(true)
    try {
      const inv = await admin.createInvite({ note: note.trim() || undefined, expiresHours: hours })
      setInvites((list) => [inv, ...(list ?? [])])
      setNote("")
      await copy(inv.code)
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setCreating(false)
    }
  }

  const revoke = async (inv: Invite) => {
    setBusy(inv.code)
    try {
      const updated = await admin.revokeInvite(inv.code)
      setInvites((list) => (list ?? []).map((i) => (i.code === inv.code ? updated : i)))
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(null)
    }
  }

  const remove = async (inv: Invite) => {
    setBusy(inv.code)
    try {
      await admin.deleteInvite(inv.code)
      setInvites((list) => (list ?? []).filter((i) => i.code !== inv.code))
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(null)
    }
  }

  const statusStyle: Record<Invite["status"], string> = {
    pending: "bg-emerald-500/15 text-emerald-300",
    used: "bg-zinc-700/60 text-zinc-300",
    expired: "bg-amber-500/15 text-amber-300",
    revoked: "bg-red-500/15 text-red-300",
  }

  return (
    <section className="mt-8">
      <h2 className="mb-3 flex items-center gap-2 text-sm font-semibold text-zinc-300">
        <Ticket className="size-4" aria-hidden="true" />
        Invite links
      </h2>
      <form
        className="flex flex-col gap-2 rounded-xl border border-zinc-800/80 bg-zinc-900/50 p-3 sm:flex-row sm:items-center"
        onSubmit={(e) => {
          e.preventDefault()
          create()
        }}
      >
        <Input
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder="Who is it for? (e.g. Sam)"
          maxLength={80}
          className="h-9 border-zinc-800 bg-zinc-950/60 sm:flex-1"
        />
        <div role="radiogroup" aria-label="Link lifetime" className="flex gap-1">
          {EXPIRY_OPTIONS.map((o) => (
            <button
              key={o.hours}
              type="button"
              role="radio"
              aria-checked={hours === o.hours}
              onClick={() => setHours(o.hours)}
              className={cn(
                "rounded-md border px-2 py-1.5 text-xs font-medium transition-colors",
                hours === o.hours
                  ? "border-emerald-500/60 bg-emerald-500/15 text-emerald-300"
                  : "border-zinc-800 bg-zinc-900/60 text-zinc-400 hover:border-zinc-700 hover:text-zinc-200"
              )}
            >
              {o.label}
            </button>
          ))}
        </div>
        <Button type="submit" disabled={creating} className="h-9 bg-emerald-500 text-zinc-950 hover:bg-emerald-400">
          {creating ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
          Create &amp; copy link
        </Button>
      </form>

      {!invites ? (
        <div className="flex justify-center py-6 text-zinc-500">
          <Loader2 className="size-5 animate-spin" />
        </div>
      ) : invites.length === 0 ? (
        <p className="py-6 text-center text-xs text-zinc-600">No invite links yet.</p>
      ) : (
        <div role="list" className="mt-3 overflow-hidden rounded-xl border border-zinc-800/80">
          {invites.map((inv) => (
            <div
              key={inv.code}
              role="listitem"
              className="flex items-center gap-3 border-b border-zinc-800/60 bg-zinc-900/30 px-3 py-2.5"
            >
              <span
                className={cn("shrink-0 rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide", statusStyle[inv.status])}
              >
                {inv.status}
              </span>
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium text-zinc-100">
                  {inv.note || <span className="text-zinc-500">Unnamed link</span>}
                  <span className="ml-2 font-mono text-[11px] text-zinc-600">{inv.code.slice(0, 6)}…</span>
                </p>
                <p className="truncate text-xs text-zinc-500">
                  {inv.status === "used" && inv.usedBy
                    ? `used by ${inv.usedBy} ${inv.usedAt ? formatRelativeDate(inv.usedAt) : ""}`
                    : inv.status === "pending"
                      ? inviteExpiry(inv.expiresAt)
                      : inv.status === "revoked" && inv.revokedAt
                        ? `revoked ${formatRelativeDate(inv.revokedAt)}`
                        : `expired ${formatRelativeDate(inv.expiresAt)}`}
                  {inv.createdBy ? ` · by ${inv.createdBy} ${formatRelativeDate(inv.createdAt)}` : ""}
                </p>
              </div>
              {inv.status === "pending" ? (
                <>
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label="Copy invite link"
                    title="Copy invite link"
                    onClick={() => copy(inv.code)}
                    className="size-8 text-zinc-400 hover:text-emerald-300"
                  >
                    {copied === inv.code ? <Check className="size-4 text-emerald-400" /> : <Copy className="size-4" />}
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label="Revoke invite link"
                    title="Revoke"
                    disabled={busy === inv.code}
                    onClick={() => revoke(inv)}
                    className="size-8 text-zinc-400 hover:text-red-300"
                  >
                    <Ban className="size-4" />
                  </Button>
                </>
              ) : (
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label="Delete from list"
                  title="Delete from list"
                  disabled={busy === inv.code}
                  onClick={() => remove(inv)}
                  className="size-8 text-zinc-500 hover:text-red-300"
                >
                  <Trash2 className="size-4" />
                </Button>
              )}
            </div>
          ))}
        </div>
      )}
      <p className="mt-2 flex items-center gap-1.5 text-[11px] text-zinc-600">
        <Link2 className="size-3" aria-hidden="true" />
        Links look like {inviteLink("…")}
      </p>
    </section>
  )
}

/** Server administration: who has an account, and whether more may join. */
export function AdminView() {
  const me = useAuthStore((s) => s.user)
  const [users, setUsers] = useState<AdminUser[] | null>(null)
  const [settings, setSettings] = useState<AdminSettings | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [pendingDelete, setPendingDelete] = useState<AdminUser | null>(null)
  const [busy, setBusy] = useState<string | null>(null)

  const load = useCallback(
    () =>
      Promise.all([admin.users(), admin.settings()])
        .then(([u, s]) => {
          setUsers(u)
          setSettings(s)
          setError(null)
        })
        .catch((e: Error) => setError(e.message)),
    []
  )

  useEffect(() => {
    load()
  }, [load])

  if (me?.role !== "admin") {
    return (
      <div className="p-4 md:p-6">
        <EmptyState icon={Shield} title="Admins only" compact />
      </div>
    )
  }

  const toggleRegistration = async (open: boolean) => {
    try {
      setSettings(await admin.updateSettings({ registrationOpen: open }))
      toast.success(open ? "Registration is open" : "Registration is closed")
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  const setRole = async (u: AdminUser, role: "user" | "admin") => {
    setBusy(u.id)
    try {
      await admin.updateUser(u.id, { role })
      toast.success(`${u.username} is now ${role === "admin" ? "an admin" : "a regular user"}`)
      await load()
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(null)
    }
  }

  const signOut = async (u: AdminUser) => {
    setBusy(u.id)
    try {
      await admin.updateUser(u.id, { signOut: true })
      toast.success(`${u.username} has been signed out everywhere`)
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(null)
    }
  }

  const remove = async (u: AdminUser) => {
    setBusy(u.id)
    try {
      await admin.deleteUser(u.id)
      toast.success(`Deleted ${u.username} and their ${u.trackCount} upload${u.trackCount === 1 ? "" : "s"}`)
      await load()
      // Their tracks are gone from the shared library.
      useLibraryStore.getState().fetchAll().catch(() => {})
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(null)
      setPendingDelete(null)
    }
  }

  return (
    <div className="p-4 pb-16 md:p-6 md:pb-12">
      <p className="text-[11px] font-semibold uppercase tracking-widest text-zinc-500">Server</p>
      <h1 className="text-2xl font-bold tracking-tight text-zinc-50">Admin</h1>

      {error ? (
        <div className="mt-6">
          <EmptyState icon={Shield} title="Couldn't load admin data" subtitle={error} compact />
        </div>
      ) : null}

      {settings ? (
        <section className="mt-6">
          <h2 className="mb-3 text-sm font-semibold text-zinc-300">Access</h2>
          <div className="flex flex-col gap-3">
            <SettingRow
              title="Open registration"
              description={
                settings.registrationPinned ? (
                  <>Set by <code className="text-zinc-400">MUSIC_ALLOW_REGISTRATION</code> on the server.</>
                ) : undefined
              }
            >
              <Switch
                checked={settings.registrationOpen}
                disabled={settings.registrationPinned}
                onCheckedChange={toggleRegistration}
                className="data-[state=checked]:bg-emerald-500"
              />
            </SettingRow>
            <SettingRow
              title="Storage per user"
              description={
                settings.userQuotaBytes > 0
                  ? `${formatFileSize(settings.userQuotaBytes)} per account`
                  : "No cap (MUSIC_MAX_BYTES_PER_USER)"
              }
            >
              <span className="text-xs text-zinc-500">{settings.transcodingAvailable ? "ffmpeg ✓" : "no ffmpeg"}</span>
            </SettingRow>
          </div>
        </section>
      ) : null}

      <InvitesSection />

      <section className="mt-8">
        <h2 className="mb-3 flex items-center gap-2 text-sm font-semibold text-zinc-300">
          <Users className="size-4" aria-hidden="true" />
          Users {users ? <span className="text-zinc-600">· {users.length}</span> : null}
        </h2>
        {!users ? (
          <div className="flex justify-center py-10 text-zinc-500">
            <Loader2 className="size-5 animate-spin" />
          </div>
        ) : (
          <div role="list" className="overflow-hidden rounded-xl border border-zinc-800/80">
            {users.map((u) => {
              const isMe = u.id === me.id
              return (
                <div
                  key={u.id}
                  role="listitem"
                  className="flex items-center gap-3 border-b border-zinc-800/60 bg-zinc-900/30 px-3 py-2.5"
                >
                  <UserAvatar userId={u.id} username={u.username} hasAvatar={u.hasAvatar} size={32} />
                  <div className="min-w-0 flex-1">
                    <p className="flex items-center gap-1.5 truncate text-sm font-medium text-zinc-100">
                      {u.username}
                      {u.role === "admin" ? (
                        <ShieldCheck className="size-3.5 text-emerald-400" aria-label="Admin" />
                      ) : null}
                      {isMe ? <span className="text-xs text-zinc-500">(you)</span> : null}
                    </p>
                    <p className="truncate text-xs text-zinc-500">
                      {u.trackCount} upload{u.trackCount === 1 ? "" : "s"} · {formatFileSize(u.bytesUsed)} ·{" "}
                      {u.lastPlayedAt ? `listened ${formatRelativeDate(u.lastPlayedAt)}` : "never listened"} · joined{" "}
                      {formatRelativeDate(u.createdAt)}
                    </p>
                  </div>
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label={`Manage ${u.username}`}
                        disabled={busy === u.id}
                        className={cn("size-8 text-zinc-400 hover:text-zinc-100")}
                      >
                        {busy === u.id ? <Loader2 className="size-4 animate-spin" /> : <UserCog className="size-4" />}
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" className="border-zinc-800 bg-zinc-950 text-zinc-200">
                      {u.role === "admin" ? (
                        <DropdownMenuItem disabled={isMe} onClick={() => setRole(u, "user")}>
                          <Shield className="size-4" /> Remove admin
                        </DropdownMenuItem>
                      ) : (
                        <DropdownMenuItem onClick={() => setRole(u, "admin")}>
                          <ShieldCheck className="size-4" /> Make admin
                        </DropdownMenuItem>
                      )}
                      <DropdownMenuItem onClick={() => signOut(u)}>
                        <LogOut className="size-4" /> Sign out everywhere
                      </DropdownMenuItem>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem
                        disabled={isMe}
                        onClick={() => setPendingDelete(u)}
                        className="text-red-400"
                      >
                        <Trash2 className="size-4" /> Delete account…
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              )
            })}
          </div>
        )}
      </section>

      <AlertDialog open={pendingDelete !== null} onOpenChange={(o) => !o && setPendingDelete(null)}>
        <AlertDialogContent className="border-zinc-800 bg-zinc-950 text-zinc-100">
          <AlertDialogHeader>
            <AlertDialogTitle>Delete {pendingDelete?.username}?</AlertDialogTitle>
            <AlertDialogDescription className="text-zinc-400">
              Their account, playlists and {pendingDelete?.trackCount ?? 0} uploaded track
              {pendingDelete?.trackCount === 1 ? "" : "s"} ({formatFileSize(pendingDelete?.bytesUsed ?? 0)}) are removed
              from the shared library for everyone. This can&apos;t be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel className="border-zinc-800 bg-transparent text-zinc-300 hover:bg-zinc-800">
              Cancel
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={() => pendingDelete && remove(pendingDelete)}
              className="bg-red-600 text-white hover:bg-red-500"
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
