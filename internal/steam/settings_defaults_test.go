package steam

import "testing"

func TestBoolWithDefault(t *testing.T) {
	// The distinction json.Unmarshal cannot make: absent means "use the default", present
	// means "the user decided". Getting it wrong either re-enables an option someone turned
	// off, or silently disables one every existing install relies on.
	cases := []struct {
		name string
		json string
		def  bool
		want bool
	}{
		{"absent key keeps a true default", `{}`, true, true},
		{"absent key keeps a false default", `{}`, false, false},
		{"explicit false overrides a true default", `{"Steam_RememberPassword":false}`, true, false},
		{"explicit true overrides a false default", `{"Steam_RememberPassword":true}`, false, true},
		{"explicit true with a true default", `{"Steam_RememberPassword":true}`, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := boolWithDefault([]byte(tc.json), "Steam_RememberPassword", tc.def); got != tc.want {
				t.Fatalf("boolWithDefault = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDefaultSettings_RemembersPassword(t *testing.T) {
	// A fresh install is someone's own machine until they say otherwise; switching should
	// land them signed in.
	if !defaultSettings().SteamRememberPassword {
		t.Fatal("SteamRememberPassword should default to true")
	}
}
