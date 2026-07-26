# Contributing

Thank you for your interest in contributing to SteamSwitch! Bug reports, fixes and
feature suggestions are welcome via
[GitHub issues](https://github.com/faeton/steamswitch/issues) and pull requests.

Before contributing, please read the [LICENSE](LICENSE) (GPL-3.0) and
[NOTICE.md](NOTICE.md) — this project is a derivative work of
[TcNo Account Switcher](https://github.com/TCNOco/TcNo-Acc-Switcher) by TroubleChute,
and upstream attribution must be preserved.

## Creating issues

- Avoid creating an issue if a similar one already exists; look through open and
  closed issues first.
- Keep each issue focused on one specific problem.
- Provide a descriptive title and, for bugs, steps to reproduce plus your Windows
  and Steam versions.

## How to contribute code

1. Fork the repository and create a branch from `master`.
2. Make your changes. Keep the scope of each pull request focused.
3. Run the checks below and make sure you're not regressing the documented baseline.
4. Open a pull request describing what changed and why.

## Building and testing

See `FORK.md` for the full development notes. In short:

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@latest   # if not installed
wails3 task common:generate:bindings   # required before frontend check/test/build
cd frontend && pnpm install && pnpm run check && pnpm run test
go test ./...
python3 tools/validate_i18n.py
```

Notes:

- `frontend/bindings/` is generated and gitignored; frontend tests fail to resolve
  imports until bindings have been generated at least once.
- The Go suite only fully passes on Windows. On macOS/Linux a fixed set of tests fail
  on Windows path/registry assumptions — compare against the baseline in `FORK.md`
  rather than expecting green.

## Translations

Only edit `frontend/src/Resources/en-US.json`; other locale files are managed
separately. Validate with `python3 tools/validate_i18n.py`.
