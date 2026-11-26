package chrome

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"SnakeMedia/internal/config"
	"github.com/gorilla/websocket"
)

func TestOpenProjectWaitsForEditor(t *testing.T) {
	const id = "ed552eeb-8538-410d-aee8-47281ac0bf1f"
	for _, startOnProject := range []bool{false, true} {
		t.Run(strconv.FormatBool(startOnProject), func(t *testing.T) {
			var navigations, checks atomic.Int32
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/json" {
					target := FlowURL
					if startOnProject {
						target += "/project/" + id
					}
					_ = json.NewEncoder(w).Encode([]pageEntry{{Type: "page", URL: target, WebSocketDebuggerURL: "ws" + strings.TrimPrefix(server.URL, "http") + "/devtools/page/flow"}})
					return
				}
				upgrader := websocket.Upgrader{}
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				for {
					var call struct {
						ID     int    `json:"id"`
						Method string `json:"method"`
						Params struct {
							URL string `json:"url"`
						} `json:"params"`
					}
					if err := conn.ReadJSON(&call); err != nil {
						return
					}
					result := map[string]any{}
					switch call.Method {
					case "Page.navigate":
						navigations.Add(1)
						if call.Params.URL != FlowURL+"/project/"+id {
							t.Error("navigation did not target the requested project")
						}
					case "Runtime.evaluate":
						result["result"] = map[string]string{"value": strconv.FormatBool(checks.Add(1) >= 2)}
					default:
						t.Errorf("unexpected operation %s", call.Method)
					}
					if err := conn.WriteJSON(map[string]any{"id": call.ID, "result": result}); err != nil {
						return
					}
				}
			}))
			defer server.Close()
			u, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			port, err := strconv.Atoi(u.Port())
			if err != nil {
				t.Fatal(err)
			}
			policy := config.DefaultFlowPagePolicy()
			policy.PollIntervalMS, policy.BrowserStartupTimeoutSeconds = 5, 2
			if err := OpenProject(context.Background(), port, id, policy); err != nil {
				t.Fatal(err)
			}
			wantNavigations := int32(1)
			if startOnProject {
				wantNavigations = 0
			}
			if navigations.Load() != wantNavigations || checks.Load() != 2 {
				t.Fatalf("navigations=%d, editor checks=%d", navigations.Load(), checks.Load())
			}
		})
	}
}
