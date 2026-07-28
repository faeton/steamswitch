package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"steamswitch/internal/basic"
	"steamswitch/internal/cli"
	"steamswitch/internal/i18n"
	"steamswitch/internal/platform"
	"steamswitch/internal/security"
	"steamswitch/internal/shortcuts"
	"steamswitch/internal/steam"
	"steamswitch/internal/vault"
	"steamswitch/internal/winutil"
)

type Dispatch struct {
	SteamSvc    *steam.SteamService
	BasicSvc    *basic.BasicService
	PlatformSvc *platform.PlatformService
}

type ListAccountRow struct {
	UniqueID     string `json:"uniqueId"`
	DisplayName  string `json:"displayName"`
	LastLoggedIn string `json:"lastLoggedIn,omitempty"`
}

type ListPlatformRow struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// RunSealRoster executes `--seal-roster`: read a plaintext roster and its passphrase from in,
// write the sealed bundle to out or to the path the flag named.
//
// It deliberately touches nothing else — no data dir, no vault, no app-lock. Sealing is pure
// cryptography over a stream, and requiring an unlocked vault to perform it would mean the
// automation producing the roster had to get past the app-lock first, which it has no business
// holding. The bundle is only useful to a machine that can already open the vault it goes into.
func (d *Dispatch) RunSealRoster(p cli.Parsed, in io.Reader, out io.Writer) error {
	dest := strings.TrimSpace(p.SealRosterOut)
	if dest == "" {
		return humanizeVaultError(vault.SealRosterStream(in, out))
	}

	// Buffer, then write with owner-only permissions. Handing the file handle straight to
	// SealRosterStream would create the file with the process umask — world-readable on a
	// default Unix setup — and leave a zero-byte or half-written bundle behind if sealing
	// failed, which the next import would report as a corrupt file rather than a failed seal.
	var buf bytes.Buffer
	if err := vault.SealRosterStream(in, &buf); err != nil {
		return humanizeVaultError(err)
	}
	if err := security.WriteSecretFile(dest, buf.Bytes()); err != nil {
		return err
	}
	fmt.Fprintln(out, dest)
	return nil
}

// humanizeVaultError turns an `internal/vault` sentinel into something worth printing.
//
// Those errors carry an i18n *key* as their message, because the vault's callers are toasts
// that look the key up. Printing `Toast_Roster_Unreadable` at a terminal tells the person
// piping into `--seal-roster` nothing at all, so the key is resolved here — falling back to
// itself, as i18n.T does, when the resource files are not next to the binary.
func humanizeVaultError(err error) error {
	if err == nil {
		return nil
	}
	exeDir, dirErr := platform.ResolveExeDir()
	if dirErr != nil {
		return err
	}
	if msg := i18n.T(exeDir, "en-US", err.Error(), nil); msg != "" && msg != err.Error() {
		return errors.New(msg)
	}
	return err
}

