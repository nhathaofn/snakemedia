package chrome

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"SnakeMedia/internal/config"
	"github.com/gorilla/websocket"
)

// Provider constants are wire protocol identifiers, never captured session data.
const FlowURL = "https://flow.google.com"
const batchPath = "/_/AiSandboxAngularFrontend/data/batchexecute"

var ErrLoginRequired = errors.New("Phiên Flow chưa đăng nhập.")
var ErrPageNotReady = errors.New("Đang chờ trang Flow tải xong.")

type CookieItem struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	HTTPOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
	SameSite string  `json:"sameSite,omitempty"`
}

// Credentials never cross the Wails bridge. All values originate in the same
// verified browser profile. Cookies are restricted to the provider origin.
type Credentials struct {
	RPCs       RPCRegistry  `json:"rpcs"`
	Cookies    []CookieItem `json:"cookies"`
	AtToken    string       `json:"atToken"`
	SessionID  string       `json:"sessionId"`
	BuildLabel string       `json:"buildLabel"`
	Language   string       `json:"language"`
	UserAgent  string       `json:"userAgent"`
	SourcePath string       `json:"sourcePath"`
	AuthUser   string       `json:"authUser,omitempty"`
}

type FlowSessionDetails struct {
	Email           string      `json:"email"`
	GaiaID          string      `json:"gaiaId"`
	Title           string      `json:"title"`
	IsLoggedIn      bool        `json:"isLoggedIn"`
	Credits         FlowCredits `json:"credits"`
	CreditsVerified bool        `json:"creditsVerified"`
	CreditsErr      error       `json:"-"`
	Credentials     Credentials `json:"-"`
}

type pageEntry struct {
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// cdpCall correlates responses and skips unsolicited events. Cancellation closes
// the connection, including a pending read/write or an awaited page promise.
func cdpCall(ctx context.Context, conn *websocket.Conn, id int, method string, params any, result any, handlers ...func(string, json.RawMessage)) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		return errors.New("CDP operation requires a deadline")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_ = conn.SetWriteDeadline(deadline)
	_ = conn.SetReadDeadline(deadline)
	if err := conn.WriteJSON(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	for {
		var msg struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code int `json:"code"`
			} `json:"error"`
		}
		if err := conn.ReadJSON(&msg); err != nil {
			return err
		}
		if msg.ID != id {
			for _, handle := range handlers {
				handle(msg.Method, msg.Params)
			}
			continue
		}
		if msg.Error != nil {
			return fmt.Errorf("CDP %s failed (%d)", method, msg.Error.Code)
		}
		if result == nil {
			return nil
		}
		return json.Unmarshal(msg.Result, result)
	}
}

func evaluate(ctx context.Context, conn *websocket.Conn, id int, expression string, out any) error {
	var result struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
		Exception json.RawMessage `json:"exceptionDetails"`
	}
	if err := cdpCall(ctx, conn, id, "Runtime.evaluate", map[string]any{"expression": expression, "awaitPromise": true, "returnByValue": true}, &result); err != nil {
		return err
	}
	if len(result.Exception) > 0 || result.Result.Value == "" {
		return errors.New("Flow page evaluation failed")
	}
	return json.Unmarshal([]byte(result.Result.Value), out)
}

// InspectFlowIdentity detects the authenticated page identity without waiting on
// optional balance/capability APIs. A failure there must not hide the account.
func InspectFlowIdentity(ctx context.Context, port int, policy config.AccountConfig) (*FlowSessionDetails, error) {
	return inspectFlowSession(ctx, port, policy, false)
}

func InspectFlowSession(ctx context.Context, port int, policy config.AccountConfig) (*FlowSessionDetails, error) {
	return inspectFlowSession(ctx, port, policy, true)
}

