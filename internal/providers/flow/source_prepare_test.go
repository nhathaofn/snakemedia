package flow

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"SnakeMedia/internal/providers/flow/chrome"
)

func TestUploadedSourceWaitsForSuccessfulProcessingAndServingURL(t *testing.T) {
	for _, failed := range []bool{false, true} {
		name := "ready"
		if failed {
			name = "failed"
		}
		t.Run(name, func(t *testing.T) {
			accounts := testAccountService(t)
			acc := seedFlowAccount(t, accounts, "source@example.invalid", true)
			stored, err := accounts.findAccount(acc.ID)
			if err != nil {
				t.Fatal(err)
			}
			cfg := accounts.cfgManager.Get()
			cfg.Accounts.VideoPollIntervalMS = 1000
			if _, err := accounts.cfgManager.Update(cfg); err != nil {
				t.Fatal(err)
			}
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			calls := 0
			http.DefaultTransport = flowRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				status := 1
				if calls >= 2 {
					status = 3
				}
				if failed {
					status = 4
				}
				state := make([]any, 9)
				state[8] = []any{status}
				descriptor := make([]any, 9)
				// Even a success status needs the materialized serving URL.
				if calls >= 3 {
					descriptor[8] = "https://flow-content.google/video/prepared"
				}
				record := []any{"source", nil, nil, nil, nil, state, nil,
					[]any{descriptor, []any{1280, 720, []any{8, 40000000}}}}
				payload, _ := json.Marshal(record)
				quoted, _ := json.Marshal(string(payload))
				return flowReply(r, RPCGetMedia, string(quoted)), nil
			})
			client, err := chrome.NewFlowClient(stored.Credentials, cfg.Accounts)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			metadata, err := testFlowService(accounts).waitForUploadedSource(ctx, client, "source")
			if failed {
				if err == nil || calls != 1 {
					t.Fatalf("failed source was not rejected immediately: calls=%d err=%v", calls, err)
				}
				return
			}
			if err != nil || calls != 3 || metadata.Width != 1280 || metadata.Height != 720 || metadata.DurationSeconds != 8.04 {
				t.Fatalf("source published before processing completed: calls=%d metadata=%+v err=%v", calls, metadata, err)
			}
		})
	}
}
