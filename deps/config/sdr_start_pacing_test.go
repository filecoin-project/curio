package config

import (
	"testing"
	"time"
)

func TestSDRStartPacingDefaultsDisabled(t *testing.T) {
	cfg := DefaultCurioConfig()
	if cfg.Subsystems.SealSDRMinStartInterval != 0 || cfg.Subsystems.SealSDRStartJitter {
		t.Fatal("SDR start pacing must remain disabled by default")
	}
}

func TestSDRStartPacingLayerValues(t *testing.T) {
	for _, interval := range []string{"0s", "1m30s", "43m45s", "25m20s"} {
		cfg := DefaultCurioConfig()
		layer := "[Subsystems]\nSealSDRMinStartInterval = \"" + interval + "\"\nSealSDRStartJitter = true\n"
		if _, err := TransparentDecode(layer, cfg); err != nil {
			t.Fatal(err)
		}
		want, err := time.ParseDuration(interval)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Subsystems.SealSDRMinStartInterval != want || !cfg.Subsystems.SealSDRStartJitter {
			t.Fatalf("layer values changed: %+v", cfg.Subsystems)
		}
	}
}
