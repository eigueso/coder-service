package coder

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/malmonte/go-backend/internal/config"
)

// newTestClient starts a stub Coder server and returns a client pointed at it.
func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	settings := &config.Settings{CoderURL: server.URL}
	return NewClient(settings, "test-token", server.Client())
}

func apiErrorFrom(t *testing.T, err error) *APIError {
	t.Helper()
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	return apiErr
}

func TestMeSendsAuthHeaders(t *testing.T) {
	var gotToken, gotAccept string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("Coder-Session-Token")
		gotAccept = r.Header.Get("Accept")
		if r.URL.Path != "/api/v2/users/me" {
			t.Errorf("path = %q", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"username": "alice"})
	}))

	user, err := client.Me(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if user["username"] != "alice" {
		t.Errorf("user = %v", user)
	}
	if gotToken != "test-token" {
		t.Errorf("Coder-Session-Token = %q", gotToken)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q", gotAccept)
	}
}

func TestStatusErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantStatus int
		wantDetail string
	}{
		{"401 masks detail", http.StatusUnauthorized, `{"message":"secret"}`,
			http.StatusUnauthorized, "Invalid or expired session token"},
		{"403 becomes 401", http.StatusForbidden, `{}`,
			http.StatusUnauthorized, "Invalid or expired session token"},
		{"404 extracts message", http.StatusNotFound, `{"message":"no workspace"}`,
			http.StatusNotFound, "no workspace"},
		{"400 extracts detail", http.StatusBadRequest, `{"detail":"bad input"}`,
			http.StatusBadRequest, "bad input"},
		{"409 extracts error", http.StatusConflict, `{"error":"conflicting build"}`,
			http.StatusConflict, "conflicting build"},
		{"500 becomes 502", http.StatusInternalServerError, `boom`,
			http.StatusBadGateway, "Coder request failed with status 500: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			}))
			_, err := client.Me(context.Background())
			apiErr := apiErrorFrom(t, err)
			if apiErr.Status != tt.wantStatus {
				t.Errorf("status = %d, want %d", apiErr.Status, tt.wantStatus)
			}
			if apiErr.Detail != tt.wantDetail {
				t.Errorf("detail = %q, want %q", apiErr.Detail, tt.wantDetail)
			}
		})
	}
}

func TestRequestConnectionErrorIs502(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	settings := &config.Settings{CoderURL: server.URL}
	client := NewClient(settings, "tok", server.Client())
	server.Close()

	_, err := client.Me(context.Background())
	apiErr := apiErrorFrom(t, err)
	if apiErr.Status != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", apiErr.Status)
	}
}

func TestRequestInvalidJSONIs502(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	_, err := client.Me(context.Background())
	apiErr := apiErrorFrom(t, err)
	if apiErr.Status != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", apiErr.Status)
	}
}

