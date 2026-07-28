/**
 * Turning a health report into the one thing a tile can show.
 *
 * Kept out of the component so the "which signal wins" rule is testable without mounting
 * anything — it is the kind of logic that looks obviously right and is quietly wrong for the
 * account that has one failing signal among five passing ones.
 */
import type { HealthReport, Signal } from "../../../bindings/steamswitch/internal/vault/models";

export type Verdict = "unknown" | "ok" | "warn" | "fail";

/** Mirrors verdictRank in internal/vault/health.go. Worst wins. */
const RANK: Record<Verdict, number> = { unknown: 0, ok: 1, warn: 2, fail: 3 };

export function isVerdict(v: string): v is Verdict {
  return v === "unknown" || v === "ok" || v === "warn" || v === "fail";
}

export function worstVerdict(a: string, b: string): Verdict {
  const va: Verdict = isVerdict(a) ? a : "unknown";
  const vb: Verdict = isVerdict(b) ? b : "unknown";
  return RANK[vb] > RANK[va] ? vb : va;
}

/**
 * The tile shows a dot only for warn and fail.
 *
 * "ok" and "unknown" both render nothing, for different reasons: a green dot on every
 * healthy account is noise that trains people to ignore the colour, and a grey dot for
 * "not checked" would mark every account on a machine where the feature is unused.
 */
export function tileDot(report: HealthReport | null | undefined): "warn" | "fail" | null {
  if (!report) {
    return null;
  }
  const v = isVerdict(report.verdict) ? report.verdict : "unknown";
  return v === "warn" || v === "fail" ? v : null;
}

/** Signals worth surfacing first: blockers, then failures, then warnings. */
export function rankedSignals(report: HealthReport | null | undefined): Signal[] {
  if (!report?.signals) {
    return [];
  }
  return [...report.signals].sort((a, b) => {
    if (a.blocker !== b.blocker) {
      return a.blocker ? -1 : 1;
    }
    const ra = RANK[isVerdict(a.status) ? a.status : "unknown"];
    const rb = RANK[isVerdict(b.status) ? b.status : "unknown"];
    if (ra !== rb) {
      return rb - ra;
    }
    return a.name.localeCompare(b.name);
  });
}

/**
 * Whether a report is stale enough to be worth re-running.
 *
 * Used only to decide what to *offer*; nothing re-checks on its own from the UI.
 */
export function isStale(report: HealthReport | null | undefined, now: Date, maxAgeHours = 24): boolean {
  if (!report?.probedAt) {
    return true;
  }
  const t = Date.parse(report.probedAt);
  if (Number.isNaN(t)) {
    return true;
  }
  return now.getTime() - t > maxAgeHours * 3600 * 1000;
}

/**
 * The i18n key for a verdict's summary line.
 */
export function verdictLabelKey(verdict: string): string {
  switch (verdict) {
    case "ok":
      return "Vault_Verdict_OK";
    case "warn":
      return "Vault_Verdict_Warn";
    case "fail":
      return "Vault_Verdict_Fail";
    default:
      return "Vault_Verdict_Unknown";
  }
}

/* ------------------------------------------------------------------ health states (A9) */

/**
 * The named health states a row or tile can be in (REDESIGN_BRIEF.md A9).
 *
 * A raw verdict ("warn") is not something to show a person: it says how bad, never what is
 * wrong or what to do. These eight states are the vocabulary the brief asks for, each with a
 * plain-language label, a tone, and a next action. Anything that does not match a known
 * pattern degrades to the verdict-only state rather than inventing a diagnosis.
 */
export type HealthStateId =
  | "checking"
  | "banned"
  | "password-wrong"
  | "token-expired"
  | "guard-unreachable"
  | "limited"
  | "ok"
  | "never-checked"
  | "attention";

export type HealthTone = "ok" | "warn" | "fail" | "neutral" | "busy";

export type HealthState = {
  id: HealthStateId;
  /** i18n key for the short label shown in the badge. */
  labelKey: string;
  /** i18n key for the one-line "what to do about it", or "" when there is nothing to do. */
  actionKey: string;
  tone: HealthTone;
  /** Higher wins when several signals are unhappy at once. */
  severity: number;
};

