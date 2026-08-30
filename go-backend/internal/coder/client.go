// Package coder is a thin HTTP client for Coder's public API, mirroring
// backend/app/coder_client.py. Error status mapping and detail extraction
// match the Python implementation so API consumers see identical responses.
package coder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/malmonte/go-backend/internal/config"
)

// APIError is the service's error type. Handlers serialize it the way FastAPI
// serializes HTTPException: {"detail": "..."} with the given status code.
type APIError struct {
	Status int
	Detail string
}

func (e *APIError) Error() string { return e.Detail }

// NewAPIError builds an APIError with the given HTTP status and detail.
func NewAPIError(status int, detail string) *APIError {
	return &APIError{Status: status, Detail: detail}
}

// Client calls Coder's public API with a per-request session token.
type Client struct {
	settings     *config.Settings
	sessionToken string
	http         *http.Client
}

// NewClient builds a Client bound to one session token. The shared
// *http.Client provides connection pooling across requests.
func NewClient(settings *config.Settings, sessionToken string, httpClient *http.Client) *Client {
	return &Client{settings: settings, sessionToken: sessionToken, http: httpClient}
}

// request performs one API call. When expectJSON is true the decoded JSON
// value is returned; otherwise the raw response body is returned as a string.
func (c *Client) request(
	ctx context.Context,
	method, path string,
	body any,
	params url.Values,
	expectJSON bool,
) (any, error) {
	target := c.settings.CoderAPIBase() + path
	if len(params) > 0 {
		target += "?" + params.Encode()
	}

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Coder-Session-Token", c.sessionToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, NewAPIError(http.StatusBadGateway,
			fmt.Sprintf("Failed to reach Coder at %s: %v", c.settings.CoderAPIBase(), err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, NewAPIError(http.StatusBadGateway,
			fmt.Sprintf("Failed to reach Coder at %s: %v", c.settings.CoderAPIBase(), err))
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if !expectJSON {
			return string(raw), nil
		}
		if len(raw) == 0 {
			return nil, nil
		}
		var payload any
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, NewAPIError(http.StatusBadGateway,
				fmt.Sprintf("Coder returned invalid JSON: %v", err))
		}
		return payload, nil
	}

	return nil, statusError(resp.StatusCode, raw)
}

// statusError maps Coder response codes to the service's error contract,
// extracting the most useful detail string from the body.
func statusError(status int, raw []byte) *APIError {
	detail := string(raw)
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err == nil {
		for _, key := range []string{"detail", "message", "error"} {
			if value, ok := payload[key].(string); ok && value != "" {
				detail = value
				break
			}
		}
	}

	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return NewAPIError(http.StatusUnauthorized, "Invalid or expired session token")
	case http.StatusNotFound:
		if detail == "" {
			detail = "Resource not found in Coder"
		}
		return NewAPIError(http.StatusNotFound, detail)
	case http.StatusBadRequest:
		if detail == "" {
			detail = "Bad request to Coder"
		}
		return NewAPIError(http.StatusBadRequest, detail)
	case http.StatusConflict:
		if detail == "" {
			detail = "Conflict in Coder"
		}
		return NewAPIError(http.StatusConflict, detail)
	}
	return NewAPIError(http.StatusBadGateway,
		fmt.Sprintf("Coder request failed with status %d: %s", status, detail))
}

func asObject(payload any, context string) (map[string]any, error) {
	obj, ok := payload.(map[string]any)
	if !ok {
		return nil, NewAPIError(http.StatusBadGateway, "Unexpected response "+context)
	}
	return obj, nil
}

// ListWorkspaces lists workspaces, defaulting to the authenticated user
// (owner:me).
func (c *Client) ListWorkspaces(ctx context.Context, owner string) (map[string]any, error) {
	if owner == "" {
		owner = "me"
	}
	params := url.Values{"q": {"owner:" + owner}}
	payload, err := c.request(ctx, http.MethodGet, "/api/v2/workspaces", nil, params, true)
	if err != nil {
		return nil, err
	}
	return asObject(payload, "listing Coder workspaces")
}

// Me returns the authenticated Coder user.
func (c *Client) Me(ctx context.Context) (map[string]any, error) {
	payload, err := c.request(ctx, http.MethodGet, "/api/v2/users/me", nil, nil, true)
	if err != nil {
		return nil, err
	}
	return asObject(payload, "fetching the Coder user")
}

// BuildInfo returns Coder deployment build info (includes dashboard_url).
func (c *Client) BuildInfo(ctx context.Context) (map[string]any, error) {
	payload, err := c.request(ctx, http.MethodGet, "/api/v2/buildinfo", nil, nil, true)
	if err != nil {
		return nil, err
	}
	return asObject(payload, "fetching Coder buildinfo")
}

// CreateWorkspace creates a workspace for the authenticated user.
func (c *Client) CreateWorkspace(ctx context.Context, body map[string]any) (map[string]any, error) {
	payload, err := c.request(ctx, http.MethodPost, "/api/v2/users/me/workspaces", body, nil, true)
	if err != nil {
		return nil, err
	}
	return asObject(payload, "creating the Coder workspace")
}

