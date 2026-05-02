package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// APIKeyEntry represents a client API key accepted by this proxy.
// Comment is optional metadata for humans, for example where the key is used.
type APIKeyEntry struct {
	APIKey  string `yaml:"api-key" json:"api-key"`
	Comment string `yaml:"comment,omitempty" json:"comment,omitempty"`
}

// UnmarshalYAML accepts both the legacy scalar format and the structured format:
//   - "key"
//   - api-key: "key"
//     comment: "Cursor"
func (entry *APIKeyEntry) UnmarshalYAML(value *yaml.Node) error {
	if entry == nil {
		return nil
	}
	if value == nil {
		*entry = APIKeyEntry{}
		return nil
	}
	switch value.Kind {
	case yaml.ScalarNode:
		*entry = APIKeyEntry{APIKey: strings.TrimSpace(value.Value)}
		return nil
	case yaml.MappingNode:
		var decoded struct {
			APIKey  string `yaml:"api-key"`
			Key     string `yaml:"key"`
			Value   string `yaml:"value"`
			Comment string `yaml:"comment"`
		}
		if err := value.Decode(&decoded); err != nil {
			return err
		}
		apiKey := decoded.APIKey
		if apiKey == "" {
			apiKey = decoded.Key
		}
		if apiKey == "" {
			apiKey = decoded.Value
		}
		*entry = APIKeyEntry{
			APIKey:  strings.TrimSpace(apiKey),
			Comment: strings.TrimSpace(decoded.Comment),
		}
		return nil
	default:
		return fmt.Errorf("api key entry must be a string or mapping")
	}
}

// MarshalYAML preserves the compact legacy scalar format when no comment exists.
func (entry APIKeyEntry) MarshalYAML() (any, error) {
	if strings.TrimSpace(entry.Comment) == "" {
		return strings.TrimSpace(entry.APIKey), nil
	}
	type apiKeyEntry APIKeyEntry
	return apiKeyEntry{
		APIKey:  strings.TrimSpace(entry.APIKey),
		Comment: strings.TrimSpace(entry.Comment),
	}, nil
}

// UnmarshalJSON accepts either a JSON string or an object with api-key/comment.
func (entry *APIKeyEntry) UnmarshalJSON(data []byte) error {
	if entry == nil {
		return nil
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*entry = APIKeyEntry{}
		return nil
	}
	if trimmed[0] == '"' {
		var key string
		if err := json.Unmarshal(trimmed, &key); err != nil {
			return err
		}
		*entry = APIKeyEntry{APIKey: strings.TrimSpace(key)}
		return nil
	}
	var decoded struct {
		APIKey  string `json:"api-key"`
		Key     string `json:"key"`
		Value   string `json:"value"`
		Comment string `json:"comment"`
	}
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return err
	}
	apiKey := decoded.APIKey
	if apiKey == "" {
		apiKey = decoded.Key
	}
	if apiKey == "" {
		apiKey = decoded.Value
	}
	*entry = APIKeyEntry{
		APIKey:  strings.TrimSpace(apiKey),
		Comment: strings.TrimSpace(decoded.Comment),
	}
	return nil
}

// MarshalJSON preserves the compact legacy string format when no comment exists.
func (entry APIKeyEntry) MarshalJSON() ([]byte, error) {
	if strings.TrimSpace(entry.Comment) == "" {
		return json.Marshal(strings.TrimSpace(entry.APIKey))
	}
	type apiKeyEntry APIKeyEntry
	return json.Marshal(apiKeyEntry{
		APIKey:  strings.TrimSpace(entry.APIKey),
		Comment: strings.TrimSpace(entry.Comment),
	})
}

// NormalizeAPIKeyEntries trims API keys and comments and removes empty keys.
func NormalizeAPIKeyEntries(entries []APIKeyEntry) []APIKeyEntry {
	if len(entries) == 0 {
		return nil
	}
	normalized := make([]APIKeyEntry, 0, len(entries))
	for _, entry := range entries {
		apiKey := strings.TrimSpace(entry.APIKey)
		if apiKey == "" {
			continue
		}
		normalized = append(normalized, APIKeyEntry{
			APIKey:  apiKey,
			Comment: strings.TrimSpace(entry.Comment),
		})
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

// APIKeyValues extracts raw key values from structured API key entries.
func APIKeyValues(entries []APIKeyEntry) []string {
	if len(entries) == 0 {
		return nil
	}
	values := make([]string, 0, len(entries))
	for _, entry := range entries {
		if key := strings.TrimSpace(entry.APIKey); key != "" {
			values = append(values, key)
		}
	}
	if len(values) == 0 {
		return nil
	}
	return values
}
