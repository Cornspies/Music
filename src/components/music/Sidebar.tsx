"use client"

import { toast } from "sonner"
import {
  AudioLines,
  CloudDownload,
  Disc3,
  Heart,
  Compass,
  History,
  Home,
  Keyboard,
  UserRound,
  ListMusic,
  LogOut,
  Mic2,
  Music4,
  Pin,
  Plus,
  Search,
  ShieldCheck,
  Smartphone,
  SquareLibrary,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { logoutEverywhere } from "@/lib/api"
import type { View } from "@/stores/ui"
import { useUIStore } from "@/stores/ui"
import { useAuthStore } from "@/stores/auth"
import { useLibraryStore } from "@/stores/library"
import { useOfflineStore } from "@/stores/offline"
import { AvatarPicker } from "@/components/music/AvatarPicker"
import { UserAvatar } from "@/components/music/UserAvatar"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

interface NavItem {
  view: View
  label: string
  icon: typeof Home
}

const NAV_ITEMS: NavItem[] = [
  { view: "home", label: "Home", icon: Home },
  { view: "search", label: "Search", icon: Search },
  { view: "library", label: "Your library", icon: SquareLibrary },
  { view: "albums", label: "Albums", icon: Disc3 },
  { view: "artists", label: "Artists", icon: Mic2 },
  { view: "playlists", label: "Playlists", icon: ListMusic },
  { view: "favorites", label: "Favorites", icon: Heart },
  { view: "history", label: "History", icon: History },
  { view: "discover", label: "Discover", icon: Compass },
  { view: "offline", label: "Downloads", icon: CloudDownload },
]

export function SidebarContent() {
  const view = useUIStore((s) => s.view)
  const viewArg = useUIStore((s) => s.viewArg)
  const navigate = useUIStore((s) => s.navigate)
  const openPlaylistDialog = useUIStore((s) => s.openPlaylistDialog)
  const playlists = useLibraryStore((s) => s.playlists)
  const user = useAuthStore((s) => s.user)
  const logout = useAuthStore((s) => s.logout)
  const downloadCount = useOfflineStore((s) => s.tracks.length)

  const signOutEverywhere = async () => {
    try {
      await logoutEverywhere()
      toast.success("Signed out on every device")
    } catch (e) {
      toast.error((e as Error).message)
      return
    }
    logout()
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center gap-3 px-5 pb-4 pt-5">
        <div className="flex size-9 shrink-0 items-center justify-center rounded-xl bg-linear-to-br from-emerald-400 to-teal-600 shadow-md shadow-emerald-900/40">
          <AudioLines className="size-5 text-zinc-950" aria-hidden="true" />
        </div>
        <div>
          <p className="text-lg font-bold leading-tight tracking-tight">Music</p>
        </div>
      </div>

      <nav aria-label="Main" className="flex flex-col gap-0.5 px-3">
        {NAV_ITEMS.map((item) => {
          const active = view === item.view
          return (
            <button
              key={item.view}
              type="button"
              onClick={() => navigate(item.view)}
              aria-current={active ? "page" : undefined}
              className={cn(
                "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
                active
                  ? "bg-zinc-800/80 text-emerald-400"
                  : "text-zinc-400 hover:bg-zinc-800/50 hover:text-zinc-100"
              )}
            >
              <item.icon className="size-4.5 shrink-0" aria-hidden="true" />
              {item.label}
              {item.view === "offline" && downloadCount > 0 ? (
                <span className="ml-auto rounded-full bg-zinc-800 px-1.5 text-[10px] tabular-nums text-zinc-400">
                  {downloadCount}
                </span>
              ) : null}
            </button>
          )
        })}
        {user?.role === "admin" ? (
          <button
            type="button"
            onClick={() => navigate("admin")}
            aria-current={view === "admin" ? "page" : undefined}
            className={cn(
              "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
              view === "admin"
                ? "bg-zinc-800/80 text-emerald-400"
                : "text-zinc-400 hover:bg-zinc-800/50 hover:text-zinc-100"
            )}
          >
            <ShieldCheck className="size-4.5 shrink-0" aria-hidden="true" />
            Admin
          </button>
        ) : null}
      </nav>

      <div className="mt-5 flex min-h-0 flex-1 flex-col px-3">
        <div className="flex items-center justify-between px-2 pb-2">
          <h3 className="text-[11px] font-semibold uppercase tracking-widest text-zinc-500">
            Playlists
          </h3>
          <Button
            variant="ghost"
            size="icon"
            aria-label="New playlist"
            title="New playlist"
            onClick={() => openPlaylistDialog({ mode: "create" })}
            className="size-7 text-zinc-500 hover:text-emerald-400"
          >
            <Plus className="size-4" />
          </Button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto pb-2">
          {playlists.length === 0 ? (
            <button
              type="button"
              onClick={() => openPlaylistDialog({ mode: "create" })}
              className="flex w-full items-center gap-3 rounded-lg border border-dashed border-zinc-800 px-3 py-2.5 text-left text-xs text-zinc-500 transition-colors hover:border-zinc-600 hover:text-zinc-300"
            >
              <Music4 className="size-4 shrink-0" aria-hidden="true" />
              Create your first playlist
            </button>
          ) : (
            playlists.map((p) => {
              const active = view === "playlist" && viewArg === p.id
              return (
                <button
                  key={p.id}
                  type="button"
                  onClick={() => navigate("playlist", p.id)}
                  aria-current={active ? "page" : undefined}
                  className={cn(
                    "flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left transition-colors",
                    active
                      ? "bg-zinc-800/80 text-zinc-100"
                      : "text-zinc-400 hover:bg-zinc-800/50 hover:text-zinc-100"
                  )}
                >
                  <div className="relative shrink-0">
                    <div
                      aria-hidden="true"
                      className="flex size-8 items-center justify-center rounded-md bg-linear-to-br from-zinc-700 to-zinc-800"
                    >
                      <Music4 className="size-3.5 text-zinc-400" />
                    </div>
                    {!p.isMine && p.owner?.id ? (
                      <UserAvatar
                        userId={p.owner.id}
                        username={p.owner.username}
                        hasAvatar={p.owner.hasAvatar}
                        size={14}
                        label={`Shared by ${p.owner.username}`}
                        className="absolute -right-1 -bottom-1 ring-2 ring-zinc-950"
                      />
                    ) : null}
                  </div>
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm">{p.name}</p>
                    <p className="truncate text-[11px] text-zinc-600">
                      {p.isMine ? "" : `${p.owner?.username ?? "shared"} · `}
                      {p.trackCount} track{p.trackCount === 1 ? "" : "s"}
                    </p>
                  </div>
                  {!p.isMine ? (
                    <Pin
                      className="size-3 shrink-0 text-emerald-400/70"
                      aria-label="Pinned"
                    />
                  ) : null}
                </button>
              )
            })
          )}
        </div>
      </div>

      <div className="border-t border-zinc-800/60 px-3 py-3">
        <div className="flex items-center gap-3">
          <AvatarPicker />
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm font-medium text-zinc-200">
              {user?.username ?? "listener"}
            </p>
          </div>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                aria-label="Sign out"
                title="Sign out"
                className="size-8 text-zinc-500 hover:text-red-400"
              >
                <LogOut className="size-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="border-zinc-800 bg-zinc-950 text-zinc-200">
              <DropdownMenuItem onClick={() => logout()}>
                <LogOut className="size-4" /> Sign out here
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={signOutEverywhere} className="text-red-400">
                <ShieldCheck className="size-4" /> Sign out everywhere
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
        <div className="mt-2 flex gap-2">
          <button
            type="button"
            onClick={() => useUIStore.getState().setShortcutsOpen(true)}
            className="flex flex-1 items-center justify-center gap-1.5 rounded-md border border-zinc-800/80 bg-zinc-900/40 py-1.5 text-[11px] font-medium text-zinc-500 transition-colors hover:border-zinc-700 hover:text-zinc-200"
          >
            <Keyboard className="size-3" aria-hidden="true" /> Shortcuts
          </button>
          <button
            type="button"
            onClick={() => useUIStore.getState().setAccountOpen(true)}
            className="flex flex-1 items-center justify-center gap-1.5 rounded-md border border-zinc-800/80 bg-zinc-900/40 py-1.5 text-[11px] font-medium text-zinc-500 transition-colors hover:border-zinc-700 hover:text-zinc-200"
          >
            <UserRound className="size-3" aria-hidden="true" /> Account
          </button>
        </div>
        <button
          type="button"
          onClick={() => useUIStore.getState().setAndroidAppOpen(true)}
          className="mt-2 flex w-full items-center justify-center gap-1.5 rounded-md border border-zinc-800/80 bg-zinc-900/40 py-1.5 text-[11px] font-medium text-zinc-500 transition-colors hover:border-zinc-700 hover:text-zinc-200"
        >
          <Smartphone className="size-3" aria-hidden="true" /> Android app
        </button>
      </div>
    </div>
  )
}
