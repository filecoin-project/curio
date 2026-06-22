//go:build !maxboom

package deps

func useEmbeddedLanternBackend(_ string) bool {
	return false
}

func embeddedLanternAPIInfo() (string, bool) {
	return "", false
}
