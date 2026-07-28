import { describe, expect, it } from "vitest";
import { detectHostOS } from "./hostOS";

describe("detectHostOS", () => {
  it("detects Windows from a WebView2 user agent", () => {
    expect(
      detectHostOS(
        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120 Safari/537.36 Edg/120",
      ),
    ).toBe("windows");
  });

  it("detects macOS from a WKWebView user agent", () => {
    expect(
      detectHostOS(
        "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15",
      ),
    ).toBe("macos");
  });

  it("falls back to Linux for anything else", () => {
    expect(detectHostOS("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")).toBe("linux");
    expect(detectHostOS("")).toBe("linux");
  });

  /*
    Windows is checked first on purpose: some Windows user agents carry a "Mac OS X"
    compatibility token, and matching that would put traffic lights on a Windows window.
  */
  it("prefers Windows when a UA mentions both", () => {
    expect(detectHostOS("Mozilla/5.0 (Windows NT 10.0) like Mac OS X")).toBe("windows");
  });
});
