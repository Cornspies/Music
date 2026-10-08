"use client"

import { Fragment, useCallback, useEffect, useState, type ReactNode } from "react"
import { invoke } from "@tauri-apps/api/core"
import { toast } from "sonner"
import { FolderOpen, Loader2, Settings } from "lucide-react"
import { useUIStore } from "@/stores/ui"
import { useOfflineStore } from "@/stores/offline"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"

interface AppSettings {
  closeToTray: boolean
  startMinimized: boolean
  autostart: boolean
  discord: boolean
  discordAvailable: boolean
  discordConnected: boolean
  downloadDir: string
  defaultDownloadDir: boolean
}

type Change = Partial<Pick<AppSettings, "closeToTray" | "startMinimized" | "autostart" | "discord">>

function message(err: unknown): string {
  return typeof err === "string" ? err : err instanceof Error ? err.message : "Something went wrong."
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex min-w-0 flex-col gap-3">
      <h3 className="text-[11px] font-semibold uppercase tracking-widest text-zinc-500">{title}</h3>
      {children}
    </section>
  )
}

function Toggle({
  id,
  label,
  hint,
  checked,
  disabled,
  onChange,
}: {
  id: string
  label: string
  hint?: string
  checked: boolean
  disabled?: boolean
  onChange: (on: boolean) => void
}) {
  return (
    <div className="flex items-start justify-between gap-4">
      <div>
        <Label htmlFor={id} className="text-sm font-medium text-zinc-200">
          {label}
        </Label>
        {hint ? <p className="mt-0.5 text-xs text-zinc-500">{hint}</p> : null}
      </div>
      <Switch
        id={id}
        checked={checked}
        disabled={disabled}
        onCheckedChange={onChange}
        className="mt-0.5 data-[state=checked]:bg-emerald-500"
      />
    </div>
  )
}

export function AppSettingsDialog() {
  const open = useUIStore((s) => s.appSettingsOpen)
  const setOpen = useUIStore((s) => s.setAppSettingsOpen)
  const [settings, setSettings] = useState<AppSettings | null>(null)
  const [moving, setMoving] = useState(false)

  const refresh = useCallback(() => {
    invoke<AppSettings>("settings_get")
      .then(setSettings)
      .catch((err: unknown) => toast.error(message(err)))
  }, [])

  useEffect(() => {
    if (open) refresh()
  }, [open, refresh])

  useEffect(() => {
    if (!open || !settings?.discord || settings.discordConnected) return
    const timer = window.setTimeout(refresh, 3000)
    return () => window.clearTimeout(timer)
  }, [open, settings, refresh])

  const change = (patch: Change) => {
    setSettings((s) => (s ? { ...s, ...patch } : s))
    invoke<AppSettings>("settings_set", { change: patch })
      .then(setSettings)
      .catch((err: unknown) => {
        toast.error(message(err))
        refresh()
      })
  }

  const move = (command: "settings_pick_downloads" | "settings_reset_downloads") => {
    setMoving(true)
    invoke<AppSettings | null>(command)
      .then((next) => {
        if (!next) return
        setSettings(next)
        void useOfflineStore.getState().init()
        toast.success("Downloads moved")
      })
      .catch((err: unknown) => toast.error(message(err)))
      .finally(() => setMoving(false))
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent aria-describedby={undefined} className="border-zinc-800 bg-zinc-950 text-zinc-100 sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-base">
            <Settings className="size-4 text-emerald-400" aria-hidden="true" />
            App settings
          </DialogTitle>
        </DialogHeader>

        {!settings ? (
          <div className="flex justify-center py-8">
            <Loader2 className="size-5 animate-spin text-zinc-500" />
          </div>
        ) : (
          <div className="flex min-w-0 flex-col gap-6 pt-1">
            <Section title="Startup">
              <Toggle
                id="autostart"
                label="Start when you sign in"
                checked={settings.autostart}
                onChange={(on) => change({ autostart: on })}
              />
              {settings.autostart ? (
                <Toggle
                  id="start-minimized"
                  label="Start in the tray"
                  checked={settings.startMinimized}
                  onChange={(on) => change({ startMinimized: on })}
                />
              ) : null}
            </Section>

            <Section title="Window">
              <Toggle
                id="close-to-tray"
                label="Minimize to tray when closed"
                hint={settings.closeToTray ? undefined : "Closing the window quits Music."}
                checked={settings.closeToTray}
                onChange={(on) => change({ closeToTray: on })}
              />
            </Section>

            <Section title="Downloads">
              <p className="rounded-md border border-zinc-800 bg-zinc-900/60 px-3 py-2 font-mono text-xs [overflow-wrap:anywhere] text-zinc-300">
                {settings.downloadDir.split(/(?<=[/\\])/).map((part, i) => (
                  <Fragment key={i}>
                    {part}
                    <wbr />
                  </Fragment>
                ))}
              </p>
              <div className="flex flex-wrap gap-2">
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => invoke("settings_open_downloads").catch((err: unknown) => toast.error(message(err)))}
                  className="bg-zinc-800 text-zinc-100 hover:bg-zinc-700"
                >
                  <FolderOpen className="size-4" /> Open folder
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={moving}
                  onClick={() => move("settings_pick_downloads")}
                  className="bg-zinc-800 text-zinc-100 hover:bg-zinc-700"
                >
                  {moving ? <Loader2 className="size-4 animate-spin" /> : null} Change…
                </Button>
                {!settings.defaultDownloadDir ? (
                  <Button
                    size="sm"
                    variant="ghost"
                    disabled={moving}
                    onClick={() => move("settings_reset_downloads")}
                    className="text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100"
                  >
                    Use default
                  </Button>
                ) : null}
              </div>
            </Section>

            <Section title="Discord">
              <Toggle
                id="discord"
                label="Show what you're playing"
                hint={
                  !settings.discordAvailable
                    ? "Not set up in this build."
                    : settings.discord
                      ? settings.discordConnected
                        ? "Connected to Discord."
                        : "Shows while music plays and Discord is open."
                      : undefined
                }
                checked={settings.discord}
                disabled={!settings.discordAvailable}
                onChange={(on) => change({ discord: on })}
              />
            </Section>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
