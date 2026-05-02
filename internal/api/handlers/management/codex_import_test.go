package management

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v6/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestImportCodexAuthJSON_RawTokensShapeConvertsAndRegisters(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	authDir := t.TempDir()
	store := sdkAuth.NewFileTokenStore()
	manager := coreauth.NewManager(store, nil, nil)
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)
	h.tokenStore = store

	accessToken := testJWT(t, map[string]any{"exp": 1893456000})
	idToken := testJWT(t, map[string]any{
		"email": "user@example.com",
		"sub":   "sub-user",
		"exp":   1893457000,
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acct_user",
			"chatgpt_plan_type":  "plus",
		},
	})
	body := fmt.Sprintf(`{
		"tokens": {
			"access_token": %q,
			"refresh_token": "refresh-token",
			"id_token": %q,
			"account_id": "acct_from_tokens"
		},
		"last_refresh": "2026-05-02T12:00:00Z"
	}`, accessToken, idToken)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v0/management/codex/import-auth-json", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	h.ImportCodexAuthJSON(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	const wantName = "codex-user@example.com-plus.json"
	saved := readSavedAuthJSON(t, filepath.Join(authDir, wantName))
	if got := saved["type"]; got != "codex" {
		t.Fatalf("type = %#v, want codex", got)
	}
	if got := saved["email"]; got != "user@example.com" {
		t.Fatalf("email = %#v, want user@example.com", got)
	}
	if got := saved["access_token"]; got != accessToken {
		t.Fatalf("access_token not preserved")
	}
	if got := saved["refresh_token"]; got != "refresh-token" {
		t.Fatalf("refresh_token = %#v, want refresh-token", got)
	}
	if got := saved["id_token"]; got != idToken {
		t.Fatalf("id_token not preserved")
	}
	if got := saved["account_id"]; got != "acct_from_tokens" {
		t.Fatalf("account_id = %#v, want acct_from_tokens", got)
	}
	if got := saved["plan_type"]; got != "plus" {
		t.Fatalf("plan_type = %#v, want plus", got)
	}
	if got := saved["expired"]; got != "2030-01-01T00:00:00Z" {
		t.Fatalf("expired = %#v, want 2030-01-01T00:00:00Z", got)
	}

	auth, ok := manager.GetByID(wantName)
	if !ok || auth == nil {
		t.Fatalf("expected imported auth to be registered as %s", wantName)
	}
	if got := auth.Attributes["plan_type"]; got != "plus" {
		t.Fatalf("registered plan_type attr = %q, want plus", got)
	}
	if got, _ := auth.Metadata["access_token"].(string); got != accessToken {
		t.Fatalf("registered metadata access token not preserved")
	}
}

func TestImportCodexAuthJSON_MultipartLegacyShapeUsesOverrides(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	authDir := t.TempDir()
	store := sdkAuth.NewFileTokenStore()
	manager := coreauth.NewManager(store, nil, nil)
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)
	h.tokenStore = store

	accessToken := testJWT(t, map[string]any{"exp": 1893456000})
	idToken := testJWT(t, map[string]any{
		"email": "ignored@example.com",
		"sub":   "sub-team",
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acct_team",
		},
	})
	source := fmt.Sprintf(`{
		"openai-codex": {
			"access": %q,
			"refresh": "legacy-refresh",
			"id_token": %q,
			"accountId": "acct_team",
			"lastRefresh": "2026-05-02T13:00:00Z"
		}
	}`, accessToken, idToken)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "auth.json")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err = file.Write([]byte(source)); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	_ = writer.WriteField("email", "team@example.com")
	_ = writer.WriteField("plan_type", "Team")
	_ = writer.WriteField("prefix", "team-a")
	_ = writer.WriteField("proxy_url", "direct")
	_ = writer.WriteField("refresh_interval_seconds", "900")
	if err = writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v0/management/codex/import-auth-json", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	ctx.Request = req
	h.ImportCodexAuthJSON(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	hash := sha256.Sum256([]byte("acct_team"))
	accountHash := hex.EncodeToString(hash[:])[:8]
	wantName := fmt.Sprintf("codex-%s-team@example.com-team.json", accountHash)
	saved := readSavedAuthJSON(t, filepath.Join(authDir, wantName))
	if got := saved["email"]; got != "team@example.com" {
		t.Fatalf("email = %#v, want team@example.com", got)
	}
	if got := saved["plan_type"]; got != "Team" {
		t.Fatalf("plan_type = %#v, want Team", got)
	}
	if got := saved["prefix"]; got != "team-a" {
		t.Fatalf("prefix = %#v, want team-a", got)
	}
	if got := saved["proxy_url"]; got != "direct" {
		t.Fatalf("proxy_url = %#v, want direct", got)
	}
	if got := saved["refresh_interval_seconds"]; got != float64(900) {
		t.Fatalf("refresh_interval_seconds = %#v, want 900", got)
	}

	auth, ok := manager.GetByID(wantName)
	if !ok || auth == nil {
		t.Fatalf("expected imported auth to be registered as %s", wantName)
	}
	if auth.Prefix != "team-a" {
		t.Fatalf("registered prefix = %q, want team-a", auth.Prefix)
	}
	if auth.ProxyURL != "direct" {
		t.Fatalf("registered proxy_url = %q, want direct", auth.ProxyURL)
	}
	if got := auth.Attributes["refresh_interval_seconds"]; got != "900" {
		t.Fatalf("registered refresh_interval_seconds = %q, want 900", got)
	}
}

func TestImportCodexAuthJSON_RequiresIDToken(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	authDir := t.TempDir()
	store := sdkAuth.NewFileTokenStore()
	manager := coreauth.NewManager(store, nil, nil)
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)
	h.tokenStore = store

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v0/management/codex/import-auth-json", strings.NewReader(`{"access_token":"access"}`))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	h.ImportCodexAuthJSON(ctx)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "id_token") {
		t.Fatalf("expected id_token error, got %s", rec.Body.String())
	}
}

func testJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": "none", "typ": "JWT"}
	headerData, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal jwt header: %v", err)
	}
	payloadData, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal jwt claims: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(headerData) + "." +
		base64.RawURLEncoding.EncodeToString(payloadData) + ".signature"
}

func readSavedAuthJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved auth file %s: %v", path, err)
	}
	var saved map[string]any
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("unmarshal saved auth file: %v", err)
	}
	return saved
}
