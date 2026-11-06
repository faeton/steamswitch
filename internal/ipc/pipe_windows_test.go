//go:build windows

package ipc

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestIsClosedPipeErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"net.ErrClosed", net.ErrClosed, true},
		{"io.EOF", io.EOF, true},
		{"wrapped net.ErrClosed", &net.OpError{Op: "accept", Err: net.ErrClosed}, true},
		{"wrapped io.EOF", &net.OpError{Op: "accept", Err: io.EOF}, true},
		{"other error", errors.New("boom"), false},
		{"nil inside OpError", &net.OpError{Op: "accept"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isClosedPipeErr(tc.err); got != tc.want {
				t.Errorf("isClosedPipeErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestStopIsIdempotent(t *testing.T) {
	stop, err := StartGUIServer(func(argv []string) {})
	if err != nil {
		// Pipe might be in use by another test; skip.
		t.Skipf("StartGUIServer: %v", err)
	}
	stop()
	stop() // must not panic on double close
}

// TestForwardArgsRoundTrip is the path a second instance takes to hand its arguments to the GUI.
func TestForwardArgsRoundTrip(t *testing.T) {
	got := make(chan []string, 1)
	stop, err := StartGUIServer(func(argv []string) { got <- argv })
	if err != nil {
		t.Fatalf("StartGUIServer: %v", err)
	}
	defer stop()

	want := []string{"+s:76561198000000000"}
	if err := ForwardArgs(want); err != nil {
		t.Fatalf("ForwardArgs: %v", err)
	}
	select {
	case argv := <-got:
		if len(argv) != 1 || argv[0] != want[0] {
			t.Fatalf("handler got %v, want %v", argv, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handler was not called")
	}
}
