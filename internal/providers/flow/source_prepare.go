package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"SnakeMedia/internal/media"
	"SnakeMedia/internal/providers/flow/chrome"
)

// A successful Scotty upload only starts Flow's transcoding/safety pipeline.
// Editing must wait for status 3 AND a serving URL, not just an upload media ID.
func (s *Provider) waitForUploadedSource(ctx context.Context, client *chrome.FlowClient, id string) (media.VideoMetadata, error) {
	policy := s.cfg.Get().Accounts
	rpc := client.Credentials().RPCs.ID(chrome.MethodMedia)
	fReq, err := BuildBatchEnvelope(rpc, []any{id})
	if err != nil {
		return media.VideoMetadata{}, err
	}
	failures := 0
	delay := time.Duration(policy.VideoPollIntervalMS) * time.Millisecond
	for {
		body, callErr := client.Do(ctx, rpc, fReq, time.Duration(policy.RequestTimeoutSeconds)*time.Second)
		if callErr == nil {
			var payload string
			payload, callErr = chrome.ParseRPCPayload(string(body), rpc)
			if callErr == nil {
				rows, parseErr := parseVideoMedia("[["+payload+"]]", 0)
				if parseErr != nil {
					return media.VideoMetadata{}, parseErr
				}
				if len(rows) != 1 || rows[0].name != id {
					return media.VideoMetadata{}, errors.New("Flow trả về thông tin nguồn không khớp.")
				}
				video := rows[0]
				if videoFailed(video) {
					return media.VideoMetadata{}, fmt.Errorf("Flow xử lý video nguồn thất bại: %s %s", videoStatusText(video.status), video.errMsg)
				}
				if video.statusObserved && video.status == videoStatusSuccess && video.url != "" {
					var record []any
					if err := json.Unmarshal([]byte(payload), &record); err != nil {
						return media.VideoMetadata{}, err
					}
					return preparedVideoMetadata(record)
				}
			}
		}
		if callErr != nil {
			failures++
			if failures >= s.cfg.Get().Flow.MaxPollFailures {
				return media.VideoMetadata{}, fmt.Errorf("Không theo dõi được xử lý nguồn: %w", callErr)
			}
			delay = min(delay*2, time.Duration(s.cfg.Get().Flow.PollMaxIntervalMS)*time.Millisecond)
			var remote *chrome.HTTPError
			if errors.As(callErr, &remote) {
				delay = max(delay, remote.RetryAfter)
			}
		} else {
			failures = 0
			delay = time.Duration(policy.VideoPollIntervalMS) * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return media.VideoMetadata{}, ctx.Err()
		case <-time.After(delay):
		}
	}
}

// Flow's normalized video metadata: media field 8, metadata field 2,
// [width, height, [duration seconds, duration nanoseconds]].
func preparedVideoMetadata(record []any) (media.VideoMetadata, error) {
	if len(record) <= 7 {
		return media.VideoMetadata{}, errors.New("Thiếu video đã xử lý từ Flow.")
	}
	video := asList(record[7])
	if len(video) <= 1 {
		return media.VideoMetadata{}, errors.New("Thiếu thông số video đã xử lý.")
	}
	meta := asList(video[1])
	if len(meta) <= 2 {
		return media.VideoMetadata{}, errors.New("Thông số video đã xử lý không hợp lệ.")
	}
	w, _ := meta[0].(float64)
	h, _ := meta[1].(float64)
	stamp := asList(meta[2])
	var seconds float64
	if len(stamp) > 0 {
		seconds, _ = stamp[0].(float64)
	}
	if len(stamp) > 1 {
		nanos, _ := stamp[1].(float64)
		seconds += nanos / float64(time.Second)
	}
	if w <= 0 || h <= 0 || seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return media.VideoMetadata{}, errors.New("Flow chưa cung cấp thời lượng hoặc kích thước nguồn hợp lệ.")
	}
	return media.VideoMetadata{Width: int(w), Height: int(h), DurationSeconds: seconds}, nil
}
