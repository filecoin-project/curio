//go:build !linux

package supraffi

// TreeRFile builds tree-r from a last-layer file (optionally with a staged data file).
// Used for snap updates, does not require NVMe devices.
// This is a stub implementation for non-Linux platforms.
func TreeRFile(lastLayerFilename, dataFilename, outputDir string, sectorSize uint64) int {
	panic("TreeRFile: supraseal is only available on Linux")
}