// GetWorkspace looks up a workspace by name for the authenticated user.
func (c *Client) GetWorkspace(ctx context.Context, name string) (map[string]any, error) {
	payload, err := c.request(ctx, http.MethodGet,
		"/api/v2/users/me/workspace/"+url.PathEscape(name), nil, nil, true)
	if err != nil {
		return nil, err
	}
	return asObject(payload, "fetching the Coder workspace")
}

// CreateWorkspaceBuild starts a workspace build (start / stop / delete).
func (c *Client) CreateWorkspaceBuild(
	ctx context.Context, workspaceID, transition string, orphan bool,
) (map[string]any, error) {
	body := map[string]any{"transition": transition}
	if orphan {
		body["orphan"] = true
	}
	payload, err := c.request(ctx, http.MethodPost,
		"/api/v2/workspaces/"+url.PathEscape(workspaceID)+"/builds", body, nil, true)
	if err != nil {
		return nil, err
	}
	return asObject(payload, "creating the Coder workspace build")
}

// DeleteWorkspace deletes a workspace by name via a delete-transition build.
func (c *Client) DeleteWorkspace(ctx context.Context, name string, orphan bool) (map[string]any, error) {
	workspace, err := c.GetWorkspace(ctx, name)
	if err != nil {
		return nil, err
	}
	id, _ := workspace["id"].(string)
	return c.CreateWorkspaceBuild(ctx, id, "delete", orphan)
}

// GetWorkspaceBuild fetches a workspace build by ID.
func (c *Client) GetWorkspaceBuild(ctx context.Context, buildID string) (map[string]any, error) {
	payload, err := c.request(ctx, http.MethodGet,
		"/api/v2/workspacebuilds/"+url.PathEscape(buildID), nil, nil, true)
	if err != nil {
		return nil, err
	}
	return asObject(payload, "fetching the Coder workspace build")
}

// GetWorkspaceBuildLogs fetches provisioner logs for a build (HTTP snapshot;
// no follow). Returns []any for JSON format or string for text format.
func (c *Client) GetWorkspaceBuildLogs(
	ctx context.Context, buildID string, after, before *int64, format string,
) (any, error) {
	params := url.Values{}
	if after != nil {
		params.Set("after", strconv.FormatInt(*after, 10))
	}
	if before != nil {
		params.Set("before", strconv.FormatInt(*before, 10))
	}
	if format != "" && format != "json" {
		params.Set("format", format)
	}
	expectJSON := format != "text"
	return c.request(ctx, http.MethodGet,
		"/api/v2/workspacebuilds/"+url.PathEscape(buildID)+"/logs", nil, params, expectJSON)
}

// GetAgentLogs fetches workspace agent / startup-script logs.
func (c *Client) GetAgentLogs(ctx context.Context, agentID string, after *int64) ([]any, error) {
	params := url.Values{}
	if after != nil {
		params.Set("after", strconv.FormatInt(*after, 10))
	}
	payload, err := c.request(ctx, http.MethodGet,
		"/api/v2/workspaceagents/"+url.PathEscape(agentID)+"/logs", nil, params, true)
	if err != nil {
		return nil, err
	}
	entries, ok := payload.([]any)
	if !ok {
		return nil, NewAPIError(http.StatusBadGateway, "Unexpected response fetching Coder agent logs")
	}
	return entries, nil
}

// FindUserByEmail resolves a Coder user by email via GET /api/v2/users?q=...
func (c *Client) FindUserByEmail(ctx context.Context, email string) (map[string]any, error) {
	payload, err := c.request(ctx, http.MethodGet, "/api/v2/users", nil, url.Values{"q": {email}}, true)
	if err != nil {
		return nil, err
	}
	obj, ok := payload.(map[string]any)
	if !ok {
		return nil, NewAPIError(http.StatusBadGateway, "Unexpected response listing Coder users")
	}
	users, ok := obj["users"].([]any)
	if !ok {
		return nil, NewAPIError(http.StatusBadGateway, "Unexpected response listing Coder users")
	}
	needle := strings.ToLower(strings.TrimSpace(email))
	for _, entry := range users {
		user, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if candidate, _ := user["email"].(string); strings.ToLower(candidate) == needle {
			return user, nil
		}
	}
	return nil, NewAPIError(http.StatusNotFound, "No Coder user found for email "+email)
}

// defaultTokenLifetimeNs is 24 hours, matching the Python backend's default.
const defaultTokenLifetimeNs = 86_400_000_000_000

// CreateTokenForUser mints a named API token for userID (Owner-only for other
// users). A lifetimeNs of zero or less uses the 24-hour default.
func (c *Client) CreateTokenForUser(
	ctx context.Context, userID, tokenName string, lifetimeNs int64,
) (string, error) {
	if lifetimeNs <= 0 {
		lifetimeNs = defaultTokenLifetimeNs
	}
	payload, err := c.request(ctx, http.MethodPost,
		"/api/v2/users/"+url.PathEscape(userID)+"/keys/tokens",
		map[string]any{"token_name": tokenName, "lifetime": lifetimeNs}, nil, true)
	if err != nil {
		return "", err
	}
	obj, _ := payload.(map[string]any)
	key, _ := obj["key"].(string)
	if key == "" {
		return "", NewAPIError(http.StatusBadGateway,
			"Coder create token succeeded but no key was returned")
	}
	return key, nil
}
