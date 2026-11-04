package logsanitize

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

const fullID = "76561198000123456"

func TestSteamIDHandler_MasksEveryPlaceAnIDCanTravel(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(NewSteamIDHandler(slog.NewTextHandler(&buf, nil))).With("preset", "id "+fullID)
	log.Info("in message "+fullID,
		"steamId", fullID,
		"url", "/img/profiles/steam/"+fullID+".jpg",
		"number", uint64(76561198000123456),
		"err", errors.New("open /x/"+fullID+"/y: denied"),
		slog.Group("g", "inner", fullID))
	out := buf.String()
	if strings.Contains(out, fullID) {
		t.Fatalf("full SteamID in log: %s", out)
	}
	if !strings.Contains(out, "…123456") {
		t.Fatalf("expected the tail to survive: %s", out)
	}
}

func TestSteamIDHandler_LeavesOtherNumbersAlone(t *testing.T) {
	for _, s := range []string{"570", "1234567890123456789", "39734274", "timestamp=1762224000"} {
		if got := MaskSteamIDs(s); got != s {
			t.Errorf("%q became %q", s, got)
		}
	}
}
