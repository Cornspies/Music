export const OPERATOR = (process.env.NEXT_PUBLIC_MUSIC_OPERATOR ?? "").trim()

export const CONTACT = (process.env.NEXT_PUBLIC_MUSIC_CONTACT ?? "").trim()

export const SERVER_HOST = (() => {
  try {
    return new URL((process.env.NEXT_PUBLIC_MUSIC_DEFAULT_SERVER ?? "").trim()).host
  } catch {
    return ""
  }
})()
