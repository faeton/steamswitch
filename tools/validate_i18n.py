#!/usr/bin/env python3
"""Validate locale JSON files against frontend/src/Resources/en-US.json.

An untranslated key is not a defect here. Both consumers of these files merge the locale
over en-US per key -- `stores/i18n.ts` (`{...en, ...mod.default}`) and `internal/i18n.T` --
so a key a translator has not reached renders in English, never as a raw key. This fork has
also added far more strings than it has translations for: en-US carries ~1360 keys and the
upstream locale snapshot carries ~565, and nothing has updated the latter since the initial
import. Failing on that gap makes the check permanently red, which buries the findings that
*are* defects:

  * an extra key -- dead weight, or a rename that only landed on one side
  * a placeholder mismatch -- `{login}` dropped or misspelled renders a literal brace at the
    user, and this is the only place that catches it
  * a broken `<br>` -- markup the UI injects unescaped

Those fail the run. Missing keys are reported as coverage. `--strict` restores the old
all-or-nothing behaviour for anyone wiring this into a translation sync that should gate on
completeness.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
RESOURCES = ROOT / "frontend" / "src" / "Resources"
SOURCE_LOCALE = "en-US"
SOURCE_FILE = RESOURCES / f"{SOURCE_LOCALE}.json"
PLACEHOLDER_RE = re.compile(r"\{([A-Za-z_][A-Za-z0-9_]*)\}")
TAG_RE = re.compile(r"</?([A-Za-z][A-Za-z0-9]*)\b[^>]*>|<br\s*/?>", re.IGNORECASE)


def load_json(path: Path) -> dict[str, str]:
    with path.open(encoding="utf-8-sig") as f:
        data = json.load(f)
    if not isinstance(data, dict):
        raise ValueError(f"{path} is not a JSON object")
    return {str(k): str(v) for k, v in data.items()}


def placeholders(value: str) -> list[str]:
    return PLACEHOLDER_RE.findall(value)


def html_tags(value: str) -> list[str]:
    tags: list[str] = []
    for match in TAG_RE.finditer(value):
        raw = match.group(0).lower()
        if raw.startswith("<br"):
            tags.append("br")
        else:
            prefix = "/" if raw.startswith("</") else ""
            tags.append(prefix + match.group(1).lower())
    return tags


def validate_locale(path: Path, source: dict[str, str], strict: bool) -> tuple[list[str], list[str]]:
    """Return (errors, notes) for one locale file."""
    errors: list[str] = []
    notes: list[str] = []
    try:
        data = load_json(path)
    except Exception as exc:  # noqa: BLE001 - report all parse/load failures.
        return [f"{path.name}: cannot load JSON: {exc}"], []

    source_keys = list(source)
    missing = [key for key in source_keys if key not in data]
    extra = [key for key in data if key not in source]

    # An extra key is always a defect: en-US is the only place a string may be born, so a key
    # that exists nowhere else is either dead weight from a deleted feature or half a rename.
    if extra:
        errors.append(f"{path.name}: extra {len(extra)} key(s): {', '.join(extra[:10])}")

    if missing:
        translated = len(source_keys) - len(missing)
        line = (
            f"{path.name}: {translated}/{len(source_keys)} keys translated "
            f"({len(missing)} fall back to {SOURCE_LOCALE})"
        )
        (errors if strict else notes).append(line)

    # Key order is diff hygiene for a translation round-trip, not correctness -- and it can
    # only be judged over the keys both files actually have.
    if strict and not missing and not extra and list(data) != source_keys:
        errors.append(f"{path.name}: key order differs from {SOURCE_FILE.name}")

    for key, source_value in source.items():
        if key not in data:
            continue
        translated = data[key]
        src_placeholders = placeholders(source_value)
        dst_placeholders = placeholders(translated)
        if dst_placeholders != src_placeholders:
            errors.append(
                f"{path.name}:{key}: placeholders {dst_placeholders} != {src_placeholders}"
            )

        src_tags = html_tags(source_value)
        if src_tags:
            dst_tags = html_tags(translated)
            if dst_tags != src_tags:
                errors.append(f"{path.name}:{key}: html tags {dst_tags} != {src_tags}")

    return errors, notes


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("locales", nargs="*", help="Locale codes to validate. Defaults to every non-English JSON.")
    parser.add_argument(
        "--strict",
        action="store_true",
        help="Also fail on untranslated keys and key order. For gating a translation sync, not a build.",
    )
    args = parser.parse_args()

    source = load_json(SOURCE_FILE)
    if args.locales:
        paths = [RESOURCES / f"{locale}.json" for locale in args.locales]
    else:
        paths = sorted(path for path in RESOURCES.glob("*.json") if path.stem != SOURCE_LOCALE)

    if not paths:
        print("No locale files to validate.")
        return 0

    all_errors: list[str] = []
    all_notes: list[str] = []
    for path in paths:
        if not path.exists():
            all_errors.append(f"{path.name}: file does not exist")
            continue
        errors, notes = validate_locale(path, source, args.strict)
        all_errors.extend(errors)
        all_notes.extend(notes)

    for note in all_notes:
        print(note)

    if all_errors:
        for error in all_errors:
            print(error, file=sys.stderr)
        print(f"FAILED: {len(all_errors)} issue(s) across {len(paths)} locale file(s).", file=sys.stderr)
        return 1

    print(f"OK: {len(paths)} locale file(s) validate against {SOURCE_FILE.name}.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
