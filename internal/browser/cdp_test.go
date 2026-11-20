package browser

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"SnakeMedia/internal/config"
	"github.com/gorilla/websocket"
)

func TestCloseTabHTTPStatuses(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/json/close/tab-ok":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Target is closing"))
		case "/json/close/tab-gone":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	policy := config.DefaultAccountConfig()
	ctx := context.Background()

	if err := CloseTab(ctx, port, policy, ""); err != nil {
		t.Fatalf("empty targetID should be no-op, got %v", err)
	}
	if err := CloseTab(ctx, port, policy, "tab-ok"); err != nil {
		t.Fatalf("HTTP 200 CloseTab = %v", err)
	}
	if err := CloseTab(ctx, port, policy, "tab-gone"); err != nil {
		t.Fatalf("HTTP 404 CloseTab should succeed, got %v", err)
	}
	if err := CloseTab(ctx, port, policy, "tab-err"); err == nil {
		t.Fatal("expected HTTP 500 CloseTab to error")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 3 {
		t.Fatalf("requests = %v, want 3 calls", paths)
	}
}

func TestEvaluateReturnsEvalErrorOnRejectedObjectValue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var req struct {
			ID int `json:"id"`
		}
		if conn.ReadJSON(&req) != nil {
			return
		}
		// Chrome puts result.value = {} (an object) alongside exceptionDetails when a Promise rejects with an Error.
		_ = conn.WriteJSON(map[string]any{
			"id": req.ID,
			"result": map[string]any{
				"result": map[string]any{"type": "object", "subtype": "error", "value": map[string]any{}},
				"exceptionDetails": map[string]any{
					"text":      "Uncaught (in promise)",
					"exception": map[string]any{"description": "Error: sessions timeout"},
				},
			},
		})
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	var out map[string]any
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = Evaluate(ctx, ws, 1, "ignored", &out)
	var evalErr *EvalError
	if !errors.As(err, &evalErr) || !strings.Contains(evalErr.Detail, "sessions timeout") {
		t.Fatalf("Evaluate error = %v, want *EvalError containing sessions timeout", err)
	}
}
