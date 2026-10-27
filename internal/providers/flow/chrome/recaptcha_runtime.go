package chrome

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"SnakeMedia/internal/config"
	"github.com/gorilla/websocket"
)

// ErrNoRuntimeSiteKey means the account's loaded Flow page did not expose its
// reCAPTCHA site key, so the runtime minter could not run.
var ErrNoRuntimeSiteKey = errors.New("Flow chưa cung cấp site key reCAPTCHA runtime. Hãy tải lại project.")

// MintRuntimeRecaptchaToken uses the existing mint block unchanged, substituting
// only its fallback key with a key discovered from this account's loaded page.
// recaptcha.go remains untouched; absent runtime configuration is an error.
func MintRuntimeRecaptchaToken(ctx context.Context, port int, policy config.AccountConfig, action string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(policy.RequestTimeoutSeconds)*time.Second)
	defer cancel()
	var pages []pageEntry
	if err := localJSON(ctx, port, "/json", policy, &pages); err != nil {
		return "", err
	}
	for _, page := range pages {
		if _, ok := page.flowPage(); !ok {
			continue
		}
		if !localWebSocket(page.WebSocketDebuggerURL, port) {
			return "", errors.New("invalid account page endpoint")
		}
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, page.WebSocketDebuggerURL, nil)
		if err != nil {
			return "", err
		}
		defer conn.Close()
		stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
		defer stop()
		var key string
		err = evaluate(ctx, conn, 1, `JSON.stringify((()=>{
 for(const element of document.querySelectorAll('script[src]')) {
   const u=new URL(element.src,location.href);
   if((u.hostname==='www.google.com'||u.hostname==='www.recaptcha.net')&&u.pathname.includes('/recaptcha/')) {
     const key=u.searchParams.get('render');if(key&&key!=='explicit')return key;
   }
 }
 const seen=new WeakSet();let budget=512;
 const visit=(value,depth)=>{
   if(!value||typeof value!=='object'||depth>8||budget--<=0||seen.has(value))return '';
   seen.add(value);
   let descriptors;try{descriptors=Object.getOwnPropertyDescriptors(value)}catch{return ''}
   for(const [name,descriptor] of Object.entries(descriptors)){
     if(!('value' in descriptor))continue;
     const entry=descriptor.value;
     if(name.toLowerCase()==='sitekey'&&typeof entry==='string')return entry;
   }
   for(const descriptor of Object.values(descriptors)){
     if(!('value' in descriptor))continue;
     const found=visit(descriptor.value,depth+1);if(found)return found;
   }
   return '';
 };
 return visit(window.___grecaptcha_cfg,0);
})())`, &key)
		if err != nil {
			return "", err
		}
		if !regexp.MustCompile(`^[A-Za-z0-9_-]{20,}$`).MatchString(key) {
			return "", ErrNoRuntimeSiteKey
		}
		script := strings.ReplaceAll(BuildRecaptchaMintScript(action), FlowRecaptchaSiteKey, key)
		var result mintResult
		if err := evaluate(ctx, conn, 2, script, &result); err != nil {
			return "", err
		}
		if result.Error != "" || result.Token == "" {
			return "", errors.New("Không lấy được token reCAPTCHA từ phiên Flow hiện tại: " + result.Error)
		}
		return result.Token, nil
	}
	return "", ErrPageNotReady
}
