package platform

import (
	"errors"
	"reflect"
	"testing"
)

func TestChangeProtocol(t *testing.T) {
	boom := errors.New("boom")
	for _, tc := range []struct {
		name          string
		enabled       bool
		previous      bool
		regErr, unErr error
		saveErr       error
		wantCalls     []string
		wantErr       error
	}{
		{name: "enable", enabled: true, wantCalls: []string{"register", "save:true"}},
		{name: "enable refused saves nothing", enabled: true, regErr: boom, wantCalls: []string{"register"}, wantErr: boom},
		{name: "enable undone when saving fails", enabled: true, saveErr: boom,
			wantCalls: []string{"register", "save:true", "unregister"}, wantErr: boom},
		{name: "refreshing an enabled link keeps it when saving fails", enabled: true, previous: true, saveErr: boom,
			wantCalls: []string{"register", "save:true"}, wantErr: boom},
		{name: "disable", enabled: false, previous: true, wantCalls: []string{"unregister", "save:false"}},
		{name: "disable refused saves nothing", enabled: false, previous: true, unErr: boom, wantCalls: []string{"unregister"}, wantErr: boom},
		{name: "disable undone when saving fails", enabled: false, previous: true, saveErr: boom,
			wantCalls: []string{"unregister", "save:false", "register"}, wantErr: boom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			err := changeProtocol(tc.enabled, tc.previous,
				func() error { calls = append(calls, "register"); return tc.regErr },
				func() error { calls = append(calls, "unregister"); return tc.unErr },
				func(v bool) error {
					if v {
						calls = append(calls, "save:true")
					} else {
						calls = append(calls, "save:false")
					}
					return tc.saveErr
				})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Fatalf("calls = %v, want %v", calls, tc.wantCalls)
			}
		})
	}
}
