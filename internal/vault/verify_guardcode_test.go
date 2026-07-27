package vault

import (
	"context"
	"errors"
	"testing"
	"time"

	"steamswitch/internal/vault/steamauth"
	"steamswitch/internal/vault/totp"
)

// guardCodeForLogin must answer only an emailed Guard code by prompting — never a device
// confirmation (approve-on-phone) or a device code, which a typed email code cannot satisfy.

func TestGuardCodeForLogin_DeviceConfirmationDoesNotPrompt(t *testing.T) {
	t.Cleanup(func() { SetGuardCodeNeededHook(nil) })
	called := false
	SetGuardCodeNeededHook(func(string, string, string) { called = true })

	e := Entry{SteamID64: "76561190000000010", Email: EmailBinding{Source: EmailSourceNone}}
	sess := &steamauth.Session{Guard: steamauth.GuardDeviceConfirmation, GuardMessage: "phone"}

	code, err := guardCodeForLogin(context.Background(), e, sess, time.Now())
	if err == nil {
		t.Fatal("a device-confirmation challenge has no code to type; it must fail closed")
	}
	if code != "" || called {
		t.Fatalf("no manual prompt should fire for device confirmation; code=%q called=%v", code, called)
	}
}

func TestGuardCodeForLogin_EmailManualPrompts(t *testing.T) {
	t.Cleanup(func() { SetGuardCodeNeededHook(nil) })
	SetGuardCodeNeededHook(func(id, requestID, _ string) {
		go func() { _ = SubmitManualGuardCode(id, requestID, "ZZ9Y9") }()
	})

	for _, src := range []string{EmailSourceManual, EmailSourceNone} {
		e := Entry{SteamID64: "76561190000000011", Email: EmailBinding{Source: src}}
		sess := &steamauth.Session{Guard: steamauth.GuardEmailCode, GuardMessage: "e***@x"}
		code, err := guardCodeForLogin(context.Background(), e, sess, time.Now())
		if err != nil || code != "ZZ9Y9" {
			t.Fatalf("email guard, source %q: code=%q err=%v; want the typed code", src, code, err)
		}
	}
}

func TestGuardCodeForLogin_DeviceCodeWithoutSeedFailsClosed(t *testing.T) {
	t.Cleanup(func() { SetGuardCodeNeededHook(nil) })
	called := false
	SetGuardCodeNeededHook(func(string, string, string) { called = true })

	e := Entry{SteamID64: "76561190000000012", Email: EmailBinding{Source: EmailSourceManual}}
	sess := &steamauth.Session{Guard: steamauth.GuardDeviceCode}

	if _, err := guardCodeForLogin(context.Background(), e, sess, time.Now()); !errors.Is(err, totp.ErrEmptySecret) {
		t.Fatalf("device code with no stored seed should fail with ErrEmptySecret, got %v", err)
	}
	if called {
		t.Fatal("a device-code challenge must not fall through to an email prompt")
	}
}

func TestGuardCodeForLogin_SharedSecretTakesTOTPNotManual(t *testing.T) {
	t.Cleanup(func() { SetGuardCodeNeededHook(nil) })
	called := false
	SetGuardCodeNeededHook(func(string, string, string) { called = true })

	// A stored seed must win even on an email challenge with a manual source — the TOTP path is
	// taken (and here errors on the deliberately-invalid seed), never the manual prompt.
	e := Entry{SteamID64: "76561190000000013", SharedSecret: "not-valid-base32", Email: EmailBinding{Source: EmailSourceManual}}
	sess := &steamauth.Session{Guard: steamauth.GuardEmailCode}

	if _, err := guardCodeForLogin(context.Background(), e, sess, time.Now()); err == nil {
		t.Fatal("an unreadable stored seed should error on the TOTP path")
	}
	if called {
		t.Fatal("a stored shared secret must take the TOTP path, not prompt for a manual code")
	}
}
