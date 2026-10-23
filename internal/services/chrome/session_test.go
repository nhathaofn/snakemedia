package chrome

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/config"
	"github.com/gorilla/websocket"
)

const creditsRPC = "nzlxg" // reference fixture only; production resolves this ID
const availabilityRPC = "testAvailability"

func creditsFrame(payload string) string {
	frame, _ := json.Marshal([]any{[]any{"wrb.fr", creditsRPC, payload, nil, nil, nil, "generic"}})
	return fmt.Sprintf(")]}'\n\n%d\n%s\n", len(frame), frame)
}

func availabilityFrame(payload string) string {
	frame, _ := json.Marshal([]any{[]any{"wrb.fr", availabilityRPC, payload, nil, nil, nil, "generic"}})
	return fmt.Sprintf("%d\n%s\n", len(frame), frame)
}

func TestParseCreditsWireFormat(t *testing.T) {
	for _, balance := range []int{58, 0} {
		c, err := ParseCreditsFromText(creditsFrame(fmt.Sprintf("[%d,1,2,2,null,58,[1793162716,562000000]]", balance)), creditsRPC)
		if err != nil || c.Credits != balance || c.Tier != TierPro || c.DailyLimit != 58 || c.ResetAt == "" || c.ResetStatus != 2 || c.QuotaFlag != 2 {
			t.Fatalf("invalid credits: %+v %v", c, err)
		}
	}
	for _, bad := range []string{`[["e",4,null,null,16],[999,2,0]]`, `[["wrb.fr","other","[58,1,2,2,null,58,null]"]]`, creditsFrame(`[58,1]`), creditsFrame(`[-1,1,2,2,null,58,null]`), creditsFrame(`[58,1,2,2,null,58,[1,1000000000]]`), ")]}'\n99999\n[]", `[["wrb.fr","nzlxg",null,null,null,[16]]]`} {
		if c, err := ParseCreditsFromText(bad, creditsRPC); err == nil {
			t.Fatalf("accepted invalid response: %+v", c)
		}
	}
	if tierName(4) != TierUnsubscribed || tierName(6) != TierExempt || tierName(8) != TierUltra1P5 || tierName(99) != TierUnknown {
		t.Fatal("tier mapping incomplete")
	}

	// Real Google batchexecute response with length-prefixed chunk headers and trailing metadata frames
	realResponse := ")]}'\n\n146\n[[\"wrb.fr\",\"nzlxg\",\"[58,1,2,2,null,58,[1793162716,562000000]]\",null,null,null,\"generic\"],[\"di\",386],[\"af.httprm\",385,\"-7219182486955131569\",26]]\n25\n[[\"e\",4,null,null,182]]\n"
	realCredits, err := ParseCreditsFromText(realResponse, creditsRPC)
	if err != nil || realCredits.Credits != 58 || realCredits.Tier != TierPro || realCredits.DailyLimit != 58 {
		t.Fatalf("real Google response decoding failed: %+v %v", realCredits, err)
	}

	// Multi-chunk response containing non-ASCII / multibyte characters where byte length != character length
	unicodeResponse := ")]}'\n\n200\n[[\"wrb.fr\",\"nzlxg\",\"[100,1,2,2,null,100,[1793162716,562000000]]\",null,null,null,\"generic\"],[\"di\",1],[\"msg\",\"Tiếng Việt có dấu: đỗ đạt\"]] \n30\n[[\"e\",4,null,null,200]]\n"
	unicodeCredits, err := ParseCreditsFromText(unicodeResponse, creditsRPC)
	if err != nil || unicodeCredits.Credits != 100 {
		t.Fatalf("unicode response decoding failed: %+v %v", unicodeCredits, err)
	}
}

