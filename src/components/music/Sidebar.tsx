"use client"

import { useMemo, useState } from "react"
import { toast } from "sonner"
import {
  CloudDownload,
  Heart,
  Compass,
  History,
  Home,
  Keyboard,
  UserRound,
  ListMusic,
  LogOut,
  Music4,
  PanelLeftClose,
  PanelLeftOpen,
  Pin,
  PinOff,
  Plus,
  Search,
  Settings,
  MonitorSmartphone,
  MoreHorizontal,
  ShieldCheck,
  SquareLibrary,
  Upload,
  Users,
  AudioLines,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { logoutEverywhere } from "@/lib/api"
import { isDesktop } from "@/lib/desktop"
import type { LibraryPin } from "@/lib/types"
import type { View } from "@/stores/ui"
import { useUIStore } from "@/stores/ui"
import { useAuthStore } from "@/stores/auth"
import { useLibraryStore } from "@/stores/library"
import { useOfflineStore } from "@/stores/offline"
import { useListenStore } from "@/stores/listen"
import { ArtistImage } from "@/components/music/ArtistImage"
import { AvatarPicker } from "@/components/music/AvatarPicker"
import { CoverArt } from "@/components/music/CoverArt"
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
  { view: "playlists", label: "Playlists", icon: ListMusic },
  { view: "favorites", label: "Favorites", icon: Heart },
  { view: "history", label: "History", icon: History },
  { view: "discover", label: "Discover", icon: Compass },
  { view: "offline", label: "Downloads", icon: CloudDownload },
]

interface Tip {
  label: string
  top: number
  left: number
}

type TipProps = {
  onMouseEnter?: (e: React.MouseEvent<HTMLElement>) => void
  onMouseLeave?: () => void
  onFocus?: (e: React.FocusEvent<HTMLElement>) => void
  onBlur?: () => void
}

function useRailTips(enabled: boolean) {
  const [tip, setTip] = useState<Tip | null>(null)
  const show = (label: string, el: HTMLElement) => {
    const r = el.getBoundingClientRect()
    setTip({ label, top: r.top + r.height / 2, left: r.right + 10 })
  }
  const props = (label: string): TipProps =>
    enabled
      ? {
          onMouseEnter: (e) => show(label, e.currentTarget),
          onMouseLeave: () => setTip(null),
          onFocus: (e) => show(label, e.currentTarget),
          onBlur: () => setTip(null),
        }
      : {}
  const node =
    enabled && tip ? (
      <div
        role="tooltip"
        style={{ top: tip.top, left: tip.left }}
        className="pointer-events-none fixed z-50 -translate-y-1/2 rounded-md border border-zinc-700/80 bg-zinc-800 px-2 py-1 text-xs font-medium whitespace-nowrap text-zinc-100 shadow-lg shadow-black/40"
      >
        {tip.label}
      </div>
    ) : null
  return { props, node, hide: () => setTip(null) }
}

function NavButton({
  icon: Icon,
  label,
  active,
  onClick,
  collapsed,
  tip,
  badge,
  tone = "default",
}: {
  icon: typeof Home
  label: string
  active?: boolean
  onClick: () => void
  collapsed: boolean
  tip: TipProps
  badge?: React.ReactNode
  tone?: "default" | "live"
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-current={active ? "page" : undefined}
      aria-label={collapsed ? label : undefined}
      {...tip}
      className={cn(
        "relative flex items-center rounded-lg text-sm font-medium transition-colors",
        collapsed ? "mx-auto size-11 justify-center" : "gap-3 px-3 py-2",
        active
          ? "bg-zinc-800/80 text-emerald-400"
          : tone === "live"
            ? "text-emerald-400 hover:bg-zinc-800/50"
            : "text-zinc-400 hover:bg-zinc-800/50 hover:text-zinc-100"
      )}
    >
      <Icon className="size-4.5 shrink-0" aria-hidden="true" />
      {collapsed ? (
        badge ? <span className="absolute top-2 right-2 size-1.5 rounded-full bg-emerald-400" aria-hidden="true" /> : null
      ) : (
        <>
          {label}
          {badge}
        </>
      )}
    </button>
  )
}

