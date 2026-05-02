package management

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/auth/codex"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

type codexImportOptions struct {
	EmailOverride          string
	PlanTypeOverride       string
	AccountIDOverride      string
	ProxyURL               string
	Prefix                 string
	RefreshIntervalSeconds int
}

type codexSourceRecord struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	AccountID    string
	LastRefresh  string
}

type codexImportResult struct {
	Record          *coreauth.Auth
	Email           string
	AccountID       string
	PlanType        string
	Expired         string
	LastRefresh     string
	HasAccessToken  bool
	HasRefreshToken bool
	HasIDToken      bool
}

// ImportCodexAuthJSON converts an existing Codex CLI auth.json file into a
// CLIProxyAPI Codex auth file and persists it in the configured auth directory.
func (h *Handler) ImportCodexAuthJSON(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "config unavailable"})
		return
	}
	if h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "core auth manager unavailable"})
		return
	}
	if strings.TrimSpace(h.cfg.AuthDir) == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "auth directory not configured"})
		return
	}

	source, opts, err := h.readCodexImportRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := buildCodexImportRecord(source, opts)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()
	if c.Request != nil && c.Request.Context() != nil {
		ctx = c.Request.Context()
	}
	savedPath, err := h.saveTokenRecord(ctx, result.Record)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "save_failed", "message": err.Error()})
		return
	}

	if errRegister := h.registerSavedAuthFile(ctx, savedPath); errRegister != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "register_failed", "message": errRegister.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":            "ok",
		"auth-file":         savedPath,
		"file":              result.Record.FileName,
		"provider":          "codex",
		"email":             result.Email,
		"plan_type":         result.PlanType,
		"account_id":        result.AccountID,
		"expired":           result.Expired,
		"last_refresh":      result.LastRefresh,
		"has_access_token":  result.HasAccessToken,
		"has_refresh_token": result.HasRefreshToken,
		"has_id_token":      result.HasIDToken,
	})
}

func (h *Handler) registerSavedAuthFile(ctx context.Context, path string) error {
	if h == nil || h.authManager == nil {
		return nil
	}
	auth, err := h.buildAuthFromFileData(path, nil)
	if err != nil {
		return err
	}
	return h.upsertAuthRecord(ctx, auth)
}

func (h *Handler) readCodexImportRequest(c *gin.Context) (map[string]any, codexImportOptions, error) {
	if c == nil || c.Request == nil {
		return nil, codexImportOptions{}, fmt.Errorf("request is required")
	}

	if c.ContentType() == "multipart/form-data" {
		headers, err := h.multipartAuthFileHeaders(c)
		if err != nil {
			return nil, codexImportOptions{}, fmt.Errorf("invalid multipart form: %w", err)
		}
		if len(headers) == 0 {
			return nil, codexImportOptions{}, fmt.Errorf("file is required")
		}
		if len(headers) > 1 {
			return nil, codexImportOptions{}, fmt.Errorf("exactly one file is required")
		}
		file, err := headers[0].Open()
		if err != nil {
			return nil, codexImportOptions{}, fmt.Errorf("failed to open uploaded file: %w", err)
		}
		defer file.Close()

		data, err := io.ReadAll(file)
		if err != nil {
			return nil, codexImportOptions{}, fmt.Errorf("failed to read uploaded file: %w", err)
		}
		source, err := decodeJSONObject(data)
		if err != nil {
			return nil, codexImportOptions{}, err
		}
		return source, codexImportOptionsFromForm(c), nil
	}

	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, codexImportOptions{}, fmt.Errorf("failed to read body")
	}
	root, err := decodeJSONObject(data)
	if err != nil {
		return nil, codexImportOptions{}, err
	}

	opts := codexImportOptionsFromMap(root)
	for _, key := range []string{"auth_json", "authJson", "codex_auth_json", "codexAuthJson", "source_json", "sourceJson"} {
		if nested, ok := root[key]; ok {
			source, errSource := codexImportSourceObject(nested)
			if errSource != nil {
				return nil, codexImportOptions{}, errSource
			}
			return source, opts, nil
		}
	}
	return root, opts, nil
}

func codexImportSourceObject(raw any) (map[string]any, error) {
	switch v := raw.(type) {
	case map[string]any:
		return v, nil
	case string:
		return decodeJSONObject([]byte(v))
	default:
		return nil, fmt.Errorf("source auth json must be an object or JSON string")
	}
}

func decodeJSONObject(data []byte) (map[string]any, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, fmt.Errorf("json body is required")
	}
	var obj map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil {
		return nil, fmt.Errorf("invalid json: %w", err)
	}
	if obj == nil {
		return nil, fmt.Errorf("json object is required")
	}
	return obj, nil
}

