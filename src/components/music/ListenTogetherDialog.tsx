"use client"

import { useEffect, useState } from "react"
import { toast } from "sonner"
import { Check, Copy, Crown, Loader2, LogOut, Minus, Music2, Plus, Share2, Trash2, Users, X } from "lucide-react"
import { cn } from "@/lib/utils"
import { listen as listenApi, listenLink } from "@/lib/api"
import { copyText } from "@/lib/clipboard"
import type { ListenInvite, ListenState } from "@/lib/types"
import { useAuthStore } from "@/stores/auth"
import { usePlayerStore } from "@/stores/player"
import {
  MAX_LISTEN_GUESTS,
  forgetListenInvite,
  joinListening,
  leaveListening,
  removeListener,
  restoreListening,
  startListening,
  updateListening,
  useListenStore,
} from "@/stores/listen"
import { UserAvatar } from "@/components/music/UserAvatar"
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
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"

export function ListenTogetherDialog() {
  const open = useListenStore((s) => s.dialogOpen)
  const setOpen = useListenStore((s) => s.setDialogOpen)
  const session = useListenStore((s) => s.session)
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent className="border-zinc-800 bg-zinc-950 text-zinc-100 sm:max-w-md">
        {session ? <SessionPanel session={session} /> : <StartPanel />}
      </DialogContent>
    </Dialog>
  )
}

function Title({ children }: { children: React.ReactNode }) {
  return (
    <DialogTitle className="flex items-center gap-2 text-base">
      <Users className="size-4 text-emerald-400" aria-hidden="true" />
      {children}
    </DialogTitle>
  )
}

function GuestStepper({ value, onChange, disabled }: { value: number; onChange: (n: number) => void; disabled?: boolean }) {
  return (
    <div className="flex items-center gap-1">
      <Button
        variant="ghost"
        size="icon"
        aria-label="Fewer people"
        disabled={disabled || value <= 1}
        onClick={() => onChange(value - 1)}
        className="size-8 rounded-md border border-zinc-800 text-zinc-300"
      >
        <Minus className="size-3.5" />
      </Button>
      <span className="w-8 text-center text-sm font-medium tabular-nums" aria-live="polite">
        {value}
      </span>
      <Button
        variant="ghost"
        size="icon"
        aria-label="More people"
        disabled={disabled || value >= MAX_LISTEN_GUESTS}
        onClick={() => onChange(value + 1)}
        className="size-8 rounded-md border border-zinc-800 text-zinc-300"
      >
        <Plus className="size-3.5" />
      </Button>
    </div>
  )
}