const STATES: Record<HealthStateId, HealthState> = {
  // Banned is terminal and outranks everything: no amount of fixing credentials helps.
  banned: { id: "banned", labelKey: "Vault_State_Banned", actionKey: "", tone: "fail", severity: 100 },
  "password-wrong": {
    id: "password-wrong",
    labelKey: "Vault_State_PasswordWrong",
    actionKey: "Vault_State_PasswordWrong_Next",
    tone: "fail",
    severity: 80,
  },
  "token-expired": {
    id: "token-expired",
    labelKey: "Vault_State_TokenExpired",
    actionKey: "Vault_State_TokenExpired_Next",
    tone: "warn",
    severity: 60,
  },
  "guard-unreachable": {
    id: "guard-unreachable",
    labelKey: "Vault_State_GuardUnreachable",
    actionKey: "Vault_State_GuardUnreachable_Next",
    tone: "warn",
    severity: 50,
  },
  // Rate limiting is temporary and self-clearing, so it sits below the states a user can act on.
  limited: {
    id: "limited",
    labelKey: "Vault_State_Limited",
    actionKey: "Vault_State_Limited_Next",
    tone: "warn",
    severity: 40,
  },
  attention: {
    id: "attention",
    labelKey: "Vault_State_Attention",
    actionKey: "Vault_State_Attention_Next",
    tone: "warn",
    severity: 30,
  },
  checking: { id: "checking", labelKey: "Vault_Verdict_Checking", actionKey: "", tone: "busy", severity: 20 },
  ok: { id: "ok", labelKey: "Vault_State_OK", actionKey: "", tone: "ok", severity: 10 },
  "never-checked": {
    id: "never-checked",
    labelKey: "Vault_State_NeverChecked",
    actionKey: "Vault_State_NeverChecked_Next",
    tone: "neutral",
    severity: 0,
  },
};

/**
 * Which state a signal implies, or null when it says nothing actionable.
 *
 * Matched on the signal's `detail` key rather than its `name`, because the name only says
 * *which probe* ran ("token") while the detail says what it found ("Vault_Signal_TokenValid"
 * vs "Vault_Signal_TokenExpired") — the same probe produces both happy and unhappy details.
 */
function stateForSignal(detail: string, status: string): HealthStateId | null {
  switch (detail) {
    case "Vault_Signal_Banned":
      return "banned";
    case "Vault_Signal_PasswordWrong":
      return "password-wrong";
    case "Vault_Signal_TokenExpired":
    case "Vault_Signal_SessionRevoked":
    case "Vault_Signal_TokenUnreadable":
      return "token-expired";
    case "Vault_Signal_GuardRejected":
    case "Vault_Signal_PasswordOKGuardUnavailable":
    case "Vault_Signal_BadAuthenticatorSeed":
      return "guard-unreachable";
    case "Vault_Signal_RateLimited":
    case "Vault_Signal_LikelyLimited":
    case "Vault_Signal_AccountSuspended":
      return "limited";
    default:
      // A signal that is unhappy for a reason with no dedicated state still has to show as
      // unhappy — otherwise a genuinely broken account would read as fine.
      return status === "fail" || status === "warn" ? "attention" : null;
  }
}

/**
 * Collapse a report into the single state a row or tile shows.
 *
 * `checking` is passed in rather than read off the report because it is a UI condition (a
 * request is in flight), not something the backend records.
 */
export function healthState(
  report: HealthReport | null | undefined,
  opts: { checking?: boolean } = {},
): HealthState {
  if (opts.checking) {
    return STATES.checking;
  }
  if (!report || !report.probedAt) {
    return STATES["never-checked"];
  }

  let worst: HealthState | null = null;
  for (const signal of report.signals ?? []) {
    const id = stateForSignal(signal.detail, signal.status);
    if (!id) continue;
    const candidate = STATES[id];
    if (!worst || candidate.severity > worst.severity) {
      worst = candidate;
    }
  }
  if (worst) {
    return worst;
  }

  // Probed, nothing unhappy matched: trust the overall verdict. A report whose verdict is
  // still "unknown" has run but learned nothing, which is "never checked" from where the
  // user stands, not "healthy".
  const verdict = isVerdict(report.verdict) ? report.verdict : "unknown";
  return verdict === "ok" ? STATES.ok : verdict === "unknown" ? STATES["never-checked"] : STATES.attention;
}
