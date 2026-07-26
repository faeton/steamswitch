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
