// Backend origin helper, shared by the API client and the auth store.
//
// This module deliberately has no imports: both `lib/api.ts` and
// `stores/auth.ts` need it, and api.ts already imports the auth store, so
// putting it in either of those would create an import cycle.
//
// NEXT_PUBLIC_MUSIC_API_PORT is inlined into the browser bundle at build time,
// so it must match MUSIC_API_PORT and takes effect only after a rebuild.

export const API_PORT = process.env.NEXT_PUBLIC_MUSIC_API_PORT ?? "3001"

/** Appends the XTransformPort gateway query parameter to a relative API path. */
export function gw(path: string): string {
  return `${path}${path.includes("?") ? "&" : "?"}XTransformPort=${API_PORT}`
}