func codexImportOptionsFromForm(c *gin.Context) codexImportOptions {
	return codexImportOptions{
		EmailOverride:          firstNonEmptyStringValue(c.PostForm("email_override"), c.PostForm("emailOverride"), c.PostForm("email")),
		PlanTypeOverride:       firstNonEmptyStringValue(c.PostForm("plan_type_override"), c.PostForm("planTypeOverride"), c.PostForm("plan_type"), c.PostForm("planType")),
		AccountIDOverride:      firstNonEmptyStringValue(c.PostForm("account_id_override"), c.PostForm("accountIdOverride"), c.PostForm("account_id"), c.PostForm("accountId")),
		ProxyURL:               firstNonEmptyStringValue(c.PostForm("proxy_url"), c.PostForm("proxyUrl"), c.PostForm("proxy-url")),
		Prefix:                 c.PostForm("prefix"),
		RefreshIntervalSeconds: firstPositiveIntValue(c.PostForm("refresh_interval_seconds"), c.PostForm("refreshIntervalSeconds"), c.PostForm("refresh_interval"), c.PostForm("refreshInterval")),
	}
}

func codexImportOptionsFromMap(obj map[string]any) codexImportOptions {
	return codexImportOptions{
		EmailOverride:          firstNonEmptyStringValue(codexString(obj, "email_override"), codexString(obj, "emailOverride"), codexString(obj, "email")),
		PlanTypeOverride:       firstNonEmptyStringValue(codexString(obj, "plan_type_override"), codexString(obj, "planTypeOverride"), codexString(obj, "plan_type"), codexString(obj, "planType")),
		AccountIDOverride:      firstNonEmptyStringValue(codexString(obj, "account_id_override"), codexString(obj, "accountIdOverride"), codexString(obj, "account_id"), codexString(obj, "accountId")),
		ProxyURL:               firstNonEmptyStringValue(codexString(obj, "proxy_url"), codexString(obj, "proxyUrl"), codexString(obj, "proxy-url")),
		Prefix:                 codexString(obj, "prefix"),
		RefreshIntervalSeconds: firstPositiveIntValue(codexString(obj, "refresh_interval_seconds"), codexString(obj, "refreshIntervalSeconds"), codexString(obj, "refresh_interval"), codexString(obj, "refreshInterval")),
	}
}

func buildCodexImportRecord(source map[string]any, opts codexImportOptions) (*codexImportResult, error) {
	sourceRecord := readCodexSourceRecord(source)
	if sourceRecord.AccessToken == "" {
		return nil, fmt.Errorf("no Codex access token found")
	}
	if sourceRecord.IDToken == "" {
		return nil, fmt.Errorf("no Codex id_token found")
	}

	idClaims := parseJWTClaimsObject(sourceRecord.IDToken)
	accessClaims := parseJWTClaimsObject(sourceRecord.AccessToken)

	email := firstNonEmptyStringValue(
		opts.EmailOverride,
		codexString(source, "email"),
		codexString(idClaims, "email"),
	)
	if email == "" {
		return nil, fmt.Errorf("could not derive the Codex account email")
	}

	accountID := firstNonEmptyStringValue(
		opts.AccountIDOverride,
		sourceRecord.AccountID,
		nestedCodexString(idClaims, "https://api.openai.com/auth", "chatgpt_account_id"),
		codexString(idClaims, "sub"),
	)
	planType := firstNonEmptyStringValue(
		opts.PlanTypeOverride,
		codexString(source, "plan_type"),
		nestedCodexString(idClaims, "https://api.openai.com/auth", "chatgpt_plan_type"),
	)
	lastRefresh := firstNonEmptyStringValue(sourceRecord.LastRefresh, time.Now().UTC().Format(time.RFC3339Nano))
	expired := firstNonEmptyStringValue(
		codexString(source, "expired"),
		codexString(source, "expire"),
		jwtUnixSecondsToRFC3339(accessClaims, "exp"),
		jwtUnixSecondsToRFC3339(idClaims, "exp"),
	)

	accountIDHash := ""
	if normalizePlanTypeForCodexImport(planType) == "team" && accountID != "" {
		digest := sha256.Sum256([]byte(accountID))
		accountIDHash = hex.EncodeToString(digest[:])[:8]
	}

	storage := &codex.CodexTokenStorage{
		IDToken:      sourceRecord.IDToken,
		AccessToken:  sourceRecord.AccessToken,
		RefreshToken: sourceRecord.RefreshToken,
		AccountID:    accountID,
		LastRefresh:  lastRefresh,
		Email:        email,
		Expire:       expired,
	}

	metadata := map[string]any{
		"type":         "codex",
		"email":        email,
		"access_token": sourceRecord.AccessToken,
		"id_token":     sourceRecord.IDToken,
		"account_id":   accountID,
		"last_refresh": lastRefresh,
	}
	if sourceRecord.RefreshToken != "" {
		metadata["refresh_token"] = sourceRecord.RefreshToken
	}
	if expired != "" {
		metadata["expired"] = expired
	}
	if planType != "" {
		metadata["plan_type"] = planType
	}
	if proxyURL := strings.TrimSpace(opts.ProxyURL); proxyURL != "" {
		metadata["proxy_url"] = proxyURL
	}
	if prefix := normalizeAuthPrefix(opts.Prefix); prefix != "" {
		metadata["prefix"] = prefix
	}
	if opts.RefreshIntervalSeconds > 0 {
		metadata["refresh_interval_seconds"] = opts.RefreshIntervalSeconds
	}

	fileName := codex.CredentialFileName(email, planType, accountIDHash, true)
	record := &coreauth.Auth{
		ID:       fileName,
		Provider: "codex",
		FileName: fileName,
		Label:    email,
		Storage:  storage,
		Metadata: metadata,
		Prefix:   normalizeAuthPrefix(opts.Prefix),
		ProxyURL: strings.TrimSpace(opts.ProxyURL),
	}
	if planType != "" || opts.RefreshIntervalSeconds > 0 {
		record.Attributes = make(map[string]string)
		if planType != "" {
			record.Attributes["plan_type"] = strings.TrimSpace(planType)
		}
		if opts.RefreshIntervalSeconds > 0 {
			record.Attributes["refresh_interval_seconds"] = strconv.Itoa(opts.RefreshIntervalSeconds)
		}
	}

	return &codexImportResult{
		Record:          record,
		Email:           email,
		AccountID:       accountID,
		PlanType:        planType,
		Expired:         expired,
		LastRefresh:     lastRefresh,
		HasAccessToken:  sourceRecord.AccessToken != "",
		HasRefreshToken: sourceRecord.RefreshToken != "",
		HasIDToken:      sourceRecord.IDToken != "",
	}, nil
}

