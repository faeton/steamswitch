/**
 * Which OS's window chrome to draw (REDESIGN_BRIEF.md; the design doc opens with "Window
 * chrome per OS").
 *
 * The window is frameless on every platform, so the app draws its own caption controls. That
 * is fine on Windows, where the design's ─ ▢ ✕ cluster on the right *is* the native idiom —
 * and wrong everywhere else. A macOS user given Windows caption buttons on the right, with no
 * traffic lights on the left, reads the app as a bad port before they have used it once.
 *
 * Detected from the user agent rather than asked of the backend: this decides layout during
 * the first paint, and a round-trip would mean the chrome visibly re-arranging itself.
 */
export type HostOS = "windows" | "macos" | "linux";

export function detectHostOS(ua = typeof navigator === "undefined" ? "" : navigator.userAgent): HostOS {
  // Order matters. "Mac OS X" appears in the UA of iPadOS and of some Windows browsers'
  // compatibility tokens, but Windows is checked first, and this only ever runs inside our
  // own WebView2/WKWebView.
  if (/windows|win32|win64/i.test(ua)) return "windows";
  if (/macintosh|mac os x/i.test(ua)) return "macos";
  return "linux";
}

export const hostOS: HostOS = detectHostOS();
