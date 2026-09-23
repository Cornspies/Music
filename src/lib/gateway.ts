export const API_PORT = process.env.NEXT_PUBLIC_MUSIC_API_PORT ?? "3001"

export function gw(path: string): string {
  return `${path}${path.includes("?") ? "&" : "?"}XTransformPort=${API_PORT}`
}
