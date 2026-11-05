package tray

import "testing"

func TestIsCurrentSteamArg(t *testing.T) {
	const id = "76561198000000001"
	cases := []struct {
		arg, current string
		want         bool
	}{
		{"+s:" + id, id, true},
		{" +s:" + id + " ", " " + id, true},
		{"+s:76561198000000002", id, false},
		{"+s:" + id, "", false},
		{"", "", false},
	}
	for _, c := range cases {
		if got := isCurrentSteamArg(c.arg, c.current); got != c.want {
			t.Errorf("arg %q current %q: got %v", c.arg, c.current, got)
		}
	}
}
