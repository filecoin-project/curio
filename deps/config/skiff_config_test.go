package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSkiffConfigFromCurio(t *testing.T) {
	curio := DefaultCurioConfig()
	curio.Subsystems.EnablePDP = true
	curio.Subsystems.DataPath = "/data"

	sk := SkiffConfigFromCurio(curio)
	require.True(t, sk.Subsystems.EnablePDP)
	require.Equal(t, "/data", sk.Subsystems.DataPath)
}

func TestApplySkiffConfigToCurio(t *testing.T) {
	curio := DefaultCurioConfig()
	curio.Subsystems.EnableSealSDR = true

	sk := DefaultSkiffUIConfig()
	sk.Subsystems.DataPath = "/mnt/storage"
	sk.HTTP.Enable = true

	ApplySkiffConfigToCurio(curio, sk)
	require.Equal(t, "/mnt/storage", curio.Subsystems.DataPath)
	require.True(t, curio.HTTP.Enable)
	require.True(t, curio.Subsystems.EnableSealSDR, "curio-only fields should be preserved")
}
