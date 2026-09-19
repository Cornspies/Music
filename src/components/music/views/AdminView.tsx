"use client"

import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"
import { Loader2, LogOut, Shield, ShieldCheck, Trash2, UserCog, Users } from "lucide-react"
import { cn } from "@/lib/utils"
import { admin } from "@/lib/api"
import { formatFileSize, formatRelativeDate } from "@/lib/format"
import type { AdminSettings, AdminUser } from "@/lib/types"
import { useAuthStore } from "@/stores/auth"
import { useLibraryStore } from "@/stores/library"
import { UserAvatar } from "@/components/music/UserAvatar"
import { EmptyState } from "@/components/music/EmptyState"
import { Button } from "@/components/ui/button"
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
  description: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <div className="flex items-start justify-between gap-4 rounded-xl border border-zinc-800/80 bg-zinc-900/50 px-4 py-3">
      <div className="min-w-0">
        <p className="text-sm font-medium text-zinc-100">{title}</p>
        <p className="mt-0.5 text-xs text-zinc-500">{description}</p>
      </div>
      <div className="shrink-0 pt-0.5">{children}</div>
    </div>
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
        <EmptyState icon={Shield} title="Admins only" subtitle="Ask the person who runs this server." compact />
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
                  <>Fixed by <code className="text-zinc-400">MUSIC_ALLOW_REGISTRATION</code> in the server&apos;s environment.</>
                ) : settings.inviteRequired ? (
                  "Anyone with the invite code can create an account."
                ) : (
                  "When on, anyone who finds this server's address can create an account and add to the shared library."
                )
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
                  ? `Uploads are capped at ${formatFileSize(settings.userQuotaBytes)} per account (MUSIC_MAX_BYTES_PER_USER).`
                  : "No cap. Set MUSIC_MAX_BYTES_PER_USER on the server to add one."
              }
            >
              <span className="text-xs text-zinc-500">{settings.transcodingAvailable ? "ffmpeg ✓" : "no ffmpeg"}</span>
            </SettingRow>
          </div>
        </section>
      ) : null}

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
                  className="flex items-center gap-3 border-b border-zinc-800/60 bg-zinc-900/30 px-3 py-2.5 last:border-b-0"
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
                        className="text-red-400 focus:text-red-300"
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
