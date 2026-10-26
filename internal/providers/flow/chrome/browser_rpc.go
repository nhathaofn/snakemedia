package chrome

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

// DoFromBrowser submits a first-party RPC from the same Chrome session that
// minted the token. Chrome supplies its own cookies, Origin and transport.
// Request/session data stays in Go and this account's validated Flow tab.
func (f *FlowClient) DoFromBrowser(ctx context.Context, port int, rpcID, fReq string, timeout time.Duration) ([]byte, error) {
	if !rpcIDPattern.MatchString(rpcID) {
		return nil, errors.New("invalid runtime RPC ID")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var pages []pageEntry
	if err := localJSON(ctx, port, "/json", f.policy, &pages); err != nil {
		return nil, err
	}
	for _, page := range pages {
		if _, ok := page.flowPage(); !ok {
			continue
		}
		if !localWebSocket(page.WebSocketDebuggerURL, port) {
			return nil, errors.New("invalid account page endpoint")
		}
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, page.WebSocketDebuggerURL, nil)
		if err != nil {
			return nil, err
		}
		defer conn.Close()
		conn.SetReadLimit(max(f.policy.MaxDiscoveryBytes, 2*f.policy.MaxResponseBytes))
		stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
		defer stop()
		request, _ := json.Marshal(map[string]any{
			"path":    batchPath + "?" + batchParams(f.creds, rpcID).Encode(),
			"body":    url.Values{"f.req": {fReq}, "at": {f.creds.AtToken}}.Encode(),
			"timeout": timeout.Milliseconds(), "maxBytes": f.policy.MaxResponseBytes,
		})
		script := `(async()=>{
 if(location.origin!==` + quoteJS(FlowURL) + `)throw Error('Unexpected Flow origin');
 const request=` + string(request) + `;
 const response=await fetch(request.path,{method:'POST',credentials:'include',signal:AbortSignal.timeout(request.timeout),
 headers:{'Content-Type':'application/x-www-form-urlencoded;charset=UTF-8','X-Same-Domain':'1'},body:request.body});
 const reader=response.body.getReader();const chunks=[];let size=0;
 try{while(true){const {done,value}=await reader.read();if(done)break;size+=value.byteLength;
 if(size>request.maxBytes){await reader.cancel();throw Error('Flow response exceeded configured limit');}chunks.push(value);}}
 finally{reader.releaseLock();}
 const bytes=new Uint8Array(size);let offset=0;
 for(const chunk of chunks){bytes.set(chunk,offset);offset+=chunk.length;}
 return JSON.stringify({status:response.status,text:new TextDecoder().decode(bytes),retryAfter:response.headers.get('Retry-After')||''});
})()`
		var reply struct {
			Status     int    `json:"status"`
			Text       string `json:"text"`
			RetryAfter string `json:"retryAfter"`
		}
		if err := evaluate(ctx, conn, 1, script, &reply); err != nil {
			return nil, err
		}
		var cookies struct {
			Cookies []CookieItem `json:"cookies"`
		}
		if err := cdpCall(ctx, conn, 2, "Network.getCookies", map[string]any{"urls": []string{FlowURL}}, &cookies); err == nil {
			if jar, err := flowCookieJar(f.origin, cookies.Cookies); err == nil {
				f.creds.Cookies = cookies.Cookies
				f.client.Jar = jar
			}
		}
		body := []byte(reply.Text)
		if reply.Status != http.StatusOK {
			return body, responseError(reply.Status, reply.RetryAfter)
		}
		return body, nil
	}
	return nil, ErrPageNotReady
}

func quoteJS(value string) string { encoded, _ := json.Marshal(value); return string(encoded) }
