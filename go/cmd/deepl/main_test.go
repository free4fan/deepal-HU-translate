package main

import (
	"testing"

	"deepal-hu-translate/internal/app"
)

// parseMode — новый режим locale-restore (CLI: deepl dyn|stat locale-restore).
func TestParseModeLocaleRestore(t *testing.T) {
	mode, ok := parseMode("locale-restore")
	if !ok || mode != app.ModeLocaleRestore {
		t.Errorf("parseMode(locale-restore) = %q,%v; want locale-restore mode", mode, ok)
	}
	if !isMode("locale-restore") {
		t.Error("isMode(locale-restore) = false")
	}
}