func readCodexSourceRecord(source map[string]any) codexSourceRecord {
	if tokens := codexMap(source, "tokens"); tokens != nil {
		return codexSourceRecord{
			AccessToken:  codexString(tokens, "access_token"),
			RefreshToken: codexString(tokens, "refresh_token"),
			IDToken:      codexString(tokens, "id_token"),
			AccountID:    codexString(tokens, "account_id"),
			LastRefresh:  codexString(source, "last_refresh"),
		}
	}

	if legacy := codexMap(source, "openai-codex"); legacy != nil {
		return codexSourceRecord{
			AccessToken:  codexString(legacy, "access"),
			RefreshToken: codexString(legacy, "refresh"),
			IDToken:      codexString(legacy, "id_token"),
			AccountID:    codexString(legacy, "accountId"),
			LastRefresh:  codexString(legacy, "lastRefresh"),
		}
	}

	return codexSourceRecord{
		AccessToken:  codexString(source, "access_token"),
		RefreshToken: codexString(source, "refresh_token"),
		IDToken:      codexString(source, "id_token"),
		AccountID:    codexString(source, "account_id"),
		LastRefresh:  codexString(source, "last_refresh"),
	}
}

func parseJWTClaimsObject(token string) map[string]any {
	token = strings.TrimSpace(token)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	payload := parts[1]
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}
	data, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return nil
	}
	obj, err := decodeJSONObject(data)
	if err != nil {
		return nil
	}
	return obj
}

func codexMap(obj map[string]any, key string) map[string]any {
	if obj == nil {
		return nil
	}
	if v, ok := obj[key].(map[string]any); ok {
		return v
	}
	return nil
}

func codexString(obj map[string]any, key string) string {
	if obj == nil {
		return ""
	}
	return strings.TrimSpace(codexAnyString(obj[key]))
}

func nestedCodexString(obj map[string]any, objectName, propertyName string) string {
	return codexString(codexMap(obj, objectName), propertyName)
}

func codexAnyString(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case json.Number:
		return val.String()
	default:
		return fmt.Sprint(val)
	}
}

func firstNonEmptyStringValue(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func firstPositiveIntValue(values ...string) int {
	for _, value := range values {
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}

func jwtUnixSecondsToRFC3339(claims map[string]any, key string) string {
	if claims == nil {
		return ""
	}
	seconds, ok := codexInt64(claims[key])
	if !ok || seconds <= 0 {
		return ""
	}
	return time.Unix(seconds, 0).UTC().Format(time.RFC3339)
}

func codexInt64(v any) (int64, bool) {
	switch val := v.(type) {
	case json.Number:
		i, err := val.Int64()
		return i, err == nil
	case float64:
		return int64(val), true
	case int64:
		return val, true
	case int:
		return int64(val), true
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
		return i, err == nil
	default:
		return 0, false
	}
}

func normalizePlanTypeForCodexImport(planType string) string {
	planType = strings.TrimSpace(planType)
	if planType == "" {
		return ""
	}
	parts := strings.FieldsFunc(planType, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(parts) == 0 {
		return ""
	}
	for i, part := range parts {
		parts[i] = strings.ToLower(strings.TrimSpace(part))
	}
	return strings.Join(parts, "-")
}

func normalizeAuthPrefix(prefix string) string {
	trimmed := strings.Trim(strings.TrimSpace(prefix), "/")
	if trimmed == "" || strings.Contains(trimmed, "/") {
		return ""
	}
	return trimmed
}