function pinKey(p: Pick<LibraryPin, "kind" | "name" | "artist">) {
  return `${p.kind}\n${p.name}\n${p.artist}`
}

export function SidebarContent({ collapsed = false, collapsible = false }: { collapsed?: boolean; collapsible?: boolean }) {
  const view = useUIStore((s) => s.view)
  const viewArg = useUIStore((s) => s.viewArg)
  const navigate = useUIStore((s) => s.navigate)
  const openPlaylistDialog = useUIStore((s) => s.openPlaylistDialog)
  const playlists = useLibraryStore((s) => s.playlists)
  const pins = useLibraryStore((s) => s.pins)
  const tracks = useLibraryStore((s) => s.tracks)
  const user = useAuthStore((s) => s.user)
  const logout = useAuthStore((s) => s.logout)
  const downloadCount = useOfflineStore((s) => s.tracks.length)
  const listeners = useListenStore((s) => s.session?.members.length ?? 0)
  const tips = useRailTips(collapsed)

  const albumCovers = useMemo(() => {
    const wanted = new Set(pins.filter((p) => p.kind === "album").map((p) => `${p.name}\n${p.artist}`))
    const out = new Map<string, string>()
    if (wanted.size === 0) return out
    for (const t of tracks) {
      const key = `${t.album}\n${t.artist}`
      if (t.hasCover && wanted.has(key) && !out.has(key)) out.set(key, t.id)
    }
    return out
  }, [pins, tracks])

  const go = (target: View, arg?: string) => {
    tips.hide()
    navigate(target, arg)
  }

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

  const openUpload = () => {
    useUIStore.getState().setSidebarOpen(false)
    useUIStore.getState().setUploadOpen(true)
  }

  const setCollapsed = (value: boolean) => {
    tips.hide()
    useUIStore.getState().setSidebarCollapsed(value)
  }

  const albumArg = (p: LibraryPin) => JSON.stringify({ name: p.name, artist: p.artist })
  const pinActive = (p: LibraryPin) =>
    p.kind === "artist" ? view === "artist" && viewArg === p.name : view === "album" && viewArg === albumArg(p)

  const footerIcon = "flex size-9 items-center justify-center rounded-md text-zinc-500 transition-colors hover:bg-zinc-800/60 hover:text-zinc-200"

  return (
    <div className="flex h-full min-h-0 flex-col">
      {collapsed ? (
        <div className="flex shrink-0 flex-col items-center gap-1 pt-3 pb-1">
          <button
            type="button"
            onClick={() => setCollapsed(false)}
            aria-label="Expand sidebar"
            {...tips.props("Expand sidebar")}
            className="flex size-11 items-center justify-center rounded-lg text-zinc-400 transition-colors hover:bg-zinc-800/50 hover:text-zinc-100"
          >
            <PanelLeftOpen className="size-4.5" />
          </button>
          <button
            type="button"
            onClick={openUpload}
            aria-label="Upload"
            {...tips.props("Upload")}
            className="flex size-11 items-center justify-center rounded-lg text-zinc-400 transition-colors hover:bg-zinc-800/50 hover:text-zinc-100"
          >
            <Upload className="size-4.5" />
          </button>
        </div>
      ) : (
        <div className="flex shrink-0 items-center gap-1 px-5 pt-4 pb-2">
          <AudioLines className="mr-auto size-5 text-emerald-400" aria-label="Music" />
          <Button
            variant="ghost"
            size="sm"
            onClick={openUpload}
            className="h-8 gap-1.5 px-2.5 text-xs text-zinc-400 hover:bg-zinc-800/60 hover:text-zinc-100"
          >
            <Upload className="size-3.5" aria-hidden="true" /> Upload
          </Button>
          {collapsible ? (
            <Button
              variant="ghost"
              size="icon"
              aria-label="Collapse sidebar"
              title="Collapse sidebar"
              onClick={() => setCollapsed(true)}
              className="-mr-2 size-8 text-zinc-500 hover:bg-zinc-800/60 hover:text-zinc-100"
            >
              <PanelLeftClose className="size-4" />
            </Button>
          ) : null}
        </div>
      )}

      <div className={cn("flex min-h-0 flex-1 flex-col overflow-x-hidden overflow-y-auto", collapsed && "scrollbar-none!")}>
        <nav aria-label="Main" className={cn("flex shrink-0 flex-col gap-0.5", collapsed ? "px-2" : "px-3")}>
          {NAV_ITEMS.map((item) => (
            <NavButton
              key={item.view}
              icon={item.icon}
              label={item.label}
              collapsed={collapsed}
              tip={tips.props(item.label)}
              active={
                view === item.view ||
                (item.view === "discover" && view === "catalog") ||
                (item.view === "home" && view === "mix")
              }
              onClick={() => go(item.view)}
              badge={
                item.view === "offline" && downloadCount > 0 ? (
                  <span className="ml-auto rounded-full bg-zinc-800 px-1.5 text-[10px] text-zinc-400 tabular-nums">
                    {downloadCount}
                  </span>
                ) : undefined
              }
            />
          ))}
          <NavButton
            icon={Users}
            label="Listen together"
            collapsed={collapsed}
            tip={tips.props(listeners ? `Listen together · ${listeners} live` : "Listen together")}
            tone={listeners ? "live" : "default"}
            onClick={() => {
              tips.hide()
              useUIStore.getState().setSidebarOpen(false)
              useListenStore.getState().setDialogOpen(true)
            }}
            badge={
              listeners ? (
                <span className="ml-auto rounded-full bg-emerald-500/15 px-1.5 text-[10px] text-emerald-300 tabular-nums">
                  {listeners} live
                </span>
              ) : undefined
            }
          />
          {user?.role === "admin" ? (
            <NavButton
              icon={ShieldCheck}
              label="Admin"
              collapsed={collapsed}
              tip={tips.props("Admin")}
              active={view === "admin"}
              onClick={() => go("admin")}
            />
          ) : null}
        </nav>

        <div className={cn("mt-2 flex min-h-36 flex-1 flex-col", collapsed ? "px-2" : "px-3")}>
          {collapsed ? (
            <div className="mx-auto mb-1 flex flex-col items-center gap-1 border-t border-zinc-800/70 pt-2">
              <button
                type="button"
                onClick={() => openPlaylistDialog({ mode: "create" })}
                aria-label="New playlist"
                {...tips.props("New playlist")}
                className="flex size-11 items-center justify-center rounded-lg text-zinc-500 transition-colors hover:bg-zinc-800/50 hover:text-emerald-400"
              >
                <Plus className="size-4.5" />
              </button>
            </div>
          ) : (
            <div className="flex items-center justify-between border-t border-zinc-800/70 px-2 pt-2 pb-1">
              <h3 className="text-[11px] font-semibold tracking-widest text-zinc-500 uppercase">Pins</h3>
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
          )}
          <div className={cn("min-h-0 flex-1 overflow-x-hidden overflow-y-auto pb-2", collapsed && "flex flex-col items-center gap-1 scrollbar-none!")}>
            {pins.map((p) => {
              const active = pinActive(p)
              const label = p.kind === "artist" ? p.name : `${p.name} · ${p.artist}`
              const art =
                p.kind === "artist" ? (
                  <ArtistImage name={p.name} size={128} sizes="40px" initialsClassName="text-[11px]" />
                ) : (
                  <CoverArt
                    trackId={albumCovers.get(`${p.name}\n${p.artist}`) ?? `${p.name}|${p.artist}`}
                    title={p.name}
                    hasCover={albumCovers.has(`${p.name}\n${p.artist}`)}
                    size={128}
                    sizes="40px"
                    initialsClassName="text-[10px] md:text-[10px]"
                  />
                )
              const open = () => (p.kind === "artist" ? go("artist", p.name) : go("album", albumArg(p)))
              if (collapsed) {
                return (
                  <button
                    key={pinKey(p)}
                    type="button"
                    onClick={open}
                    aria-label={label}
                    aria-current={active ? "page" : undefined}
                    {...tips.props(label)}
                    className={cn(
                      "flex size-11 shrink-0 items-center justify-center rounded-lg transition-colors",
                      active ? "bg-zinc-800/80" : "hover:bg-zinc-800/50"
                    )}
                  >
                    <span className={cn("block size-9", p.kind === "artist" && "overflow-hidden rounded-full")}>{art}</span>
                  </button>
                )
              }
              return (
                <div
                  key={pinKey(p)}
                  className={cn(
                    "group flex items-center rounded-lg transition-colors",
                    active ? "bg-zinc-800/80 text-zinc-100" : "text-zinc-400 hover:bg-zinc-800/50 hover:text-zinc-100"
                  )}
                >
                  <button
                    type="button"
                    onClick={open}
                    aria-current={active ? "page" : undefined}
                    className="flex min-w-0 flex-1 items-center gap-3 px-3 py-1.5 text-left"
                  >
                    <span className={cn("block size-8 shrink-0", p.kind === "artist" && "overflow-hidden rounded-full")}>{art}</span>
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-sm">{p.name}</span>
                      <span className="block truncate text-[11px] text-zinc-600">
                        {p.kind === "artist" ? "Artist" : `Album · ${p.artist || "Unknown artist"}`}
                      </span>
                    </span>
                  </button>
                  <button
                    type="button"
                    onClick={() => useLibraryStore.getState().togglePin(p.kind, p.name, p.artist)}
                    aria-label={`Unpin ${p.name}`}
                    title="Unpin"
                    className="mr-1.5 rounded p-1.5 text-zinc-500 opacity-0 transition-opacity group-hover:opacity-100 hover:text-zinc-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
                  >
                    <PinOff className="size-3.5" />
                  </button>
                </div>
              )
            })}

            {playlists.length === 0 && pins.length === 0 && !collapsed ? (
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
                const icon = (
                  <div className="relative shrink-0">
                    <div
                      aria-hidden="true"
                      className={cn(
                        "flex items-center justify-center rounded-md bg-linear-to-br from-zinc-700 to-zinc-800",
                        collapsed ? "size-9" : "size-8"
                      )}
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
                )
                if (collapsed) {
                  return (
                    <button
                      key={p.id}
                      type="button"
                      onClick={() => go("playlist", p.id)}
                      aria-label={p.name}
                      aria-current={active ? "page" : undefined}
                      {...tips.props(p.name)}
                      className={cn(
                        "flex size-11 shrink-0 items-center justify-center rounded-lg transition-colors",
                        active ? "bg-zinc-800/80" : "hover:bg-zinc-800/50"
                      )}
                    >
                      {icon}
                    </button>
                  )
                }
                return (
                  <button
                    key={p.id}
                    type="button"
                    onClick={() => go("playlist", p.id)}
                    aria-current={active ? "page" : undefined}
                    className={cn(
                      "flex w-full items-center gap-3 rounded-lg px-3 py-1.5 text-left transition-colors",
                      active ? "bg-zinc-800/80 text-zinc-100" : "text-zinc-400 hover:bg-zinc-800/50 hover:text-zinc-100"
                    )}
                  >
                    {icon}
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm">{p.name}</p>
                      <p className="truncate text-[11px] text-zinc-600">
                        {p.isMine ? "" : `${p.owner?.username ?? "shared"} · `}
                        {p.trackCount} track{p.trackCount === 1 ? "" : "s"}
                      </p>
                    </div>
                    {!p.isMine ? <Pin className="size-3 shrink-0 text-emerald-400/70" aria-label="Pinned" /> : null}
                  </button>
                )
              })
            )}
          </div>
        </div>
      </div>

      {collapsed ? (
        <div className="flex shrink-0 flex-col items-center gap-1 border-t border-zinc-800/60 py-3">
          <AvatarPicker />
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <button type="button" aria-label="More" {...tips.props("Account and more")} className={footerIcon}>
                <MoreHorizontal className="size-4" />
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent side="right" align="end" className="border-zinc-800 bg-zinc-950 text-zinc-200">
              <DropdownMenuItem onClick={() => useUIStore.getState().setAccountOpen(true)}>
                <UserRound className="size-4" /> Account
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => useUIStore.getState().setShortcutsOpen(true)}>
                <Keyboard className="size-4" /> Shortcuts
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => useUIStore.getState().setAppsOpen(true)}>
                <MonitorSmartphone className="size-4" /> Other platforms
              </DropdownMenuItem>
              {isDesktop ? (
                <DropdownMenuItem onClick={() => useUIStore.getState().setAppSettingsOpen(true)}>
                  <Settings className="size-4" /> Settings
                </DropdownMenuItem>
              ) : null}
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={() => logout()}>
                <LogOut className="size-4" /> Sign out here
              </DropdownMenuItem>
              <DropdownMenuItem onClick={signOutEverywhere} className="text-red-400">
                <ShieldCheck className="size-4" /> Sign out everywhere
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      ) : (
        <div className="shrink-0 border-t border-zinc-800/60 px-3 py-3">
          <div className="flex items-center gap-3">
            <AvatarPicker />
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-medium text-zinc-200">{user?.username ?? "listener"}</p>
            </div>
            <SignOutMenu
              onEverywhere={signOutEverywhere}
              onHere={() => logout()}
              tip={{}}
              className="flex size-8 items-center justify-center rounded-md text-zinc-500 transition-colors hover:bg-zinc-800/60 hover:text-red-400"
            />
          </div>
          <div className="mt-2 flex gap-1.5">
            {[
              { label: "Account", icon: UserRound, open: () => useUIStore.getState().setAccountOpen(true) },
              { label: "Shortcuts", icon: Keyboard, open: () => useUIStore.getState().setShortcutsOpen(true) },
              { label: "Other platforms", icon: MonitorSmartphone, open: () => useUIStore.getState().setAppsOpen(true) },
              ...(isDesktop
                ? [{ label: "Settings", icon: Settings, open: () => useUIStore.getState().setAppSettingsOpen(true) }]
                : []),
            ].map((b) => (
              <button
                key={b.label}
                type="button"
                onClick={b.open}
                aria-label={b.label}
                title={b.label}
                className="flex flex-1 items-center justify-center rounded-md border border-zinc-800/80 bg-zinc-900/40 py-1.5 text-zinc-500 transition-colors hover:border-zinc-700 hover:text-zinc-200"
              >
                <b.icon className="size-3.5" aria-hidden="true" />
              </button>
            ))}
          </div>
        </div>
      )}
      {tips.node}
    </div>
  )
}

function SignOutMenu({
  onHere,
  onEverywhere,
  tip,
  className,
}: {
  onHere: () => void
  onEverywhere: () => void
  tip: TipProps
  className: string
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button type="button" aria-label="Sign out" {...tip} className={className}>
          <LogOut className="size-4" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" side="right" className="border-zinc-800 bg-zinc-950 text-zinc-200">
        <DropdownMenuItem onClick={onHere}>
          <LogOut className="size-4" /> Sign out here
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={onEverywhere} className="text-red-400">
          <ShieldCheck className="size-4" /> Sign out everywhere
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
