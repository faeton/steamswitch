package main

import (
	"steamswitch/internal/platform"

	_ "embed"
)

//go:embed Platforms.json
var embeddedPlatformsJSON []byte

func init() {
	platform.SetEmbeddedPlatformsJSON(embeddedPlatformsJSON)
}
