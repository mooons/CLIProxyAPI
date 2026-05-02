package management

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func newAPIKeysTestHandler(t *testing.T, entries []config.APIKeyEntry) (*Handler, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("api-keys:\n  - k1\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg := &config.Config{
		SDKConfig: config.SDKConfig{
			APIKeys: entries,
		},
	}
	return NewHandler(cfg, path, nil), path
}

func TestPatchAPIKeysUpdatesComment(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, path := newAPIKeysTestHandler(t, []config.APIKeyEntry{{APIKey: "k1"}})

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/api-keys", bytes.NewBufferString(`{"index":0,"value":{"comment":"Cursor on laptop"}}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.PatchAPIKeys(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := h.cfg.APIKeys[0].Comment; got != "Cursor on laptop" {
		t.Fatalf("comment = %q, want %q", got, "Cursor on laptop")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(raw), "comment: Cursor on laptop") {
		t.Fatalf("saved config missing comment:\n%s", string(raw))
	}
}

func TestPutAPIKeysAcceptsLegacyAndStructuredEntries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _ := newAPIKeysTestHandler(t, nil)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := `["legacy-key",{"api-key":"structured-key","comment":"CI"}]`
	c.Request = httptest.NewRequest(http.MethodPut, "/v0/management/api-keys", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.PutAPIKeys(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(h.cfg.APIKeys) != 2 {
		t.Fatalf("len(APIKeys) = %d, want 2", len(h.cfg.APIKeys))
	}
	if h.cfg.APIKeys[0].APIKey != "legacy-key" || h.cfg.APIKeys[0].Comment != "" {
		t.Fatalf("legacy entry = %#v", h.cfg.APIKeys[0])
	}
	if h.cfg.APIKeys[1].APIKey != "structured-key" || h.cfg.APIKeys[1].Comment != "CI" {
		t.Fatalf("structured entry = %#v", h.cfg.APIKeys[1])
	}
}