function StartPanel() {
  const [capacity, setCapacity] = useState(5)
  const [everyone, setEveryone] = useState(false)
  const [busy, setBusy] = useState(false)
  const playing = usePlayerStore((s) => s.currentTrack)

  const start = async () => {
    setBusy(true)
    try {
      await startListening({ capacity, everyoneCanControl: everyone })
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <DialogHeader>
        <Title>Listen together</Title>
        <DialogDescription className="text-xs text-zinc-500">
          Share a link, and everyone who joins hears the same music as you, at the same moment. Pausing only
          pauses it for you. They need an account on this server.
        </DialogDescription>
      </DialogHeader>

      <div className="flex flex-col gap-5 pt-1">
        <div className="flex items-center justify-between gap-4">
          <div>
            <p className="text-sm font-medium text-zinc-200">People who can join</p>
            <p className="text-xs text-zinc-500">Not counting you</p>
          </div>
          <GuestStepper value={capacity} onChange={setCapacity} />
        </div>
        <div className="flex items-start justify-between gap-4">
          <div>
            <Label htmlFor="listen-everyone" className="text-sm font-medium text-zinc-200">
              Everyone can add songs and skip
            </Label>
            <p className="text-xs text-zinc-500">Otherwise only you choose what plays.</p>
          </div>
          <Switch
            id="listen-everyone"
            checked={everyone}
            onCheckedChange={setEveryone}
            className="mt-0.5 data-[state=checked]:bg-emerald-500"
          />
        </div>
        <p className="text-xs text-zinc-500">
          {playing ? `Starts with “${playing.title}” and what's queued after it.` : "Starts empty; add songs once it's going."}
        </p>
      </div>

      <DialogFooter>
        <Button onClick={start} disabled={busy} className="bg-emerald-500 text-zinc-950 hover:bg-emerald-400">
          {busy ? <Loader2 className="size-4 animate-spin" /> : <Users className="size-4" />}
          Start session
        </Button>
      </DialogFooter>
    </>
  )
}

function SessionPanel({ session }: { session: ListenState }) {
  const me = useAuthStore((s) => s.user?.id)
  const upcoming = useListenStore((s) => s.entries.length - (s.entries.findIndex((e) => e.id === session.current) + 1))
  const [busy, setBusy] = useState(false)
  const [clearOpen, setClearOpen] = useState(false)
  const guests = session.members.length - 1

  const leave = async () => {
    setBusy(true)
    await leaveListening()
    setBusy(false)
    useListenStore.getState().setDialogOpen(false)
  }

  return (
    <>
      <DialogHeader>
        <Title>Listen together</Title>
        <DialogDescription className="text-xs text-zinc-500">
          {session.isHost
            ? `You're hosting · ${guests} of ${session.capacity} joined`
            : `Hosted by ${session.host.username}`}
        </DialogDescription>
      </DialogHeader>

      {session.isHost && session.code ? <InviteLink code={session.code} /> : null}

      <section aria-label="People listening" className="flex flex-col gap-1">
        {session.members.map((m) => (
          <div key={m.id} className="flex items-center gap-3 rounded-lg px-1 py-1.5">
            <span className="relative">
              <UserAvatar userId={m.id} username={m.username} hasAvatar={m.hasAvatar} size={28} />
              <span
                aria-hidden="true"
                className={cn(
                  "absolute -right-0.5 -bottom-0.5 size-2.5 rounded-full ring-2 ring-zinc-950",
                  m.online ? "bg-emerald-400" : "bg-zinc-600"
                )}
              />
            </span>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm text-zinc-100">
                {m.username}
                {m.id === me ? <span className="text-zinc-500"> (you)</span> : null}
              </span>
              <span className="block text-[11px] text-zinc-500">
                {m.isHost ? "Host · " : ""}
                {m.online ? "Listening" : "Away"}
              </span>
            </span>
            {m.isHost ? <Crown className="size-4 text-amber-300/80" aria-label="Host" /> : null}
            {session.isHost && !m.isHost ? (
              <Button
                variant="ghost"
                size="icon"
                aria-label={`Remove ${m.username}`}
                title={`Remove ${m.username}`}
                onClick={() => void removeListener(m.id)}
                className="size-7 text-zinc-500 hover:text-red-400"
              >
                <X className="size-4" />
              </Button>
            ) : null}
          </div>
        ))}
      </section>

      {session.isHost ? (
        <div className="flex flex-col gap-4 border-t border-zinc-800/80 pt-4">
          <div className="flex items-center justify-between gap-4">
            <p className="text-sm font-medium text-zinc-200">People who can join</p>
            <GuestStepper
              value={session.capacity}
              onChange={(capacity) => void updateListening({ capacity })}
            />
          </div>
          <div className="flex items-start justify-between gap-4">
            <Label htmlFor="listen-everyone-live" className="text-sm font-medium text-zinc-200">
              Everyone can add songs and skip
            </Label>
            <Switch
              id="listen-everyone-live"
              checked={session.everyoneCanControl}
              onCheckedChange={(everyoneCanControl) => void updateListening({ everyoneCanControl })}
              className="mt-0.5 data-[state=checked]:bg-emerald-500"
            />
          </div>
          <div className="flex items-center justify-between gap-4">
            <div>
              <p className="text-sm font-medium text-zinc-200">Queue</p>
              <p className="text-xs text-zinc-500">
                {upcoming} {upcoming === 1 ? "song" : "songs"} up next
              </p>
            </div>
            <Button
              variant="ghost"
              size="sm"
              disabled={upcoming === 0}
              onClick={() => setClearOpen(true)}
              className="text-red-400 hover:bg-red-500/10 hover:text-red-300"
            >
              <Trash2 className="size-3.5" /> Clear queue
            </Button>
          </div>
          <p className="text-xs text-zinc-500">Pausing only pauses it for you. Skipping moves everyone on.</p>
          <ClearQueueConfirm open={clearOpen} onOpenChange={setClearOpen} count={upcoming} />
        </div>
      ) : (
        <p className="border-t border-zinc-800/80 pt-4 text-xs text-zinc-500">
          {session.everyoneCanControl
            ? `${session.host.username} lets everyone add songs and skip. Pausing only pauses it for you.`
            : `${session.host.username} chooses what plays. Pausing only pauses it for you.`}
        </p>
      )}

      <DialogFooter>
        <Button
          variant="ghost"
          onClick={leave}
          disabled={busy}
          className="text-red-400 hover:bg-red-500/10 hover:text-red-300"
        >
          {busy ? <Loader2 className="size-4 animate-spin" /> : <LogOut className="size-4" />}
          {session.isHost ? "End session" : "Leave"}
        </Button>
      </DialogFooter>
    </>
  )
}

/** Asks the host before clearing the session's queue for everyone. */
export function ClearQueueConfirm({
  open,
  onOpenChange,
  count,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  count: number
}) {
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Clear the queue?</AlertDialogTitle>
          <AlertDialogDescription>
            {count === 1 ? "The song" : `The ${count} songs`} after the one playing now{" "}
            {count === 1 ? "is" : "are"} removed for everyone in the session. What's playing carries on.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            className="bg-red-600 text-white hover:bg-red-500"
            onClick={() => usePlayerStore.getState().clearQueue()}
          >
            Clear queue
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

function InviteLink({ code }: { code: string }) {
  const [copied, setCopied] = useState(false)
  const url = listenLink(code)
  const canShare = typeof navigator !== "undefined" && typeof navigator.share === "function"

  const copy = async () => {
    if (await copyText(url)) {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
      toast.success("Invite link copied")
    } else {
      toast.error(`Couldn't copy automatically. The link is: ${url}`)
    }
  }

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center gap-2">
        <input
          readOnly
          value={url}
          onFocus={(e) => e.currentTarget.select()}
          aria-label="Invite link"
          className="min-w-0 flex-1 rounded-md border border-zinc-800 bg-zinc-900/60 px-3 py-2 text-xs text-zinc-200 outline-none focus:border-emerald-500/60"
        />
        <Button size="sm" onClick={copy} className="bg-zinc-100 text-zinc-900 hover:bg-white">
          {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
          {copied ? "Copied" : "Copy"}
        </Button>
        {canShare ? (
          <Button
            size="icon"
            variant="ghost"
            aria-label="Share invite link"
            onClick={() => navigator.share({ title: "Listen together", url }).catch(() => {})}
            className="size-8 text-zinc-300"
          >
            <Share2 className="size-4" />
          </Button>
        ) : null}
      </div>
      <p className="text-[11px] text-zinc-500">Anyone with an account here can join with this link.</p>
    </div>
  )
}

/** The prompt an invite link opens. */
export function ListenInviteDialog() {
  const code = useListenStore((s) => s.invite)
  const close = () => {
    forgetListenInvite()
    useListenStore.getState().setInvite(null)
  }
  return (
    <Dialog open={code !== null} onOpenChange={(o) => !o && close()}>
      {code ? <InviteBody key={code} code={code} onClose={close} /> : null}
    </Dialog>
  )
}

function InviteBody({ code, onClose }: { code: string; onClose: () => void }) {
  const current = useListenStore((s) => s.session)
  const [invite, setInvite] = useState<ListenInvite | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let alive = true
    listenApi
      .invite(code)
      .then((i) => alive && setInvite(i))
      .catch((e: Error) => alive && setError(e.message))
    return () => {
      alive = false
    }
  }, [code])

  const alreadyHere = invite !== null && current?.id === invite.id

  const join = async () => {
    setBusy(true)
    try {
      if (invite?.joined && !alreadyHere) await restoreListening()
      else if (!alreadyHere) await joinListening(code)
      onClose()
      toast.success(`You're listening with ${invite?.host.username ?? "the host"}`)
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <DialogContent className="border-zinc-800 bg-zinc-950 text-zinc-100 sm:max-w-sm">
      <DialogHeader>
        <Title>Listen together</Title>
        <DialogDescription className="text-xs text-zinc-500">
          {invite
            ? `${invite.host.username} invited you to listen along.`
            : error
              ? "This invite can't be used."
              : "Looking up the invite…"}
        </DialogDescription>
      </DialogHeader>

      {error ? (
        <p className="text-sm text-red-400">{error.charAt(0).toUpperCase() + error.slice(1)}.</p>
      ) : !invite ? (
        <div className="flex justify-center py-6">
          <Loader2 className="size-5 animate-spin text-zinc-500" />
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          <div className="flex items-center gap-3">
            <UserAvatar
              userId={invite.host.id}
              username={invite.host.username}
              hasAvatar={invite.host.hasAvatar}
              size={36}
            />
            <div className="min-w-0">
              <p className="truncate text-sm font-medium text-zinc-100">{invite.host.username}&apos;s session</p>
              <p className="text-xs text-zinc-500">
                {invite.guests} of {invite.capacity} joined
              </p>
            </div>
          </div>
          {invite.nowPlaying ? (
            <p className="flex items-center gap-2 truncate text-xs text-zinc-400">
              <Music2 className="size-3.5 shrink-0 text-emerald-400" aria-hidden="true" />
              <span className="truncate">
                {invite.nowPlaying.title}
                {invite.nowPlaying.artist ? ` · ${invite.nowPlaying.artist}` : ""}
              </span>
            </p>
          ) : null}
          {invite.full && !invite.joined ? (
            <p className="text-sm text-amber-300">This session is full.</p>
          ) : current && !alreadyHere ? (
            <p className="text-xs text-amber-300/90">
              {current.isHost ? "Joining ends the session you're hosting." : "You'll leave the session you're in now."}
            </p>
          ) : null}
        </div>
      )}

      <DialogFooter className="gap-2">
        <Button variant="ghost" onClick={onClose} className="text-zinc-300">
          Not now
        </Button>
        <Button
          onClick={join}
          disabled={!invite || busy || (invite.full && !invite.joined)}
          className="bg-emerald-500 text-zinc-950 hover:bg-emerald-400"
        >
          {busy ? <Loader2 className="size-4 animate-spin" /> : <Users className="size-4" />}
          {alreadyHere || invite?.joined ? "Open" : "Join"}
        </Button>
      </DialogFooter>
    </DialogContent>
  )
}
