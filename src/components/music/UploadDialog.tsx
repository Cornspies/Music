"use client"

import { useRef, useState } from "react"
import { toast } from "sonner"
import {
  CheckCircle2,
  CloudUpload,
  FileAudio,
  Loader2,
  Sparkles,
  XCircle,
  Copy,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { isDesktop } from "@/lib/desktop"
import {
  confirmPendingUpload,
  currentToken,
  discardPendingUpload,
  replaceWithPendingUpload,
  uploadUrl,
} from "@/lib/api"
import { formatFileSize, formatSizeChange, storedBytes } from "@/lib/format"
import type { DuplicateMatch, DuplicateResponse, Track } from "@/lib/types"
import { useAuthStore } from "@/stores/auth"
import { useUIStore } from "@/stores/ui"
import { useLibraryStore } from "@/stores/library"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Progress } from "@/components/ui/progress"
import { MetadataReviewDialog } from "@/components/music/MetadataReviewDialog"
import { MetadataQueueStatus } from "@/components/music/MetadataQueueStatus"
import { CoverArt } from "@/components/music/CoverArt"
import { formatDuration } from "@/lib/format"

type TaskStatus = "measuring" | "uploading" | "done" | "error" | "duplicate" | "skipped"

interface UploadTask {
  key: string
  name: string
  size: number
  progress: number
  status: TaskStatus
  message?: string
  duplicate?: DuplicateResponse
}

class DuplicateError extends Error {
  constructor(readonly response: DuplicateResponse) {
    super(response.error)
  }
}

const AUDIO_EXT = /\.(mp3|wav|flac|ogg|oga|m4a|aac|opus|weba|webm)$/i

function isAudioFile(file: File): boolean {
  return file.type.startsWith("audio/") || AUDIO_EXT.test(file.name)
}

const FORMATS: Record<string, string> = {
  "audio/mpeg": "MP3",
  "audio/mp4": "AAC",
  "audio/aac": "AAC",
  "audio/flac": "FLAC",
  "audio/wav": "WAV",
  "audio/ogg": "OGG",
  "audio/webm": "WebM",
}

const LOSSLESS = new Set(["audio/flac", "audio/wav"])

interface Quality {
  mimeType: string
  fileSize: number
  duration: number
}

function kbps(q: Quality): number {
  return q.duration > 0 && q.fileSize > 0 ? Math.round((q.fileSize * 8) / q.duration / 1000) : 0
}

function qualityLabel(q: Quality): string {
  const rate = kbps(q)
  return [FORMATS[q.mimeType] ?? q.mimeType.replace(/^audio\//, "").toUpperCase(), rate ? `${rate} kbps` : ""]
    .filter(Boolean)
    .join(" · ")
}

function betterThan(a: Quality, b: Quality): boolean {
  const lossless = LOSSLESS.has(a.mimeType)
  if (lossless !== LOSSLESS.has(b.mimeType)) return lossless
  return kbps(a) > kbps(b) * 1.15
}

function measureDuration(file: File): Promise<number | undefined> {
  if (isDesktop) return Promise.resolve(undefined)
  return new Promise((resolve) => {
    const url = URL.createObjectURL(file)
    const el = new Audio()
    let settled = false
    const finish = (d?: number) => {
      if (settled) return
      settled = true
      URL.revokeObjectURL(url)
      resolve(d)
    }
    el.preload = "metadata"
    el.onloadedmetadata = () => {
      finish(Number.isFinite(el.duration) && el.duration > 0 ? Math.round(el.duration * 100) / 100 : undefined)
    }
    el.onerror = () => finish(undefined)
    window.setTimeout(() => finish(undefined), 6000)
    el.src = url
  })
}

function uploadTrack(
  file: File,
  duration: number | undefined,
  onProgress: (pct: number) => void
): Promise<Track> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open("POST", uploadUrl())
    const token = currentToken()
    if (token) xhr.setRequestHeader("Authorization", `Bearer ${token}`)
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress(Math.round((e.loaded / e.total) * 100))
    }
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          resolve(JSON.parse(xhr.responseText) as Track)
        } catch {
          reject(new Error("Invalid server response"))
        }
      } else if (xhr.status === 409) {
        try {
          reject(new DuplicateError(JSON.parse(xhr.responseText) as DuplicateResponse))
        } catch {
          reject(new Error("Invalid server response"))
        }
      } else {
        let message = `Upload failed (${xhr.status})`
        try {
          const data = JSON.parse(xhr.responseText) as { error?: string }
          if (data?.error) message = data.error
        } catch {}
        reject(new Error(message))
      }
    }
    xhr.onerror = () => reject(new Error("Network error during upload"))
    const fd = new FormData()
    fd.append("file", file, file.name)
    if (duration !== undefined) fd.append("duration", String(duration))
    xhr.send(fd)
  })
}

