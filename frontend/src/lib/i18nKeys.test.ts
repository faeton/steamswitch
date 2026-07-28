import { describe, expect, it } from "vitest";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

/**
 * Every i18n key named literally in the source must exist in en-US.json.
 *
 * The failure this prevents is silent and user-facing: `interpolate($m[key] ?? key, ...)` in
 * `stores/i18n.ts` falls back to the *key itself*, so a reference to a key that was renamed
 * or deleted renders `Vault_SignIn_Assist_Title` at the user instead of text. Nothing else
 * catches it -- svelte-check sees a string, and `tools/validate_i18n.py` only compares locale
 * files against en-US, never against the code.
 *
 * It is a real hazard rather than a theoretical one: pruning keys that grep says are unused is
 * how a fork sheds upstream leftovers, and one bad prune takes out something like `Yes`/`No`,
 * which every modal's buttons depend on.
 *
 * Only literal keys are checked. Keys built at runtime -- `` $t(`Roster_Field_${f}`) `` and the
 * handful like it -- are invisible here by construction, so they still need care by hand.
 */

const SRC = fileURLToPath(new URL("..", import.meta.url));
const RESOURCES = join(SRC, "Resources");

/** `$t("Key")` / `t("Key")`, single or double quoted. */
const CALL_RE = /\$?\bt\(\s*(["'])([A-Za-z][A-Za-z0-9_]*)\1/g;
/** `labelKey: "Key"` and friends -- keys held in data and passed to t() indirectly. */
const PROP_RE = /\b(?:label|title|desc|action|failed|fallback|verdict|health)Key\b\s*[:=]\s*(["'])([A-Za-z][A-Za-z0-9_]*)\1/g;

function sourceFiles(dir: string): string[] {
  const out: string[] = [];
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) {
      if (path !== RESOURCES) {
        out.push(...sourceFiles(path));
      }
    } else if ((name.endsWith(".svelte") || name.endsWith(".ts")) && !name.endsWith(".test.ts")) {
      out.push(path);
    }
  }
  return out;
}

function referencedKeys(): Map<string, string[]> {
  const found = new Map<string, string[]>();
  for (const file of sourceFiles(SRC)) {
    const text = readFileSync(file, "utf8");
    for (const re of [CALL_RE, PROP_RE]) {
      re.lastIndex = 0;
      let m: RegExpExecArray | null;
      while ((m = re.exec(text)) !== null) {
        const key = m[2];
        const where = found.get(key) ?? [];
        const rel = file.slice(SRC.length);
        if (!where.includes(rel)) {
          where.push(rel);
        }
        found.set(key, where);
      }
    }
  }
  return found;
}

describe("i18n keys", () => {
  const source = JSON.parse(readFileSync(join(RESOURCES, "en-US.json"), "utf8")) as Record<
    string,
    string
  >;
  const referenced = referencedKeys();

  it("finds keys to check at all", () => {
    // Guards the regexes themselves: a scan that silently matches nothing would make the
    // assertion below pass forever while checking nothing.
    expect(referenced.size).toBeGreaterThan(200);
  });

  it("resolves every literal key against en-US.json", () => {
    const missing = [...referenced.entries()]
      .filter(([key]) => !(key in source))
      .map(([key, files]) => `${key} (${files.join(", ")})`);
    expect(missing).toEqual([]);
  });
});
