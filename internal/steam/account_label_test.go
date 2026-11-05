package steam

import (
	"strings"
	"testing"
)

func TestLabelOr(t *testing.T) {
	labels := map[string]string{"1": " Smurf 2 ", "2": "  "}
	for id, want := range map[string]string{"1": "Smurf 2", "2": "Steam name", "3": "Steam name"} {
		if got := labelOr(labels, id, "Steam name"); got != want {
			t.Errorf("%s: got %q, want %q", id, got, want)
		}
	}
	if got := labelOr(nil, "1", "Steam name"); got != "Steam name" {
		t.Errorf("nil labels: got %q", got)
	}
}

func TestSteamNameBehindLabel(t *testing.T) {
	labels := map[string]string{"1": "Smurf 2"}
	if got := steamNameBehindLabel(labels, "1", "Crybaby"); got != "Crybaby" {
		t.Errorf("labelled: got %q", got)
	}
	if got := steamNameBehindLabel(labels, "2", "Akamai"); got != "" {
		t.Errorf("no label: got %q", got)
	}
}

func TestWithAccountLabel(t *testing.T) {
	before := map[string]string{"1": "Old"}
	set, err := withAccountLabel(before, "2", " New ")
	if err != nil || set["2"] != "New" || set["1"] != "Old" {
		t.Fatalf("set: %v %v", set, err)
	}
	if len(before) != 1 {
		t.Fatalf("the input was changed: %v", before)
	}
	cleared, err := withAccountLabel(set, "2", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cleared["2"]; ok {
		t.Fatalf("an empty name should clear the label: %v", cleared)
	}
	if _, err := withAccountLabel(nil, "1", strings.Repeat("é", accountLabelMaxRunes+1)); err == nil {
		t.Fatal("an overlong name should be refused")
	}
	if _, err := withAccountLabel(nil, "1", strings.Repeat("é", accountLabelMaxRunes)); err != nil {
		t.Fatalf("a name at the limit: %v", err)
	}
}
