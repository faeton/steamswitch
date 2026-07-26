package main

import (
	"steamswitch/internal/basic"

	_ "embed"
)

//go:embed GameStats.json
var embeddedGameStatsJSON []byte

func init() {
	basic.SetEmbeddedGameStatsJSON(embeddedGameStatsJSON)
}
