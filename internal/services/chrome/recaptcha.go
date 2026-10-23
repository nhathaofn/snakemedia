package chrome

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"SnakeMedia/internal/config"
	"github.com/gorilla/websocket"
)

const (
	// FlowRecaptchaSiteKey is Google Flow's reCAPTCHA Enterprise site key.
	FlowRecaptchaSiteKey = "6LdsFiUsAAAAAIjVDZcuLhaHiDn5nnHVXVRQGeMV"
	// ActionImageGeneration is the action name for image generation token minting.
	ActionImageGeneration = "IMAGE_GENERATION"
	// ActionVideoGeneration is the action name for video generation token minting.
	ActionVideoGeneration = "VIDEO_GENERATION"
)

type mintResult struct {
	Token       string `json:"token"`
	Intercepted bool   `json:"intercepted"`
	Error       string `json:"error"`
}

// BuildRecaptchaMintScript generates the JavaScript snippet that safely evaluates within
// flow.google.com to mint clean reCAPTCHA Enterprise tokens without triggering anti-tamper traps.
func BuildRecaptchaMintScript(action string) string {
	return fmt.Sprintf(`(() => {
  return new Promise(async (resolve) => {
    function resolveSitekey() {
      try {
        const cfg = window.___grecaptcha_cfg || {};
        const clients = cfg.clients || {};
        for (const k of Object.keys(clients)) {
          const c = clients[k];
          if (c && c.sitekey) return c.sitekey;
        }
      } catch (e) {}
      return '%s';
    }

    function waitGrecaptcha(timeout) {
      return new Promise((res, rej) => {
        const start = Date.now();
        const check = () => {
          if (window.grecaptcha?.enterprise?.execute) return res();
          if (Date.now() - start > timeout) return rej(new Error('window.grecaptcha.enterprise.execute not available'));
          setTimeout(check, 100);
        };
        check();
      });
    }

    function waitReady(timeout) {
      return new Promise((res) => {
        let done = false;
        const fin = () => { if (!done) { done = true; res(); } };
        try { window.grecaptcha?.enterprise?.ready?.(fin); } catch (e) {}
        setTimeout(fin, timeout);
      });
    }

    try {
      await waitGrecaptcha(10000);
      const sitekey = resolveSitekey();
      const realObjectAssign = Object.assign;
      let intercepted = false;

      Object.assign = function (target, ...sources) {
        const result = realObjectAssign.call(this, target, ...sources);
        if (result && typeof result === 'object' && result.action === 'extension_hijack_detected') {
          intercepted = true;
          result.action = '%s';
        }
        return result;
      };

      try {
        await waitReady(2000);
        const tokenPromise = window.grecaptcha.enterprise.execute(sitekey, { action: '%s' });
        const timeoutPromise = new Promise((_, rej) => setTimeout(() => rej(new Error('reCAPTCHA enterprise execute timed out')), 15000));
        const token = await Promise.race([tokenPromise, timeoutPromise]);
        if (!token) return resolve(JSON.stringify({ error: 'empty token' }));
        resolve(JSON.stringify({ token: String(token), intercepted }));
      } finally {
        Object.assign = realObjectAssign;
      }
    } catch (err) {
      resolve(JSON.stringify({ error: String(err) }));
    }
  });
})()`, FlowRecaptchaSiteKey, action, action)
}

// MintRecaptchaToken connects via CDP to the active flow.google.com tab and mints a clean token.
func MintRecaptchaToken(ctx context.Context, port int, policy config.AccountConfig, action string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(policy.RequestTimeoutSeconds)*time.Second)
	defer cancel()

	var pages []pageEntry
	if err := localJSON(ctx, port, "/json", policy, &pages); err != nil {
		return "", fmt.Errorf("không kết nối được tới Chrome CDP tại cổng %d: %w", port, err)
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
		return "", ErrPageNotReady
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, target, nil)
	if err != nil {
		return "", fmt.Errorf("không mở được kết nối WebSocket CDP: %w", err)
	}
	defer conn.Close()

	script := BuildRecaptchaMintScript(action)
	var res mintResult
	if err := evaluate(ctx, conn, 9999, script, &res); err != nil {
		return "", fmt.Errorf("lỗi thực thi mint script trên tab Flow: %w", err)
	}

	if res.Error != "" {
		return "", fmt.Errorf("trình duyệt không sinh được token (%s)", res.Error)
	}
	if res.Token == "" {
		return "", errors.New("token reCAPTCHA trả về rỗng")
	}

	return res.Token, nil
}
