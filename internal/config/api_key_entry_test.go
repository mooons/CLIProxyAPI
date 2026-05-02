package config

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAPIKeyEntryYAMLAcceptsLegacyAndCommentedForms(t *testing.T) {
	var cfg struct {
		APIKeys []APIKeyEntry `yaml:"api-keys"`
	}
	data := []byte(`
api-keys:
  - legacy-key
  - api-key: structured-key
    comment: Cursor
`)
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal yaml: %v", err)
	}
	if len(cfg.APIKeys) != 2 {
		t.Fatalf("len(APIKeys) = %d, want 2", len(cfg.APIKeys))
	}
	if cfg.APIKeys[0].APIKey != "legacy-key" || cfg.APIKeys[0].Comment != "" {
		t.Fatalf("legacy entry = %#v", cfg.APIKeys[0])
	}
	if cfg.APIKeys[1].APIKey != "structured-key" || cfg.APIKeys[1].Comment != "Cursor" {
		t.Fatalf("structured entry = %#v", cfg.APIKeys[1])
	}
}

func TestAPIKeyEntryJSONAcceptsLegacyAndCommentedForms(t *testing.T) {
	var entries []APIKeyEntry
	data := []byte(`["legacy-key",{"api-key":"structured-key","comment":"CI"}]`)
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].APIKey != "legacy-key" || entries[0].Comment != "" {
		t.Fatalf("legacy entry = %#v", entries[0])
	}
	if entries[1].APIKey != "structured-key" || entries[1].Comment != "CI" {
		t.Fatalf("structured entry = %#v", entries[1])
	}
}
