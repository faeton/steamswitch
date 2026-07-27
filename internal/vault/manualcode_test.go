package vault

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestManualCode_RoundTrip(t *testing.T) {
	t.Cleanup(func() { SetGuardCodeNeededHook(nil) })

	var gotID, gotHint string
	SetGuardCodeNeededHook(func(steamID64, requestID, hint string) {
		gotID, gotHint = steamID64, hint
		// Answer asynchronously, the way the UI would.
		go func() { _ = SubmitManualGuardCode(steamID64, requestID, "  ABCDE  ") }()
	})

	code, err := requestManualCode(context.Background(), "76561190000000001", "e***@mail.test")
	if err != nil {
		t.Fatalf("requestManualCode: %v", err)
	}
	if code != "ABCDE" {
		t.Fatalf("code = %q; want trimmed ABCDE", code)
	}
	if gotID != "76561190000000001" || gotHint != "e***@mail.test" {
		t.Fatalf("hook got id=%q hint=%q", gotID, gotHint)
	}
}

func TestManualCode_ContextTimeout(t *testing.T) {
	t.Cleanup(func() { SetGuardCodeNeededHook(nil) })
	SetGuardCodeNeededHook(func(string, string, string) {}) // prompt shown, never answered

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := requestManualCode(ctx, "76561190000000002", ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline exceeded, got %v", err)
	}
}

func TestManualCode_NoHookWaitsOutContext(t *testing.T) {
	t.Cleanup(func() { SetGuardCodeNeededHook(nil) })
	SetGuardCodeNeededHook(nil) // no UI attached

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := requestManualCode(ctx, "76561190000000003", ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("no hook should wait out ctx, got %v", err)
	}
}

func TestManualCode_SubmitWithNothingWaiting(t *testing.T) {
	if err := SubmitManualGuardCode("76561190000000009", "1", "ZZZZZ"); !errors.Is(err, ErrNoManualRequest) {
		t.Fatalf("want ErrNoManualRequest, got %v", err)
	}
}

func TestManualCode_StaleSecondSubmitIgnored(t *testing.T) {
	t.Cleanup(func() { SetGuardCodeNeededHook(nil) })
	SetGuardCodeNeededHook(func(steamID64, requestID, hint string) {
		_ = SubmitManualGuardCode(steamID64, requestID, "FIRST")
		// A second submit for the same request must not block and must be reported as no-request.
		if err := SubmitManualGuardCode(steamID64, requestID, "SECOND"); !errors.Is(err, ErrNoManualRequest) {
			t.Errorf("second submit: want ErrNoManualRequest, got %v", err)
		}
	})
	code, err := requestManualCode(context.Background(), "76561190000000004", "")
	if err != nil || code != "FIRST" {
		t.Fatalf("code=%q err=%v; want FIRST", code, err)
	}
}

func TestManualCode_StalePromptWrongRequestID(t *testing.T) {
	t.Cleanup(func() { SetGuardCodeNeededHook(nil) })
	// A submit quoting a stale request id (a prompt left open by an earlier request) must not be
	// delivered to the current waiter, even for the same account.
	SetGuardCodeNeededHook(func(steamID64, requestID, hint string) {
		if err := SubmitManualGuardCode(steamID64, "stale-id-0", "OLDXX"); !errors.Is(err, ErrNoManualRequest) {
			t.Errorf("stale request id: want ErrNoManualRequest, got %v", err)
		}
		_ = SubmitManualGuardCode(steamID64, requestID, "FRESH")
	})
	code, err := requestManualCode(context.Background(), "76561190000000005", "")
	if err != nil || code != "FRESH" {
		t.Fatalf("code=%q err=%v; want FRESH (stale id must be rejected)", code, err)
	}
}