func inspectFlowSession(ctx context.Context, port int, policy config.AccountConfig, includeCredits bool) (details *FlowSessionDetails, err error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(policy.RequestTimeoutSeconds)*time.Second)
	defer cancel()
	defer func() {
		// A verified page identity survives optional enrichment errors. Preserve the
		// error separately, and never turn cancellation into a successful save.
		if err != nil && details != nil && details.IsLoggedIn && ctx.Err() == nil {
			details.CreditsErr = err
			err = nil
		}
	}()
	var pages []pageEntry
	if err := localJSON(ctx, port, "/json", policy, &pages); err != nil {
		return nil, err
	}
	var target string
	for _, p := range pages {
		u, err := url.Parse(p.URL)
		if err == nil && p.Type == "page" && u.Scheme == "https" && u.Host == "flow.google.com" {
			target = p.WebSocketDebuggerURL
			break
		}
	}
	if target == "" {
		return nil, ErrPageNotReady
	}
	if !localWebSocket(target, port) {
		return nil, errors.New("invalid CDP page endpoint")
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, target, nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetReadLimit(max(policy.MaxResponseBytes, policy.MaxDiscoveryBytes))
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	var identity struct {
		Origin      string      `json:"origin"`
		Email       string      `json:"email"`
		GaiaID      string      `json:"gaiaId"`
		Title       string      `json:"title"`
		Credentials Credentials `json:"credentials"`
	}
	err = evaluate(ctx, conn, 1, `JSON.stringify({
		origin:location.origin,email:String(window.WIZ_global_data?.oPEP7c||''),gaiaId:String(window.WIZ_global_data?.S06Grb||''),title:document.title,
		credentials:{atToken:window.WIZ_global_data?.SNlM0e||'',sessionId:window.WIZ_global_data?.f_sid||window.WIZ_global_data?.FdrFJe||'',
		buildLabel:window.WIZ_global_data?.cfb2h||'',language:document.documentElement.lang||navigator.language,
		userAgent:navigator.userAgent,sourcePath:location.pathname,authUser:new URL(location.href).searchParams.get('authuser')||''}
	})`, &identity)
	if err != nil {
		return nil, err
	}
	if identity.Origin != FlowURL {
		return nil, ErrPageNotReady
	}
	details = &FlowSessionDetails{Email: identity.Email, GaiaID: identity.GaiaID, Title: identity.Title, Credentials: identity.Credentials}
	if identity.Email == "" || identity.GaiaID == "" {
		return details, nil
	}
	details.IsLoggedIn = true
	var cookies struct {
		Cookies []CookieItem `json:"cookies"`
	}
	if err := cdpCall(ctx, conn, 2, "Network.getCookies", map[string]any{"urls": []string{FlowURL}}, &cookies); err != nil {
		return details, err
	}
	details.Credentials.Cookies = cookies.Cookies
	if !includeCredits {
		return details, nil
	}
	if !details.Credentials.RPCs.valid() {
		registry, err := discoverRPCs(ctx, conn, details.Credentials.BuildLabel, policy)
		if err != nil {
			return details, err
		}
		details.Credentials.RPCs = registry
	}
	if err := details.Credentials.validate(); err != nil {
		return details, err
	}
	// Use the same request builder as HTTP, with runtime-derived parameters.
	request, _ := json.Marshal(map[string]any{"path": batchPath + "?" + rpcParams(details.Credentials).Encode(), "body": rpcBody(details.Credentials).Encode(), "timeout": policy.RequestTimeoutSeconds * int(time.Second/time.Millisecond), "maxBytes": policy.MaxResponseBytes})
	expression := `(async()=>{const r=` + string(request) + `;const response=await fetch(r.path,{method:'POST',credentials:'include',signal:AbortSignal.timeout(r.timeout),headers:{'Content-Type':'application/x-www-form-urlencoded;charset=UTF-8','X-Same-Domain':'1'},body:r.body});if(!response.ok)return JSON.stringify({status:response.status,retryAfter:response.headers.get('Retry-After')});const reader=response.body.getReader();const chunks=[];let size=0;try{while(true){const {done,value}=await reader.read();if(done)break;size+=value.byteLength;if(size>r.maxBytes){await reader.cancel();throw Error('Response too large');}chunks.push(value);}}finally{reader.releaseLock();}const bytes=new Uint8Array(size);let offset=0;for(const chunk of chunks){bytes.set(chunk,offset);offset+=chunk.length;}return JSON.stringify({status:response.status,text:new TextDecoder().decode(bytes)});})()`
	var rpc struct {
		Status     int    `json:"status"`
		Text       string `json:"text"`
		RetryAfter string `json:"retryAfter"`
	}
	if err := evaluate(ctx, conn, 3, expression, &rpc); err != nil {
		return details, err
	}
	if rpc.Status != http.StatusOK {
		return details, responseError(rpc.Status, rpc.RetryAfter)
	}
	credits, err := ParseCreditsFromText(rpc.Text, details.Credentials.RPCs.Credits)
	if err != nil {
		return details, err
	}
	details.Credits = credits
	details.CreditsVerified = true
	if details.Credentials.RPCs.Availability != "" {
		if availability, parseErr := parseRPCPayload(rpc.Text, details.Credentials.RPCs.Availability); parseErr == nil {
			if code, availErr := parseAvailability(availability); availErr == nil {
				credits.AvailabilityCode = code
			}
		}
	}
	details.Credits = credits
	return details, nil
}

func (c Credentials) validate() error {
	if !c.RPCs.valid() {
		return fmt.Errorf("%w Phiên chưa có đủ dữ liệu runtime; hãy tải lại trang và đăng nhập.", ErrPageNotReady)
	}
	return c.validateSession()
}

// validateSession checks the runtime values every batchexecute call sends.
func (c Credentials) validateSession() error {
	if c.AtToken == "" || c.BuildLabel == "" || c.SessionID == "" || c.Language == "" || c.UserAgent == "" || !strings.HasPrefix(c.SourcePath, "/") {
		return fmt.Errorf("%w Phiên chưa có đủ dữ liệu runtime; hãy tải lại trang và đăng nhập.", ErrPageNotReady)
	}
	return nil
}
