// What changed in each version of the desktop app, shown once after an update.
// A few points a listener would notice, newest version first. A version without
// an entry shows nothing.
export const WHATS_NEW: Record<string, string[]> = {
  "0.5.0": [
    "Scroll over the volume slider to change the volume",
    "Long queues open at once, and adding or editing a song no longer reloads the library",
    "Signing out removes this account's downloads from the computer",
    "On Linux the app has its icon in the taskbar and the title bar",
  ],
}