export function UploadDialog() {
  const uploadOpen = useUIStore((s) => s.uploadOpen)
  const setUploadOpen = useUIStore((s) => s.setUploadOpen)

  const [tasks, setTasks] = useState<UploadTask[]>([])
  const [dragActive, setDragActive] = useState(false)
  const [busy, setBusy] = useState(false)
  const [uploadedTracks, setUploadedTracks] = useState<Track[]>([])
  const [reviewOpen, setReviewOpen] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const me = useAuthStore((s) => s.user)
  const canReplace = (m: DuplicateMatch) => me?.id === m.track.uploadedBy.id || me?.role === "admin"
  const replaceable = (task: UploadTask) => task.duplicate?.matches.filter(canReplace) ?? []

  const patchTask = (key: string, patch: Partial<UploadTask>) => {
    setTasks((prev) => prev.map((t) => (t.key === key ? { ...t, ...patch } : t)))
  }

  const handleFiles = async (fileList: FileList | null) => {
    if (!fileList || fileList.length === 0) return
    const files = Array.from(fileList).filter(isAudioFile)
    const rejected = Array.from(fileList).length - files.length
    if (rejected > 0) {
      toast.error(`${rejected} file${rejected === 1 ? "" : "s"} skipped — not an audio file`)
    }
    if (files.length === 0) return
    if (busy) {
      toast.error("An upload is already in progress")
      return
    }

    const newTasks: UploadTask[] = files.map((f) => ({
      key: `${f.name}-${f.size}-${f.lastModified}-${Math.random().toString(36).slice(2, 7)}`,
      name: f.name,
      size: f.size,
      progress: 0,
      status: "measuring",
    }))
    setTasks(newTasks)
    setBusy(true)

    const uploaded: Track[] = []
    for (let i = 0; i < files.length; i++) {
      const file = files[i]
      const task = newTasks[i]
      try {
        patchTask(task.key, { status: "measuring" })
        const duration = await measureDuration(file)
        patchTask(task.key, { status: "uploading", progress: 0 })
        const track = await uploadTrack(file, duration, (pct) =>
          patchTask(task.key, { progress: pct })
        )
        patchTask(task.key, { status: "done", progress: 100 })
        uploaded.push(track)
      } catch (e) {
        if (e instanceof DuplicateError) {
          patchTask(task.key, { status: "duplicate", progress: 100, duplicate: e.response })
        } else {
          patchTask(task.key, { status: "error", message: (e as Error).message })
        }
      }
    }

    setBusy(false)
    if (uploaded.length > 0) {
      toast.success(
        `Uploaded ${uploaded.length} track${uploaded.length === 1 ? "" : "s"} to your library`
      )
      useLibraryStore.getState().fetchAll().catch(() => {})
      setUploadedTracks(uploaded)
    }
  }

  const addAnyway = async (task: UploadTask) => {
    if (!task.duplicate) return
    patchTask(task.key, { status: "uploading", progress: 100 })
    try {
      const track = await confirmPendingUpload(task.duplicate.pendingId)
      patchTask(task.key, { status: "done", duplicate: undefined })
      setUploadedTracks((ts) => [...ts, track])
      useLibraryStore.getState().fetchAll().catch(() => {})
    } catch (e) {
      patchTask(task.key, { status: "error", message: (e as Error).message, duplicate: undefined })
    }
  }

  const replace = async (task: UploadTask, trackId: string) => {
    if (!task.duplicate) return
    patchTask(task.key, { status: "uploading", progress: 100 })
    try {
      await replaceWithPendingUpload(task.duplicate.pendingId, trackId)
      patchTask(task.key, { status: "done", message: "replaced", duplicate: undefined })
      useLibraryStore.getState().fetchAll().catch(() => {})
    } catch (e) {
      patchTask(task.key, { status: "error", message: (e as Error).message, duplicate: undefined })
    }
  }

  const skip = (task: UploadTask) => {
    if (task.duplicate) discardPendingUpload(task.duplicate.pendingId).catch(() => {})
    patchTask(task.key, { status: "skipped", duplicate: undefined })
  }

  const doneCount = tasks.filter((t) => t.status === "done").length
  const errorCount = tasks.filter((t) => t.status === "error").length
  const duplicateCount = tasks.filter((t) => t.status === "duplicate").length
  const singleReplace = tasks.filter((t) => t.status === "duplicate" && replaceable(t).length === 1)

  return (
    <>
    <Dialog
      open={uploadOpen}
      onOpenChange={(open) => {
        setUploadOpen(open)
        if (open) {
          setTasks([])
          setUploadedTracks([])
        }
      }}
    >
      <DialogContent className="max-w-md gap-0 border-zinc-800 bg-zinc-900 p-0">
        <DialogHeader className="border-b border-zinc-800/80 px-5 py-4">
          <DialogTitle className="flex items-center gap-2 text-base">
            <CloudUpload className="size-5 text-emerald-400" aria-hidden="true" />
            Upload music
          </DialogTitle>
          <DialogDescription className="text-xs">
            MP3, WAV, FLAC, OGG, M4A
          </DialogDescription>
        </DialogHeader>

        <div className="min-w-0 px-5 py-4">
          <div
            role="button"
            tabIndex={0}
            aria-label="Drop audio files here or click to browse"
            onClick={() => inputRef.current?.click()}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault()
                inputRef.current?.click()
              }
            }}
            onDragOver={(e) => {
              e.preventDefault()
              e.stopPropagation()
              setDragActive(true)
            }}
            onDragEnter={(e) => {
              e.preventDefault()
              e.stopPropagation()
              setDragActive(true)
            }}
            onDragLeave={(e) => {
              e.preventDefault()
              e.stopPropagation()
              setDragActive(false)
            }}
            onDrop={(e) => {
              e.preventDefault()
              e.stopPropagation()
              setDragActive(false)
              handleFiles(e.dataTransfer.files)
            }}
            className={cn(
              "flex cursor-pointer flex-col items-center justify-center gap-3 rounded-xl border-2 border-dashed px-6 py-8 text-center transition-colors",
              dragActive
                ? "border-emerald-500 bg-emerald-500/10"
                : "border-zinc-700 bg-zinc-950/40 hover:border-zinc-500"
            )}
          >
            <FileAudio
              className={cn("size-10", dragActive ? "text-emerald-400" : "text-zinc-500")}
              aria-hidden="true"
            />
            <div>
              <p className="text-sm font-medium text-zinc-200">Drag &amp; drop audio files</p>
              <p className="mt-1 text-xs text-zinc-500">or</p>
            </div>
            <Button
              variant="outline"
              size="sm"
              className="border-zinc-700 bg-zinc-800/60 text-zinc-200 hover:bg-zinc-800 hover:text-emerald-300"
              onClick={(e) => {
                e.stopPropagation()
                inputRef.current?.click()
              }}
            >
              Choose files
            </Button>
            <input
              ref={inputRef}
              type="file"
              multiple
              accept="audio/*"
              className="hidden"
              onChange={(e) => {
                handleFiles(e.target.files)
                e.target.value = ""
              }}
            />
          </div>

          {tasks.length > 0 && (
            <ul className="mt-4 flex max-h-72 min-w-0 flex-col gap-2 overflow-y-auto pr-1" aria-label="Upload progress">
              {tasks.map((task) => (
                <li
                  key={task.key}
                  className="min-w-0 rounded-lg border border-zinc-800/80 bg-zinc-950/50 px-3 py-2.5"
                >
                  <div className="flex items-center gap-2">
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm text-zinc-200">{task.name}</p>
                      <p className="text-[11px] text-zinc-500">
                        {formatFileSize(task.size)}
                        {task.status === "measuring" && " · reading duration…"}
                        {task.status === "uploading" && ` · uploading… ${task.progress}%`}
                        {task.status === "done" && ` · ${task.message ?? "done"}`}
                        {task.status === "duplicate" && " · already in the library?"}
                        {task.status === "skipped" && " · skipped"}
                        {task.status === "error" && task.message ? ` · ${task.message}` : ""}
                      </p>
                    </div>
                    {task.status === "measuring" && (
                      <Loader2 className="size-4 shrink-0 animate-spin text-zinc-500" aria-hidden="true" />
                    )}
                    {task.status === "uploading" && (
                      <span className="shrink-0 text-xs tabular-nums text-zinc-400">
                        {task.progress}%
                      </span>
                    )}
                    {task.status === "done" && (
                      <CheckCircle2 className="size-4 shrink-0 text-emerald-400" aria-hidden="true" />
                    )}
                    {task.status === "error" && (
                      <XCircle className="size-4 shrink-0 text-red-400" aria-hidden="true" />
                    )}
                    {task.status === "duplicate" && (
                      <Copy className="size-4 shrink-0 text-amber-400" aria-hidden="true" />
                    )}
                    {task.status === "skipped" && (
                      <XCircle className="size-4 shrink-0 text-zinc-600" aria-hidden="true" />
                    )}
                  </div>
                  {task.status === "uploading" && (
                    <Progress value={task.progress} className="mt-2 h-1.5 bg-zinc-800" />
                  )}
                  {task.status === "duplicate" && task.duplicate && (
                    <div className="mt-2 rounded-md border border-amber-500/20 bg-amber-500/5 p-2">
                      <p className="text-[11px] text-amber-200/90">
                        Looks like {task.duplicate.matches.length === 1 ? "a track" : `${task.duplicate.matches.length} tracks`} already in the library:
                      </p>
                      <p className="mt-0.5 text-[11px] text-zinc-400">This file · {qualityLabel(task.duplicate.upload)}</p>
                      <ul className="mt-1.5 flex flex-col gap-1.5">
                        {task.duplicate.matches.map((m) => (
                          <li key={m.track.id} className="flex items-center gap-2">
                            <span className="w-8 shrink-0">
                              <CoverArt trackId={m.track.id} title={m.track.title} hasCover={m.track.hasCover} />
                            </span>
                            <span className="min-w-0 flex-1">
                              <span className="block truncate text-xs text-zinc-100">{m.track.title}</span>
                              <span className="block truncate text-[11px] text-zinc-500">
                                {m.track.artist || "Unknown artist"}
                                {m.track.album ? ` · ${m.track.album}` : ""}
                                {m.track.duration > 0 ? ` · ${formatDuration(m.track.duration)}` : ""}
                                {` · uploaded by ${m.track.uploadedBy.username || "someone"}`}
                                {` · ${m.reason}`}
                              </span>
                              <span className="block truncate text-[11px] text-zinc-400">
                                {qualityLabel(m.track)}
                                {canReplace(m) ? (
                                  <span
                                    title={`Storage now ${formatFileSize(storedBytes(m.track))}, ${formatFileSize(task.duplicate!.upload.storageBytes)} after, iOS copy included`}
                                  >
                                    {` · ${formatSizeChange(task.duplicate!.upload.storageBytes - storedBytes(m.track))} if replaced`}
                                  </span>
                                ) : null}
                              </span>
                            </span>
                            {canReplace(m) && (
                              <Button
                                size="sm"
                                variant="outline"
                                className={cn(
                                  "h-7 shrink-0 px-2.5 text-xs",
                                  betterThan(task.duplicate!.upload, m.track)
                                    ? "border-emerald-700/60 bg-emerald-600/10 text-emerald-300 hover:bg-emerald-600/20 hover:text-emerald-200"
                                    : "border-zinc-700 bg-zinc-800/60 text-zinc-200 hover:bg-zinc-800"
                                )}
                                onClick={() => replace(task, m.track.id)}
                              >
                                Replace
                              </Button>
                            )}
                          </li>
                        ))}
                      </ul>
                      <div className="mt-2 flex justify-end gap-2">
                        <Button
                          size="sm"
                          variant="ghost"
                          className="h-7 px-2 text-xs text-zinc-400 hover:text-zinc-100"
                          onClick={() => skip(task)}
                        >
                          Skip
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          className="h-7 border-zinc-700 bg-zinc-800/60 px-2.5 text-xs text-zinc-200 hover:bg-zinc-800"
                          onClick={() => addAnyway(task)}
                        >
                          Add anyway
                        </Button>
                      </div>
                    </div>
                  )}
                </li>
              ))}
            </ul>
          )}

          {tasks.length > 0 && (
            <div className="mt-3 flex items-center justify-between text-[11px] text-zinc-500">
              <span>
                {doneCount}/{tasks.length} uploaded
                {errorCount > 0 ? ` · ${errorCount} failed` : ""}
                {duplicateCount > 0 ? ` · ${duplicateCount} waiting for a decision` : ""}
              </span>
              {duplicateCount > 1 && (
                <span className="flex gap-2">
                  <button type="button" className="hover:text-zinc-200" onClick={() => tasks.filter((t) => t.status === "duplicate").forEach(skip)}>
                    Skip all
                  </button>
                  <button type="button" className="text-emerald-400 hover:text-emerald-300" onClick={() => tasks.filter((t) => t.status === "duplicate").forEach((t) => void addAnyway(t))}>
                    Add all anyway
                  </button>
                  {singleReplace.length > 0 && (
                    <button type="button" className="text-emerald-400 hover:text-emerald-300" onClick={() => singleReplace.forEach((t) => void replace(t, replaceable(t)[0].track.id))}>
                      Replace all
                    </button>
                  )}
                </span>
              )}
              {!busy && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-7 px-2 text-xs text-zinc-400 hover:text-zinc-100"
                  onClick={() => {
                    setTasks([])
                    setUploadedTracks([])
                  }}
                >
                  Clear list
                </Button>
              )}
            </div>
          )}

          {!busy && uploadedTracks.length > 0 && (
            <>
              <MetadataQueueStatus className="mt-3" />
              <Button
                size="sm"
                variant="outline"
                className="mt-3 w-full border-emerald-700/60 bg-emerald-600/10 text-xs text-emerald-300 hover:bg-emerald-600/20 hover:text-emerald-200"
                onClick={() => setReviewOpen(true)}
              >
                <Sparkles className="mr-1.5 size-3.5" aria-hidden="true" />
                Check track details ({uploadedTracks.length})
              </Button>
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>

    <MetadataReviewDialog
      tracks={uploadedTracks}
      open={reviewOpen}
      onOpenChange={setReviewOpen}
    />
    </>
  )
}
