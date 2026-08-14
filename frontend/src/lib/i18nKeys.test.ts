import { describe, expect, it } from "vitest";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

/**
 * Every i18n key the code names must exist in en-US.json.
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
 * The first version of this test checked literal `$t("Key")` calls in `frontend/src` only, and
 * was described as making the next prune safe. It did not: three ways of naming a key were
 * invisible to it, and the third was already broken in the tree when it was written --
 * `Toast_Kit_UnsupportedEntry`, `Toast_Kit_BadPath` and `Toast_Steam_InvalidId` were being
 * rendered as raw keys at the user. All three gaps are covered here now:
 *
 *   1. keys passed as ordinary arguments (`reportFailure("Toast_...")`), resolved by a `t()`
 *      call somewhere else entirely;
 *   2. keys built from a template literal -- `` $t(`Roster_Field_${f}`) `` -- where the static
 *      prefix is checked for at least one match, since the suffix is only known at runtime;
 *   3. keys that originate in Go (`errors.New("Toast_Kit_BadPath")`, tray strings), which live
 *      outside `frontend/src` entirely.
 *
 * Both 1 and 3 rely on `keyPrefixes()`: a token only counts as a key if its first underscore
 * segment is one en-US.json actually uses. That keeps ordinary identifiers out while staying
 * self-maintaining -- a new namespace in en-US.json is picked up with no change here.
 */

const SRC = fileURLToPath(new URL("..", import.meta.url));
const RESOURCES = join(SRC, "Resources");
const REPO = fileURLToPath(new URL("../../..", import.meta.url));
const GO_ROOT = join(REPO, "internal");

const source = JSON.parse(readFileSync(join(RESOURCES, "en-US.json"), "utf8")) as Record<
  string,
  string
>;

/** `$t("Key")` / `t("Key")`, single or double quoted. */
const CALL_RE = /\$?\bt\(\s*(["'])([A-Za-z][A-Za-z0-9_]*)\1/g;
/** `labelKey: "Key"` and friends -- keys held in data and passed to t() indirectly. */
const PROP_RE =
  /\b(?:label|title|desc|action|failed|fallback|verdict|health)Key\b\s*[:=]\s*(["'])([A-Za-z][A-Za-z0-9_]*)\1/g;
/** Any quoted token at all; filtered down to plausible keys by `looksLikeKey`. */
const LOOSE_RE = /(["'`])([A-Z][A-Za-z0-9]*(?:_[A-Za-z0-9]+)+)\1/g;
/** `` $t(`Prefix_${x}`) `` -- the static head of a key completed at runtime. */
const TEMPLATE_RE = /\$?\bt\(\s*`([A-Za-z][A-Za-z0-9_]*_)\$\{/g;

/** The first underscore segment of every key en-US.json defines, e.g. `Toast`, `Vault`. */
function keyPrefixes(): Set<string> {
  const out = new Set<string>();
  for (const key of Object.keys(source)) {
    const head = key.split("_")[0];
    if (head) out.add(head);
  }
  return out;
}

const PREFIXES = keyPrefixes();

/**
 * `Steam_` is not usable as a loose-scan namespace: the settings file uses the same shape for
 * its own field names (`json:"Steam_RememberPassword"`, `settings.Steam_WebApiKey`), and those
 * are data, not messages. Only some of them carry a Go struct tag, so there is no structural
 * way to tell the two apart -- the namespace itself is the ambiguity.
 *
 * Nothing is lost that matters: `Steam_*` *messages* are still checked wherever they are named
 * in a `t()` call or a `...Key` property, which in this tree is everywhere they are used.
 */
const LOOSE_EXCLUDED_HEADS = new Set(["Steam"]);

function looksLikeKey(token: string): boolean {
  if (!token.includes("_")) return false;
  const head = token.split("_")[0];
  return PREFIXES.has(head) && !LOOSE_EXCLUDED_HEADS.has(head);
}

function walk(dir: string, keep: (name: string) => boolean): string[] {
  const out: string[] = [];
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) {
      if (path !== RESOURCES) out.push(...walk(path, keep));
    } else if (keep(name)) {
      out.push(path);
    }
  }
  return out;
}

const frontendFiles = (): string[] =>
  walk(SRC, (n) => (n.endsWith(".svelte") || n.endsWith(".ts")) && !n.endsWith(".test.ts"));
const goFiles = (): string[] =>
  walk(GO_ROOT, (n) => n.endsWith(".go") && !n.endsWith("_test.go"));

type Refs = Map<string, string[]>;

function add(found: Refs, key: string, file: string): void {
  const rel = file.slice(REPO.length);
  const where = found.get(key) ?? [];
  if (!where.includes(rel)) where.push(rel);
  found.set(key, where);
}

function scan(found: Refs, file: string, res: RegExp[], filter?: (k: string) => boolean): void {
  const text = readFileSync(file, "utf8");
  for (const re of res) {
    re.lastIndex = 0;
    let m: RegExpExecArray | null;
    while ((m = re.exec(text)) !== null) {
      const key = m[2];
      if (filter && !filter(key)) continue;
      add(found, key, file);
    }
  }
}

/** Literal keys, from both languages. */
function referencedKeys(): Refs {
  const found: Refs = new Map();
  for (const file of frontendFiles()) {
    scan(found, file, [CALL_RE, PROP_RE]);
    // Keys handed around as plain values before something else calls t() on them.
    scan(found, file, [LOOSE_RE], looksLikeKey);
  }
  for (const file of goFiles()) {
    scan(found, file, [LOOSE_RE], looksLikeKey);
  }
  return found;
}

/** Static heads of runtime-built keys, e.g. `Roster_Field_`. */
function templatePrefixes(): Refs {
  const found: Refs = new Map();
  for (const file of frontendFiles()) {
    const text = readFileSync(file, "utf8");
    TEMPLATE_RE.lastIndex = 0;
    let m: RegExpExecArray | null;
    while ((m = TEMPLATE_RE.exec(text)) !== null) {
      add(found, m[1], file);
    }
  }
  return found;
}

describe("i18n keys", () => {
  const referenced = referencedKeys();

  it("finds keys to check at all", () => {
    // Guards the regexes themselves: a scan that silently matches nothing would make the
    // assertions below pass forever while checking nothing.
    expect(referenced.size).toBeGreaterThan(200);
  });

  it("scans Go as well as the frontend", () => {
    // Pins the widening: the three keys that prompted it were Go-only, and a refactor that
    // dropped the Go pass would restore exactly the hole they fell through.
    const fromGo = [...referenced.entries()].filter(([, files]) =>
      files.some((f) => f.endsWith(".go")),
    );
    expect(fromGo.length).toBeGreaterThan(10);
  });

  it("resolves every literal key against en-US.json", () => {
    const missing = [...referenced.entries()]
      .filter(([key]) => !(key in source))
      .map(([key, files]) => `${key} (${files.join(", ")})`);
    expect(missing).toEqual([]);
  });

  it("leaves every runtime-built key family with something to resolve to", () => {
    // A prune cannot see `` $t(`Dota_Part_${p}`) ``, so the family is what gets checked: if
    // every `Dota_Part_*` key were deleted, the panel would render raw keys for every row.
    const keys = Object.keys(source);
    const orphaned = [...templatePrefixes().entries()]
      .filter(([prefix]) => !keys.some((k) => k.startsWith(prefix)))
      .map(([prefix, files]) => `${prefix}* (${files.join(", ")})`);
    expect(orphaned).toEqual([]);
  });
});
