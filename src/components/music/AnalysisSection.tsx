"use client"

import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"
import { AudioLines, Loader2 } from "lucide-react"
import { admin } from "@/lib/api"
import type { AnalysisSettings, TrimRule } from "@/lib/types"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
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

type Fields = { thresholdDb: string; minSeconds: string; marginSeconds: string }

const toFields = (r: TrimRule): Fields => ({
  thresholdDb: String(r.thresholdDb),
  minSeconds: String(r.minSeconds),
  marginSeconds: String(r.marginSeconds),
})

// Mirrors the server's checks, which have the last word.
function parse(f: Fields): TrimRule | string {
  const rule = { thresholdDb: Number(f.thresholdDb), minSeconds: Number(f.minSeconds), marginSeconds: Number(f.marginSeconds) }
  if (Object.values(f).some((v) => v.trim() === "") || Object.values(rule).some((v) => !Number.isFinite(v))) {
    return "Enter a number in every field."
  }
  if (rule.thresholdDb < -90 || rule.thresholdDb > -20) return "Silence level: -90 to -20 dB."
  if (rule.minSeconds < 0.1 || rule.minSeconds > 10) return "Shortest silence: 0.1 to 10 seconds."
  if (rule.marginSeconds < 0 || rule.marginSeconds >= rule.minSeconds) return "Margin: 0 up to the shortest silence."
  return rule
}

const FIELDS: { key: keyof Fields; label: string; unit: string; step: string }[] = [
  { key: "thresholdDb", label: "Silence level", unit: "dB", step: "1" },
  { key: "minSeconds", label: "Shortest silence", unit: "s", step: "0.1" },
  { key: "marginSeconds", label: "Margin kept", unit: "s", step: "0.01" },
]

export function AnalysisSection() {
  const [status, setStatus] = useState<AnalysisSettings | null>(null)
  const [fields, setFields] = useState<Fields | null>(null)
  const [trim, setTrim] = useState(true)
  const [loudness, setLoudness] = useState(true)
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)

  const apply = useCallback((st: AnalysisSettings) => {
    setStatus(st)
    setFields(toFields(st.rule))
    if (!st.trimEnabled) setTrim(false)
  }, [])

  useEffect(() => {
    admin
      .analysis()
      .then(apply)
      .catch((e: Error) => toast.error(e.message))
  }, [apply])

  if (!status || !fields) return null

  const parsed = parse(fields)
  const invalid = typeof parsed === "string" ? parsed : null
  const changed = !invalid && JSON.stringify(parsed) !== JSON.stringify(status.rule)
  const runTrim = trim && status.trimEnabled

  const submit = async (rerun: boolean) => {
    if (typeof parsed === "string") return
    setBusy(true)
    try {
      apply(await admin.reanalyze({ rule: parsed, trim: rerun && runTrim, loudness: rerun && loudness }))
      toast.success(rerun ? "Re-check started" : "Saved")
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="mt-8">
      <h2 className="mb-3 flex items-center gap-2 text-sm font-semibold text-zinc-300">
        <AudioLines className="size-4" aria-hidden="true" />
        Silence and loudness
      </h2>

      {!status.analysisEnabled ? (
        <p className="text-xs text-zinc-500">
          Off: needs ffmpeg, and <code className="text-zinc-400">MUSIC_DISABLE_ANALYSIS</code> must not be 1.
        </p>
      ) : (
        <div className="flex flex-col gap-4 rounded-xl border border-zinc-800/80 bg-zinc-900/50 px-4 py-4">
          <div className="grid gap-3 sm:grid-cols-3">
            {FIELDS.map((f) => (
              <div key={f.key} className="flex flex-col gap-1.5">
                <Label htmlFor={`trim-${f.key}`} className="text-xs text-zinc-400">
                  {f.label} ({f.unit})
                </Label>
                <Input
                  id={`trim-${f.key}`}
                  type="number"
                  inputMode="decimal"
                  step={f.step}
                  value={fields[f.key]}
                  onChange={(e) => setFields({ ...fields, [f.key]: e.target.value })}
                  className="h-9 border-zinc-800 bg-zinc-950/60"
                />
              </div>
            ))}
          </div>
          <p className="text-xs text-zinc-500">
            Silence below the level, lasting at least the shortest silence, is cut from the start and end of new
            tracks, keeping the margin.{" "}
            <button
              type="button"
              onClick={() => setFields(toFields(status.default))}
              className="text-zinc-400 underline-offset-2 hover:text-zinc-200 hover:underline"
            >
              Defaults
            </button>
          </p>
          {invalid ? <p className="text-xs text-red-400">{invalid}</p> : null}

          <div className="flex flex-wrap items-center gap-x-6 gap-y-3">
            <label className="flex items-center gap-2 text-sm text-zinc-200">
              <Switch
                checked={runTrim}
                disabled={!status.trimEnabled}
                onCheckedChange={setTrim}
                className="data-[state=checked]:bg-emerald-500"
              />
              Trim silence
              {!status.trimEnabled ? (
                <span className="text-xs text-zinc-500">
                  (off: <code>MUSIC_DISABLE_AUTO_TRIM</code>)
                </span>
              ) : null}
            </label>
            <label className="flex items-center gap-2 text-sm text-zinc-200">
              <Switch checked={loudness} onCheckedChange={setLoudness} className="data-[state=checked]:bg-emerald-500" />
              Loudness and waveform
            </label>
          </div>

          <div className="flex flex-wrap gap-2">
            <Button
              onClick={() => setConfirming(true)}
              disabled={busy || !!invalid || (!runTrim && !loudness)}
              className="h-9 bg-emerald-500 font-semibold text-zinc-950 hover:bg-emerald-400"
            >
              {busy ? <Loader2 className="size-4 animate-spin" /> : null} Re-check library
            </Button>
            <Button
              variant="ghost"
              onClick={() => submit(false)}
              disabled={busy || !changed}
              className="h-9 text-zinc-300 hover:bg-zinc-800"
            >
              Save for new tracks only
            </Button>
          </div>
        </div>
      )}

      <AlertDialog open={confirming} onOpenChange={setConfirming}>
        <AlertDialogContent className="border-zinc-800 bg-zinc-950 text-zinc-100">
          <AlertDialogHeader>
            <AlertDialogTitle>Re-check all {status.tracks} tracks?</AlertDialogTitle>
            <AlertDialogDescription className="text-zinc-400">
              {runTrim ? (
                <>
                  Cutting silence is permanent: no original is kept, so a gentler setting can&apos;t bring back what was
                  already cut.
                  {status.manuallyTrimmed > 0
                    ? ` ${status.manuallyTrimmed} track${status.manuallyTrimmed === 1 ? "" : "s"} trimmed by hand keep their edges.`
                    : ""}{" "}
                </>
              ) : null}
              Progress shows under Activity.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel className="border-zinc-800 bg-transparent text-zinc-300 hover:bg-zinc-800">
              Cancel
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={() => submit(true)}
              className="bg-emerald-500 font-semibold text-zinc-950 hover:bg-emerald-400"
            >
              Re-check
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  )
}
