"use client"

import type { View } from "@/stores/ui"

export interface Route {
  view: View
  arg: string | null
}

function encodeAlbumArg(arg: string | null): string[] {
  if (!arg) return []
  try {
    const { name, artist } = JSON.parse(arg) as { name: string; artist: string }
    return [encodeURIComponent(artist ?? ""), encodeURIComponent(name ?? "")]
  } catch {
    return []
  }
}

export function routeToPath(view: View, arg: string | null): string {
  switch (view) {
    case "home":
      return "/"
    case "search":
      return "/search"
    case "library":
      return "/library"
    case "favorites":
      return "/favorites"
    case "playlists":
      return "/playlists"
    case "playlist":
      return arg ? `/playlists/${encodeURIComponent(arg)}` : "/playlists"
    case "artist":
      return arg ? `/artists/${encodeURIComponent(arg)}` : "/library"
    case "album": {
      const segments = encodeAlbumArg(arg)
      return segments.length === 2 ? `/albums/${segments[0]}/${segments[1]}` : "/library"
    }
    case "offline":
      return "/offline"
    case "history":
      return "/history"
    case "discover":
      return "/discover"
    case "duplicates":
      return "/duplicates"
    case "filetags":
      return "/file-tags"
    case "share":
      return arg ? `/s/${encodeURIComponent(arg)}` : "/"
    case "listen":
      return arg ? `/listen/${encodeURIComponent(arg)}` : "/"
    case "admin":
      return "/admin"
    default:
      return "/"
  }
}

export function pathToRoute(pathname: string): Route {
  const parts = pathname.split("/").filter(Boolean).map((p) => safeDecode(p))

  if (parts.length === 0) return { view: "home", arg: null }

  switch (parts[0]) {
    case "search":
      return { view: "search", arg: null }
    case "library":
      return { view: "library", arg: null }
    case "favorites":
      return { view: "favorites", arg: null }
    case "playlists":
      return parts[1] ? { view: "playlist", arg: parts[1] } : { view: "playlists", arg: null }
    case "artists":
      return parts[1] ? { view: "artist", arg: parts[1] } : { view: "library", arg: null }
    case "albums":
      if (parts[1] && parts[2]) {
        return { view: "album", arg: JSON.stringify({ name: parts[2], artist: parts[1] }) }
      }
      return { view: "library", arg: null }
    case "offline":
      return { view: "offline", arg: null }
    case "history":
      return { view: "history", arg: null }
    case "discover":
      return { view: "discover", arg: null }
    case "duplicates":
      return { view: "duplicates", arg: null }
    case "file-tags":
      return { view: "filetags", arg: null }
    case "s":
      return parts[1] ? { view: "share", arg: parts[1] } : { view: "home", arg: null }
    case "listen":
      return parts[1] ? { view: "listen", arg: parts[1] } : { view: "home", arg: null }
    case "admin":
      return { view: "admin", arg: null }
    case "login":
    case "register":
      return { view: "home", arg: null }
    default:
      return { view: "home", arg: null }
  }
}

function safeDecode(segment: string): string {
  try {
    return decodeURIComponent(segment)
  } catch {
    return segment
  }
}
