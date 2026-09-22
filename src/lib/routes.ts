"use client"

import type { View } from "@/stores/ui"

/**
 * URL <-> view mapping.
 *
 * Navigation is driven by the History API rather than Next's router: the whole
 * app is one client component holding the audio element and player state, so a
 * router push (which re-runs the route tree) risks interrupting playback. The
 * optional catch-all route at app/[[...slug]]/page.tsx makes every one of these
 * paths serve the app, so deep links and refreshes work too.
 */

export interface Route {
  view: View
  /** playlist id, artist name, or JSON album key — matches UIState.viewArg */
  arg: string | null
}

/** Album viewArg is a JSON {name, artist}; in the URL it's two readable segments. */
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
    case "albums":
      return "/albums"
    case "artists":
      return "/artists"
    case "favorites":
      return "/favorites"
    case "playlists":
      return "/playlists"
    case "playlist":
      return arg ? `/playlists/${encodeURIComponent(arg)}` : "/playlists"
    case "artist":
      return arg ? `/artists/${encodeURIComponent(arg)}` : "/artists"
    case "album": {
      const segments = encodeAlbumArg(arg)
      return segments.length === 2 ? `/albums/${segments[0]}/${segments[1]}` : "/albums"
    }
    case "offline":
      return "/offline"
    case "history":
      return "/history"
    case "discover":
      return "/discover"
    case "duplicates":
      return "/duplicates"
    case "share":
      return arg ? `/s/${encodeURIComponent(arg)}` : "/"
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
      return parts[1] ? { view: "artist", arg: parts[1] } : { view: "artists", arg: null }
    case "albums":
      if (parts[1] && parts[2]) {
        return { view: "album", arg: JSON.stringify({ name: parts[2], artist: parts[1] }) }
      }
      return { view: "albums", arg: null }
    case "offline":
      return { view: "offline", arg: null }
    case "history":
      return { view: "history", arg: null }
    case "discover":
      return { view: "discover", arg: null }
    case "duplicates":
      return { view: "duplicates", arg: null }
    case "s":
      // A share link: one track, playable without an account.
      return parts[1] ? { view: "share", arg: parts[1] } : { view: "home", arg: null }
    case "admin":
      return { view: "admin", arg: null }
    case "login":
    case "register":
      // The auth screen's own addresses. Signed in, they just mean home;
      // signed out, AuthView reads the path (and ?invite=) itself.
      return { view: "home", arg: null }
    default:
      // Unknown path — show home rather than a dead end.
      return { view: "home", arg: null }
  }
}

/** A malformed %-sequence shouldn't throw and blank the app. */
function safeDecode(segment: string): string {
  try {
    return decodeURIComponent(segment)
  } catch {
    return segment
  }
}