func TestInspectSessionCorrelatesEventsAndValidatesOrigin(t *testing.T) {
	for _, malicious := range []bool{false, true} {
		t.Run(strconv.FormatBool(malicious), func(t *testing.T) {
			var server *httptest.Server
			upgrader := websocket.Upgrader{}
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/json" {
					target := FlowURL
					if malicious {
						target = "https://flow.google.com.attacker.invalid"
					}
					_ = json.NewEncoder(w).Encode([]pageEntry{{Type: "page", URL: target, WebSocketDebuggerURL: "ws" + strings.TrimPrefix(server.URL, "http") + "/devtools/page/test"}})
					return
				}
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				for {
					var msg struct {
						ID     int    `json:"id"`
						Method string `json:"method"`
					}
					if conn.ReadJSON(&msg) != nil {
						return
					}
					_ = conn.WriteJSON(map[string]any{"method": "Runtime.consoleAPICalled", "params": map[string]any{}})
					var result any
					switch msg.ID {
					case 1:
						identity, _ := json.Marshal(map[string]any{"origin": FlowURL, "email": "a@example.invalid", "gaiaId": "test-gaia", "credentials": Credentials{RPCs: RPCRegistry{Credits: creditsRPC, Availability: availabilityRPC}, AtToken: "test-at", SessionID: "test-session", BuildLabel: "runtime-build", Language: "en", UserAgent: "runtime-agent", SourcePath: "/"}})
						result = map[string]any{"result": map[string]any{"value": string(identity)}}
					case 2:
						result = map[string]any{"cookies": []CookieItem{{Name: "test", Value: "secret", Domain: "flow.google.com", Path: "/"}}}
					case 3:
						rpc, _ := json.Marshal(map[string]any{"status": http.StatusOK, "text": creditsFrame(`[0,1,2,2,null,58,null]`) + availabilityFrame(`[1]`)})
						result = map[string]any{"result": map[string]any{"value": string(rpc)}}
					}
					if conn.WriteJSON(map[string]any{"id": msg.ID, "result": result}) != nil {
						return
					}
				}
			}))
			defer server.Close()
			u, _ := url.Parse(server.URL)
			port, _ := strconv.Atoi(u.Port())
			got, err := InspectFlowSession(context.Background(), port, config.DefaultAccountConfig())
			if malicious {
				if err == nil {
					t.Fatal("substring origin accepted")
				}
				return
			}
			if err != nil || !got.IsLoggedIn || got.Credits.Credits != 0 {
				t.Fatalf("CDP response correlation failed: %v", err)
			}
		})
	}
}

func TestBoundedResponsesAndCancellation(t *testing.T) {
	if _, err := readBounded(strings.NewReader("too large"), 2); err == nil {
		t.Fatal("size limit ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := InspectFlowSession(ctx, 0, config.DefaultAccountConfig()); err == nil {
		t.Fatal("canceled work continued")
	}
	if err := responseError(http.StatusTooManyRequests, "60"); err.(*HTTPError).RetryAfter != time.Minute {
		t.Fatal("Retry-After ignored")
	}
}

func TestSignedInIdentitySurvivesRPCDiscoveryFailure(t *testing.T) {
	for _, identityOnly := range []bool{true, false} {
		t.Run(strconv.FormatBool(identityOnly), func(t *testing.T) {
			var server *httptest.Server
			upgrader := websocket.Upgrader{}
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/json" {
					_ = json.NewEncoder(w).Encode([]pageEntry{{Type: "page", URL: FlowURL, WebSocketDebuggerURL: "ws" + strings.TrimPrefix(server.URL, "http") + "/page/test"}})
					return
				}
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				for {
					var msg struct {
						ID     int    `json:"id"`
						Method string `json:"method"`
					}
					if conn.ReadJSON(&msg) != nil {
						return
					}
					var result any
					switch msg.Method {
					case "Runtime.evaluate":
						identity, _ := json.Marshal(map[string]any{"origin": FlowURL, "email": "signed-in@example.invalid", "gaiaId": "test-gaia", "credentials": Credentials{BuildLabel: "discovery-error-test"}})
						result = map[string]any{"result": map[string]any{"value": string(identity)}}
					case "Network.getCookies":
						result = map[string]any{"cookies": []CookieItem{{Name: "test", Value: "not-a-real-cookie", Domain: "flow.google.com", Path: "/"}}}
					case "Debugger.enable":
						if identityOnly {
							t.Error("identity detection attempted RPC discovery")
						}
						_ = conn.WriteJSON(map[string]any{"id": msg.ID, "error": map[string]any{"code": -32601}})
						continue
					default:
						t.Errorf("unexpected method %s", msg.Method)
					}
					if conn.WriteJSON(map[string]any{"id": msg.ID, "result": result}) != nil {
						return
					}
				}
			}))
			defer server.Close()
			u, _ := url.Parse(server.URL)
			port, _ := strconv.Atoi(u.Port())
			inspect := InspectFlowSession
			if identityOnly {
				inspect = InspectFlowIdentity
			}
			got, err := inspect(context.Background(), port, config.DefaultAccountConfig())
			if err != nil || got == nil || !got.IsLoggedIn || got.CreditsVerified {
				t.Fatalf("signed-in identity hidden by RPC failure: %+v %v", got, err)
			}
			if !identityOnly && got.CreditsErr == nil {
				t.Fatal("missing enrichment diagnostic")
			}
		})
	}
}