func (d *Dispatch) RunList(p cli.Parsed, idx *cli.PlatformIndex) error {
	switch p.Kind {
	case cli.KindListPlatforms:
		if idx == nil {
			return fmt.Errorf("platforms file not loaded")
		}
		rows := make([]ListPlatformRow, 0, len(idx.OrderedNames))
		for _, name := range idx.OrderedNames {
			code := cli.ShortTokenForPlatform(idx, name)
			if code == "" {
				code = "?"
			}
			rows = append(rows, ListPlatformRow{Code: code, Name: name})
		}
		if p.OutputJSON {
			b, err := json.Marshal(struct {
				Platforms []ListPlatformRow `json:"platforms"`
			}{Platforms: rows})
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			return nil
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "code:\tplatform name:\n")
		for _, row := range rows {
			fmt.Fprintf(tw, "%s\t%s\n", row.Code, row.Name)
		}
		_ = tw.Flush()
		return nil

	case cli.KindListAccounts:
		if err := security.RequireUnlocked(); err != nil {
			return err
		}
		var platNames []string
		if strings.TrimSpace(p.ListAccountsPlatform) != "" {
			platNames = []string{p.ListAccountsPlatform}
		} else {
			if idx == nil {
				return fmt.Errorf("platforms file not loaded")
			}
			platNames = append([]string(nil), idx.OrderedNames...)
		}

		if p.OutputJSON {
			if len(platNames) == 1 {
				rows, err := d.accountRowsForPlatform(platNames[0])
				if err != nil {
					return err
				}
				b, err := json.Marshal(struct {
					Platform string           `json:"platform"`
					Accounts []ListAccountRow `json:"accounts"`
				}{Platform: platNames[0], Accounts: rows})
				if err != nil {
					return err
				}
				fmt.Println(string(b))
				return nil
			}
			type platBlock struct {
				Platform string           `json:"platform"`
				Accounts []ListAccountRow `json:"accounts"`
			}
			blocks := make([]platBlock, 0, len(platNames))
			for _, pk := range platNames {
				rows, err := d.accountRowsForPlatform(pk)
				if err != nil {
					return fmt.Errorf("%s: %w", pk, err)
				}
				if len(rows) == 0 {
					continue
				}
				blocks = append(blocks, platBlock{Platform: pk, Accounts: rows})
			}
			b, err := json.Marshal(struct {
				Platforms []platBlock `json:"platforms"`
			}{Platforms: blocks})
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			return nil
		}

		for _, pk := range platNames {
			rows, err := d.accountRowsForPlatform(pk)
			if err != nil {
				return fmt.Errorf("%s: %w", pk, err)
			}
			if len(rows) == 0 {
				continue
			}
			fmt.Printf("%s:\n", pk)
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "  ID\tname\tlast login\n")
			for _, r := range rows {
				last := r.LastLoggedIn
				if last == "" {
					last = "-"
				}
				fmt.Fprintf(tw, "  %s\t%s\t%s\n", r.UniqueID, r.DisplayName, last)
			}
			_ = tw.Flush()
		}
		return nil

	default:
		return fmt.Errorf("internal: not a list command")
	}
}

func (d *Dispatch) accountRowsForPlatform(platformKey string) ([]ListAccountRow, error) {
	return d.commandAdapter(platformKey).AccountRows()
}

func (d *Dispatch) RunHeadless(p cli.Parsed) error {
	if err := security.RequireUnlocked(); err != nil {
		return err
	}
	switch p.Kind {
	case cli.KindSwapSteam:
		if err := d.commandAdapter(steam.PlatformKey).Swap(p.SteamID64, p.PersonaState, p.PassthroughLaunchArgs); err != nil {
			return err
		}
		return d.LaunchAfterSwap(p)
	case cli.KindSwapBasic:
		if err := d.commandAdapter(p.PlatformKey).Swap(p.UniqueID, -1, p.PassthroughLaunchArgs); err != nil {
			return err
		}
		return d.LaunchAfterSwap(p)
	case cli.KindLogout:
		return d.RunLogout(p)
	default:
		return nil
	}
}

func (d *Dispatch) LaunchAfterSwap(p cli.Parsed) error {
	if err := security.RequireUnlocked(); err != nil {
		return err
	}
	if strings.TrimSpace(p.RunAppID) != "" {
		url := "steam://rungameid/" + strings.TrimSpace(p.RunAppID)
		return winutil.Start("cmd.exe", []string{"/c", "start", "", url}, winutil.StartOpts{})
	}
	fn := strings.TrimSpace(p.RunShortcutFile)
	pk := strings.TrimSpace(p.PlatformKey)
	if fn != "" && pk != "" {
		return shortcuts.RunShortcut(pk, fn, false)
	}
	return nil
}

func (d *Dispatch) RunLogout(p cli.Parsed) error {
	if err := security.RequireUnlocked(); err != nil {
		return err
	}
	plat := strings.TrimSpace(p.LogoutPlatform)
	if plat == "" {
		plat = steam.PlatformKey
	}
	return d.commandAdapter(plat).Logout()
}