func TestNonObjectResponseIs502(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[1,2,3]`))
	}))
	_, err := client.Me(context.Background())
	apiErr := apiErrorFrom(t, err)
	if apiErr.Status != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", apiErr.Status)
	}
}

func TestListWorkspacesQuery(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query().Get("q"); q != "owner:me" {
			t.Errorf("q = %q, want owner:me", q)
		}
		json.NewEncoder(w).Encode(map[string]any{"count": 0, "workspaces": []any{}})
	}))
	if _, err := client.ListWorkspaces(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
}

func TestGetWorkspaceEscapesName(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/v2/users/me/workspace/my%20ws" {
			t.Errorf("path = %q", r.URL.EscapedPath())
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "1"})
	}))
	if _, err := client.GetWorkspace(context.Background(), "my ws"); err != nil {
		t.Fatal(err)
	}
}

func TestCreateWorkspaceBuildBody(t *testing.T) {
	var body map[string]any
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v2/workspaces/ws-1/builds" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&body)
		json.NewEncoder(w).Encode(map[string]any{"id": "b1"})
	}))
	if _, err := client.CreateWorkspaceBuild(context.Background(), "ws-1", "delete", true); err != nil {
		t.Fatal(err)
	}
	if body["transition"] != "delete" || body["orphan"] != true {
		t.Errorf("body = %v", body)
	}
}

func TestCreateWorkspaceBuildOmitsOrphan(t *testing.T) {
	var body map[string]any
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		json.NewEncoder(w).Encode(map[string]any{"id": "b1"})
	}))
	if _, err := client.CreateWorkspaceBuild(context.Background(), "ws-1", "stop", false); err != nil {
		t.Fatal(err)
	}
	if _, present := body["orphan"]; present {
		t.Errorf("orphan should be omitted, body = %v", body)
	}
}

func TestDeleteWorkspaceTwoStep(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/users/me/workspace/dev":
			json.NewEncoder(w).Encode(map[string]any{"id": "ws-9"})
		case "/api/v2/workspaces/ws-9/builds":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["transition"] != "delete" {
				t.Errorf("transition = %v", body["transition"])
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "b2", "workspace_id": "ws-9"})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	build, err := client.DeleteWorkspace(context.Background(), "dev", false)
	if err != nil {
		t.Fatal(err)
	}
	if build["id"] != "b2" {
		t.Errorf("build = %v", build)
	}
}

func TestGetWorkspaceBuildLogsJSON(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("after") != "5" || q.Get("before") != "10" {
			t.Errorf("query = %v", q)
		}
		if q.Has("format") {
			t.Error("format should be omitted for json")
		}
		json.NewEncoder(w).Encode([]any{map[string]any{"id": 1}})
	}))
	after, before := int64(5), int64(10)
	result, err := client.GetWorkspaceBuildLogs(context.Background(), "b1", &after, &before, "json")
	if err != nil {
		t.Fatal(err)
	}
	if entries, ok := result.([]any); !ok || len(entries) != 1 {
		t.Errorf("result = %v", result)
	}
}

func TestGetWorkspaceBuildLogsText(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "text" {
			t.Errorf("format = %q", r.URL.Query().Get("format"))
		}
		w.Write([]byte("line one\nline two"))
	}))
	result, err := client.GetWorkspaceBuildLogs(context.Background(), "b1", nil, nil, "text")
	if err != nil {
		t.Fatal(err)
	}
	if result != "line one\nline two" {
		t.Errorf("result = %v", result)
	}
}

func TestGetAgentLogs(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/workspaceagents/agent-1/logs" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("after") != "3" {
			t.Errorf("after = %q", r.URL.Query().Get("after"))
		}
		json.NewEncoder(w).Encode([]any{map[string]any{"id": 4}})
	}))
	after := int64(3)
	entries, err := client.GetAgentLogs(context.Background(), "agent-1", &after)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("entries = %v", entries)
	}
}

func TestGetAgentLogsNonListIs502(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"oops": true})
	}))
	_, err := client.GetAgentLogs(context.Background(), "agent-1", nil)
	apiErr := apiErrorFrom(t, err)
	if apiErr.Status != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", apiErr.Status)
	}
}

func TestFindUserByEmail(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "Alice@Example.com" {
			t.Errorf("q = %q", r.URL.Query().Get("q"))
		}
		json.NewEncoder(w).Encode(map[string]any{"users": []any{
			map[string]any{"id": "u1", "email": "bob@example.com"},
			map[string]any{"id": "u2", "email": "alice@example.com"},
		}})
	}))
	user, err := client.FindUserByEmail(context.Background(), "Alice@Example.com")
	if err != nil {
		t.Fatal(err)
	}
	if user["id"] != "u2" {
		t.Errorf("user = %v", user)
	}
}

func TestFindUserByEmailNotFound(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"users": []any{}})
	}))
	_, err := client.FindUserByEmail(context.Background(), "ghost@example.com")
	apiErr := apiErrorFrom(t, err)
	if apiErr.Status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", apiErr.Status)
	}
}

func TestFindUserByEmailBadPayloadIs502(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"users": "nope"})
	}))
	_, err := client.FindUserByEmail(context.Background(), "alice@example.com")
	apiErr := apiErrorFrom(t, err)
	if apiErr.Status != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", apiErr.Status)
	}
}

func TestCreateTokenForUser(t *testing.T) {
	var body map[string]any
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/users/u1/keys/tokens" {
			t.Errorf("path = %q", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&body)
		json.NewEncoder(w).Encode(map[string]any{"key": "secret-key"})
	}))
	key, err := client.CreateTokenForUser(context.Background(), "u1", "my-token", 0)
	if err != nil {
		t.Fatal(err)
	}
	if key != "secret-key" {
		t.Errorf("key = %q", key)
	}
	if body["token_name"] != "my-token" {
		t.Errorf("token_name = %v", body["token_name"])
	}
	// Zero lifetime uses the 24h default (in nanoseconds).
	if body["lifetime"] != float64(defaultTokenLifetimeNs) {
		t.Errorf("lifetime = %v", body["lifetime"])
	}
}

func TestCreateTokenForUserCustomLifetime(t *testing.T) {
	var body map[string]any
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		json.NewEncoder(w).Encode(map[string]any{"key": "k"})
	}))
	if _, err := client.CreateTokenForUser(context.Background(), "u1", "t", 42); err != nil {
		t.Fatal(err)
	}
	if body["lifetime"] != float64(42) {
		t.Errorf("lifetime = %v", body["lifetime"])
	}
}

func TestCreateTokenForUserMissingKeyIs502(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{})
	}))
	_, err := client.CreateTokenForUser(context.Background(), "u1", "t", 0)
	apiErr := apiErrorFrom(t, err)
	if apiErr.Status != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", apiErr.Status)
	}
}

func TestEmptySuccessBodyReturnsNil(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	payload, err := client.request(context.Background(), http.MethodGet, "/api/v2/x", nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		t.Errorf("payload = %v, want nil", payload)
	}
}

func TestAPIErrorError(t *testing.T) {
	err := NewAPIError(http.StatusTeapot, "short and stout")
	if err.Error() != "short and stout" {
		t.Errorf("Error() = %q", err.Error())
	}
}

func TestBuildInfo(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/buildinfo" {
			t.Errorf("path = %q", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"dashboard_url": "http://dash"})
	}))
	info, err := client.BuildInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info["dashboard_url"] != "http://dash" {
		t.Errorf("info = %v", info)
	}
}

func TestCreateWorkspace(t *testing.T) {
	var body map[string]any
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v2/users/me/workspaces" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&body)
		json.NewEncoder(w).Encode(map[string]any{"id": "ws-1"})
	}))
	workspace, err := client.CreateWorkspace(context.Background(), map[string]any{"name": "dev"})
	if err != nil {
		t.Fatal(err)
	}
	if workspace["id"] != "ws-1" || body["name"] != "dev" {
		t.Errorf("workspace = %v, body = %v", workspace, body)
	}
}

func TestGetWorkspaceBuild(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/workspacebuilds/b1" {
			t.Errorf("path = %q", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "b1"})
	}))
	build, err := client.GetWorkspaceBuild(context.Background(), "b1")
	if err != nil {
		t.Fatal(err)
	}
	if build["id"] != "b1" {
		t.Errorf("build = %v", build)
	}
}
