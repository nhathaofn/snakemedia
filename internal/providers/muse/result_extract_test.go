package muse

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/browser"
)

// TestPageResultExtractsResourcesAndMarkdownFallback runs the real embedded
// page.js against live Muse chat.history shapes:
//   - message.assistant carrying the video only in e.resources[] (no delta.presentation)
//   - commentary assistant event followed by final assistant event with resources
//   - both message.assistant resources and delta.presentation for the same media
//   - markdown ![alt](sandbox://...) fallback when resources are absent
//   - filtering out user-role resources, non-media resources, https URIs, and upload embeds
func TestPageResultExtractsResourcesAndMarkdownFallback(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is needed to execute the page script")
	}
	script := `const assert = require('node:assert/strict');
const historyMap = {
  resources_only: {
    chat_events: [
      {
        role: 'user',
        display_text: 'Task: Create a new video.\nprompt:\n068',
        resources: [{ kind: 'image', mime_type: 'image/jpeg', uri: 'sandbox://workspace/uploads/image_1.jpg' }]
      },
      {
        role: 'assistant',
        event_name: 'message.assistant',
        display_text: '068 done.\n\n![068 | Protection](sandbox://workspace/imagine_media/clip-068.mp4)',
        resources: [
          {
            kind: 'video',
            label: 'clip-068.mp4',
            mime_type: 'video/mp4',
            uri: 'sandbox://workspace/imagine_media/clip-068.mp4',
            metadata: JSON.stringify({
              kind: 'video',
              mime_type: 'video/mp4',
              path: 'workspace/imagine_media/clip-068.mp4',
              size_bytes: 10468216
            })
          },
          {
            kind: 'file',
            mime_type: 'text/plain',
            uri: 'sandbox://workspace/notes.txt'
          }
        ]
      }
    ]
  },
  commentary_then_resources: {
    chat_events: [
      { role: 'user', display_text: '043' },
      { role: 'assistant', event_name: 'message.assistant', display_text: '043 — generating your 10-second clip.', resources: [] },
      {
        role: 'assistant',
        event_name: 'message.assistant',
        display_text: '043 ready.',
        resources: [
          {
            kind: 'video',
            mime_type: 'video/mp4',
            uri: 'https://cdn.example/raw/clip-043.mp4',
            metadata: JSON.stringify({ path: 'workspace/imagine_media/clip-043.mp4', size_bytes: 10298135 })
          }
        ]
      }
    ]
  },
  image_resources_and_markdown_fallback: {
    chat_events: [
      {
        role: 'assistant',
        display_text: 'Here is your image.\n![ref](sandbox://workspace/uploads/image_1.jpg)\n![scene](sandbox://workspace/imagine_media/fallback-scene.webp)',
        payload: {
          resources: [
            {
              kind: 'image',
              mime_type: 'image/webp',
              uri: 'sandbox://workspace/imagine_media/res-scene.webp',
              metadata: { size_bytes: 54321 }
            }
          ]
        }
      }
    ]
  },
  both_assistant_and_presentation: {
    chat_events: [
      {
        role: 'assistant',
        event_name: 'message.assistant',
        display_text: '002 — done.',
        resources: [
          {
            kind: 'video',
            mime_type: 'video/mp4',
            uri: 'sandbox://workspace/imagine_media/clip-002.mp4',
            metadata: JSON.stringify({ size_bytes: 20904405 })
          }
        ]
      },
      {
        role: 'assistant',
        event_name: 'delta.presentation',
        display_text: '002',
        payload: {
          data: {
            videos: [{ path: 'sandbox://workspace/imagine_media/clip-002.mp4', mime: 'video/mp4', byte_len: 20904405, missing: false }]
          }
        }
      }
    ]
  }
};
global.window = {
  __hatchEarlyGatewayRuntimeState: {
    activeRuntime: {
      getSnapshot: () => ({
        async sendRequest(method, params) {
          assert.equal(method, 'chat.history');
          return historyMap[params.session_id] || { chat_events: [] };
        }
      })
    }
  }
};
const page = (` + pageSource + `)(1024);
(async () => {
  const r1 = await page.result('resources_only');
  assert.equal(r1.length, 1);
  assert.deepEqual(r1[0].images, []);
  assert.deepEqual(r1[0].videos, [{
    path: 'sandbox://workspace/imagine_media/clip-068.mp4',
    mime: 'video/mp4',
    byteLen: 10468216,
    missing: false
  }]);

  const r2 = await page.result('commentary_then_resources');
  assert.equal(r2.length, 2);
  assert.deepEqual(r2[0].videos, []);
  assert.deepEqual(r2[1].videos, [{
    path: 'sandbox://workspace/imagine_media/clip-043.mp4',
    mime: 'video/mp4',
    byteLen: 10298135,
    missing: false
  }]);

  const r3 = await page.result('image_resources_and_markdown_fallback');
  assert.equal(r3.length, 1);
  assert.deepEqual(r3[0].images, [
    { path: 'sandbox://workspace/imagine_media/res-scene.webp', mime: 'image/webp', byteLen: 54321, missing: false },
    { path: 'sandbox://workspace/imagine_media/fallback-scene.webp', mime: 'image/webp', byteLen: 0, missing: false }
  ]);

  const r4 = await page.result('both_assistant_and_presentation');
  assert.equal(r4.length, 2);
  assert.equal(r4[0].videos.length, 1);
  assert.equal(r4[1].videos.length, 1);
})().catch(e => { console.error(e); process.exitCode = 1; });`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node)
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("page result media extraction failed: %v\n%s", err, out)
	}
}

