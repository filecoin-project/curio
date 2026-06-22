//go:build !maxboom

package config

func uiInlineSchemaDocMap() map[string]string {
	return map[string]string{
		"Ingest": "CurioIngestConfig",
	}
}
