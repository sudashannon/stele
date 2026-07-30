export async function copyText(text: string): Promise<void> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return
    }
  } catch {
    // In insecure contexts the Clipboard API may exist but reject. Fall back
    // to the legacy copy command, which still works on LAN-hosted HTTP pages.
  }

  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.readOnly = true
  textarea.tabIndex = -1
  textarea.setAttribute('aria-hidden', 'true')
  textarea.className = 'fixed left-0 top-0 opacity-0'
  document.body.appendChild(textarea)

  let copied = false
  try {
    textarea.select()
    copied = document.execCommand?.('copy') ?? false
  } catch {
    copied = false
  } finally {
    textarea.remove()
  }

  if (!copied) throw new Error('无法复制到剪贴板，请手动复制')
}
