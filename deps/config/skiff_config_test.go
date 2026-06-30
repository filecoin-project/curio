package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMaxBoomConfigFromCurio(t *testing.T) {
	curio := DefaultCurioConfig()
	curio.Subsystems.EnablePDP = true
	curio.Subsystems.DataPath = "/data"
	curio.Apis.ChainBackend = ChainBackendLantern

	sk := MaxBoomConfigFromCurio(curio)
	require.True(t, sk.Subsystems.EnablePDP)
	require.Equal(t, "/data", sk.Subsystems.DataPath)
	require.Equal(t, ChainBackendLantern, sk.Apis.ChainBackend)
}

func TestApplyMaxBoomConfigToCurio(t *testing.T) {
	curio := DefaultCurioConfig()
	curio.Subsystems.EnableSealSDR = true

	sk := DefaultMaxBoomUIConfig()
	sk.Subsystems.DataPath = "/mnt/storage"
	sk.HTTP.Enable = true

	ApplyMaxBoomConfigToCurio(curio, sk)
	require.Equal(t, "/mnt/storage", curio.Subsystems.DataPath)
	require.True(t, curio.HTTP.Enable)
	require.True(t, curio.Subsystems.EnableSealSDR, "curio-only fields should be preserved")
}