func TestWantedMediaDeduplicatesAcrossAssistantAndPresentation(t *testing.T) {
	t.Run("image storyboard preserves order and merges byteLen without duplicating", func(t *testing.T) {
		events := []AssistantEvent{
			{
				Text: "frames",
				Images: []ResultMedia{
					{Path: "sandbox://workspace/imagine_media/f1.webp", Mime: "image/webp", ByteLen: 0},
					{Path: "sandbox://workspace/imagine_media/f2.webp", Mime: "image/webp", ByteLen: 200},
				},
			},
			{
				Text: "presentation",
				Images: []ResultMedia{
					{Path: "workspace/imagine_media/f1.webp", Mime: "image/webp", ByteLen: 100},
					{Path: "sandbox://workspace/imagine_media/f2.webp", Mime: "image/webp", ByteLen: 0},
				},
			},
		}
		wanted, missing := wantedMedia(events, MuseJobImage)
		if missing || len(wanted) != 2 {
			t.Fatalf("wanted = %+v, missing = %v, want 2 deduplicated images", wanted, missing)
		}
		if mediaPathKey(wanted[0].Path) != "workspace/imagine_media/f1.webp" || wanted[0].ByteLen != 100 {
			t.Fatalf("wanted[0] = %+v", wanted[0])
		}
		if mediaPathKey(wanted[1].Path) != "workspace/imagine_media/f2.webp" || wanted[1].ByteLen != 200 {
			t.Fatalf("wanted[1] = %+v", wanted[1])
		}
	})

	t.Run("video dedupes same clip and moves re-presented clip last", func(t *testing.T) {
		events := []AssistantEvent{
			{Videos: []ResultMedia{{Path: "sandbox://workspace/a.mp4", Mime: "video/mp4", Missing: true}}},
			{Videos: []ResultMedia{{Path: "sandbox://workspace/a.mp4", Mime: "video/mp4", ByteLen: 500}}},
			{Videos: []ResultMedia{{Path: "sandbox://workspace/b.mp4", Mime: "video/mp4", ByteLen: 600}}},
			{Videos: []ResultMedia{{Path: "sandbox://workspace/a.mp4", Mime: "video/mp4", ByteLen: 500}}},
		}
		wanted, missing := wantedMedia(events, MuseJobVideo)
		if missing || len(wanted) != 2 {
			t.Fatalf("wanted = %+v, missing = %v", wanted, missing)
		}
		if wanted[0].Path != "sandbox://workspace/b.mp4" || wanted[1].Path != "sandbox://workspace/a.mp4" {
			t.Fatalf("order = %+v, want [b.mp4, a.mp4]", wanted)
		}
	})
}

