/**
 * Copies text to the clipboard.
 *
 * navigator.clipboard needs a secure context, which a self-hosted instance
 * reached over plain http on a LAN won't have, so there's a fallback for
 * that case. Returns false when even that failed and the caller should show
 * the text instead.
 */
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
    legacyCopy(text)
    return true
  } catch {
    try {
      legacyCopy(text)
      return true
    } catch {
      return false
    }
  }
}

function legacyCopy(text: string): void {
  const field = document.createElement("textarea")
  field.value = text
  field.setAttribute("readonly", "")
  field.style.position = "fixed"
  field.style.opacity = "0"
  document.body.appendChild(field)
  field.select()
  try {
    document.execCommand("copy")
  } finally {
    document.body.removeChild(field)
  }
}
