/**
 * How the last mailbox sign-in reads on screen. Kept out of the components so the row flag and
 * the editor cannot describe the same result differently.
 */
export type EmailCheckLike = {
  checkedAt?: string;
  status?: string;
  messages?: number | null;
  provider?: string;
};

export type EmailCheckView = {
  tone: "ok" | "warn" | "fail";
  /** Short text for a row. */
  flagKey: string;
  /** The full sentence, saying what to do about it. */
  detailKey: string;
  params: Record<string, string | number>;
};

const VIEWS: Record<string, { tone: EmailCheckView["tone"]; key: string }> = {
  ok: { tone: "ok", key: "OK" },
  auth: { tone: "fail", key: "Auth" },
  "app-password": { tone: "fail", key: "AppPassword" },
  unsupported: { tone: "warn", key: "Unsupported" },
  // Usually temporary, and says nothing about whether the password is right.
  connect: { tone: "warn", key: "Connect" },
  config: { tone: "warn", key: "Config" },
};

export function emailCheckView(check: EmailCheckLike | null | undefined): EmailCheckView | null {
  const view = check?.status ? VIEWS[check.status] : undefined;
  if (!check || !view) return null;
  return {
    tone: view.tone,
    flagKey: `Vault_EmailFlag_${view.key}`,
    detailKey: `Vault_EmailCheck_${view.key}`,
    params: { count: check.messages ?? 0, provider: check.provider ?? "" },
  };
}

/** Only these sources can be signed in to. "manual" and "none" have nothing to check. */
export function canCheckEmail(source: string | null | undefined): boolean {
  return source === "imap" || source === "mailbox-api";
}