type stagedResultPage struct {
	*fakePage
	mu           sync.Mutex
	resultStages [][]AssistantEvent
	resultCalls  int
}

func (s *stagedResultPage) Result(ctx context.Context, sid string) ([]AssistantEvent, error) {
	s.mu.Lock()
	idx := s.resultCalls
	s.resultCalls++
	var out []AssistantEvent
	if len(s.resultStages) > 0 {
		if idx >= len(s.resultStages) {
			idx = len(s.resultStages) - 1
		}
		out = append([]AssistantEvent(nil), s.resultStages[idx]...)
	}
	s.mu.Unlock()
	if out != nil {
		return out, nil
	}
	return s.fakePage.Result(ctx, sid)
}

// When sessions.list flips to completed slightly before chat.history finishes
// indexing the assistant's video resource, collect re-reads briefly and
// succeeds instead of failing a paid generation as CẦN ĐỔI PROMPT.
func TestCollectSettleRereadsDelayedMediaAfterCompleted(t *testing.T) {
	_, files := videoResult("sandbox://workspace/imagine_media/clip-011.mp4", []byte("video-bytes"))
	page := &stagedResultPage{
		fakePage: &fakePage{
			loggedIn: true,
			sendRes:  SendResult{OK: true, SessionID: "s-011"},
			statuses: []string{"running", "completed"},
			files:    files,
			quota:    &MuseQuota{PercentUsed: 20},
		},
		resultStages: [][]AssistantEvent{
			{{Text: "011 — generating your 10-second clip."}},
			{
				{Text: "011 — generating your 10-second clip."},
				{
					Text:   "Clip 011 | A Fur-Trading Ship Arrives is ready.",
					Videos: []ResultMedia{{Path: "sandbox://workspace/imagine_media/clip-011.mp4", Mime: "video/mp4", ByteLen: 11}},
				},
			},
		},
	}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "011 | A Fur-Trading Ship Arrives")[0]

	p.runOne(context.Background(), job.ID, acc.ID)
	p.wg.Wait()

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobSucceeded || len(got.Files) != 1 {
		t.Fatalf("job = %+v, want succeeded with 1 file", got)
	}
	if len(page.sends) != 1 {
		t.Fatalf("sends = %d, want 1", len(page.sends))
	}
	if page.resultCalls != 2 {
		t.Fatalf("resultCalls = %d, want 2 (1 initial + 1 settle re-read)", page.resultCalls)
	}
}

func TestCheckVideoSettleRereadsDelayedMediaAfterCompleted(t *testing.T) {
	page := &stagedResultPage{
		fakePage: &fakePage{
			loggedIn: true,
			sendRes:  SendResult{OK: true, SessionID: "check-delayed"},
			statuses: []string{"running", "completed"},
			quota:    &MuseQuota{PercentUsed: 15, Tier: "Pro"},
		},
		resultStages: [][]AssistantEvent{
			{{Text: "Generating your video..."}},
			{{Text: "Done", Videos: []ResultMedia{{Path: "sandbox://workspace/cat.mp4", Mime: "video/mp4", ByteLen: 3}}}},
		},
	}
	s, _ := testSite(t, page)

	insp, err := s.CheckVideo(context.Background(), &browser.Browser{}, accounts.Record{}, accounts.Inspection{SignedIn: true})
	if err != nil || !insp.VideoVerified || insp.VideoCheckDisabled {
		t.Fatalf("delayed video probe failed: insp=%+v err=%v", insp, err)
	}
	if page.resultCalls != 2 {
		t.Fatalf("resultCalls = %d, want 2", page.resultCalls)
	}
}
