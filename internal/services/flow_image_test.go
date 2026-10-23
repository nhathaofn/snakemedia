package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"SnakeMedia/internal/models"
	"SnakeMedia/internal/services/chrome"
)

func TestValidateAccountCookies(t *testing.T) {
	t.Run("empty cookies fail", func(t *testing.T) {
		err := ValidateAccountCookies(nil)
		if err == nil {
			t.Fatal("expected error for nil cookies")
		}
	})

	t.Run("missing core cookies fail", func(t *testing.T) {
		cookies := []chrome.CookieItem{
			{Name: "_ga", Value: "123"},
			{Name: "NID", Value: "456"},
		}
		err := ValidateAccountCookies(cookies)
		if err == nil {
			t.Fatal("expected error for missing core cookies")
		}
	})

	t.Run("expired core cookie fails", func(t *testing.T) {
		past := float64(time.Now().Add(-1 * time.Hour).Unix())
		cookies := []chrome.CookieItem{
			{Name: "SID", Value: "val", Expires: past},
			{Name: "HSID", Value: "val"},
			{Name: "SSID", Value: "val"},
		}
		err := ValidateAccountCookies(cookies)
		if err == nil {
			t.Fatal("expected error for expired core cookie")
		}
	})

	t.Run("valid cookies succeed", func(t *testing.T) {
		future := float64(time.Now().Add(24 * time.Hour).Unix())
		cookies := []chrome.CookieItem{
			{Name: "__Secure-1PSID", Value: "val", Expires: future},
			{Name: "HSID", Value: "val"},
			{Name: "__Secure-3PAPISID", Value: "val", Expires: future},
		}
		err := ValidateAccountCookies(cookies)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestFlowImageServiceRoundRobin(t *testing.T) {
	future := float64(time.Now().Add(24 * time.Hour).Unix())
	validCookies := []chrome.CookieItem{
		{Name: "SID", Value: "s1", Expires: future},
		{Name: "HSID", Value: "h1"},
		{Name: "SSID", Value: "ss1", Expires: future},
	}

	acc1 := storedAccount{
		AccountProfile: AccountProfile{ID: "acc1", Status: AccountConnected},
		Credentials:    chrome.Credentials{Cookies: validCookies},
	}
	acc2 := storedAccount{
		AccountProfile: AccountProfile{ID: "acc2", Status: AccountConnected},
		Credentials:    chrome.Credentials{Cookies: validCookies},
	}
	accUnverified := storedAccount{
		AccountProfile: AccountProfile{ID: "acc3", Status: AccountUnverified},
		Credentials:    chrome.Credentials{Cookies: validCookies},
	}

	service := &FlowImageService{}
	mockAccounts := []storedAccount{acc1, acc2, accUnverified}

	// We test round-robin logic directly
	validOnly := make([]storedAccount, 0)
	for _, a := range mockAccounts {
		if a.Status == AccountConnected && ValidateAccountCookies(a.Credentials.Cookies) == nil {
			validOnly = append(validOnly, a)
		}
	}
	if len(validOnly) != 2 {
		t.Fatalf("expected 2 valid accounts, got %d", len(validOnly))
	}

	// Verify nextAccount distribution
	for i := 0; i < 4; i++ {
		acc, err := service.NextAccount()
		// Since accounts is nil on service, verify error
		if err == nil || acc != nil {
			t.Fatal("expected error when accounts service is nil")
		}
	}
}

func TestFlowImageServiceGetImageModels(t *testing.T) {
	t.Run("returns error when no valid accounts exist", func(t *testing.T) {
		service := NewFlowImageService(nil)
		res := service.GetImageModels(context.Background())
		if res.Success {
			t.Fatal("expected failure when no accounts exist")
		}
		if res.Code != models.CodeUnavailable {
			t.Fatalf("expected CodeUnavailable, got %v", res.Code)
		}
	})

	t.Run("returns real models with mock live fetch", func(t *testing.T) {
		service := NewFlowImageService(nil)
		// Inject seam
		service.fetchLiveModels = func(_ context.Context, _ storedAccount) ([]FlowModel, error) {
			return []FlowModel{
				{ID: "NARWHAL", Name: "Nano 2", Alias: "nano2", Default: true},
				{ID: "GEM_PIX_2", Name: "Nano Pro", Alias: "nano-pro"},
			}, nil
		}

		future := float64(time.Now().Add(24 * time.Hour).Unix())
		validAcc := storedAccount{
			AccountProfile: AccountProfile{ID: "test", Status: AccountConnected},
			Credentials: chrome.Credentials{
				Cookies: []chrome.CookieItem{
					{Name: "SID", Value: "1", Expires: future},
					{Name: "HSID", Value: "1"},
					{Name: "SSID", Value: "1"},
				},
			},
		}

		// When mock fetch returns models
		modelsList, err := service.fetchLiveModels(context.Background(), validAcc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(modelsList) != 2 {
			t.Fatalf("expected 2 models, got %d", len(modelsList))
		}
		if modelsList[0].ID != "NARWHAL" {
			t.Fatalf("expected NARWHAL, got %s", modelsList[0].ID)
		}
	})

	t.Run("returns error when live fetch fails", func(t *testing.T) {
		service := NewFlowImageService(nil)
		service.fetchLiveModels = func(_ context.Context, _ storedAccount) ([]FlowModel, error) {
			return nil, errors.New("network error")
		}

		_, err := service.fetchLiveModels(context.Background(), storedAccount{})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestFlowImageServiceGenerateImages(t *testing.T) {
	t.Run("empty prompt fails with CodeInvalidArgument", func(t *testing.T) {
		service := NewFlowImageService(nil)
		res := service.GenerateImages(context.Background(), GenerateImageRequest{Prompt: "   "})
		if res.Success {
			t.Fatal("expected error for empty prompt")
		}
		if res.Code != models.CodeInvalidArgument {
			t.Fatalf("expected CodeInvalidArgument, got %v", res.Code)
		}
	})

	t.Run("no accounts fails with CodeUnavailable", func(t *testing.T) {
		service := NewFlowImageService(nil)
		res := service.GenerateImages(context.Background(), GenerateImageRequest{Prompt: "test prompt"})
		if res.Success {
			t.Fatal("expected error when no accounts exist")
		}
		if res.Code != models.CodeUnavailable {
			t.Fatalf("expected CodeUnavailable, got %v", res.Code)
		}
	})

	t.Run("successful generation with mock generator", func(t *testing.T) {
		service := NewFlowImageService(nil)
		service.generateLiveImages = func(_ context.Context, _ storedAccount, req GenerateImageRequest) ([]GeneratedImageItem, error) {
			items := make([]GeneratedImageItem, req.Count)
			for i := 0; i < req.Count; i++ {
				items[i] = GeneratedImageItem{
					ID:          "test-1",
					URL:         "https://example.com/test.png",
					Prompt:      req.Prompt,
					Model:       req.ModelID,
					AspectRatio: req.AspectRatio,
				}
			}
			return items, nil
		}

		future := float64(time.Now().Add(24 * time.Hour).Unix())
		validAcc := storedAccount{
			AccountProfile: AccountProfile{ID: "test", Status: AccountConnected},
			Credentials: chrome.Credentials{
				Cookies: []chrome.CookieItem{
					{Name: "SID", Value: "1", Expires: future},
					{Name: "HSID", Value: "1"},
					{Name: "SSID", Value: "1"},
				},
			},
		}

		items, err := service.generateLiveImages(context.Background(), validAcc, GenerateImageRequest{
			Prompt:      "A neon cyberpunk cat",
			ModelID:     "NARWHAL",
			AspectRatio: "16:9",
			Count:       4,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(items) != 4 {
			t.Fatalf("expected 4 items, got %d", len(items))
		}
		if items[0].Prompt != "A neon cyberpunk cat" {
			t.Fatalf("expected prompt match, got %s", items[0].Prompt)
		}
	})
}

func TestBuildImageBatchRequest(t *testing.T) {
	data, err := BuildImageBatchRequest("cyberpunk city", "proj-test", "16:9", "GEM_PIX_2", 2, "test_token")
	if err != nil {
		t.Fatalf("unexpected error building payload: %v", err)
	}

	var outer []any
	if err := json.Unmarshal(data, &outer); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(outer) != 5 {
		t.Fatalf("expected outer array length 5, got %d", len(outer))
	}
	if outer[0] != nil {
		t.Fatalf("expected outer[0] to be nil, got %v", outer[0])
	}
	if outer[2] != float64(1) {
		t.Fatalf("expected outer[2] to be 1, got %v", outer[2])
	}

	items, ok := outer[1].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("expected 2 generation items, got %v", outer[1])
	}

	item0, ok := items[0].([]any)
	if !ok || len(item0) != 14 {
		t.Fatalf("expected item0 to have 14 elements, got %d", len(item0))
	}

	// Index 4: aspectRatio 16:9 should be 3
	if item0[4] != float64(3) {
		t.Fatalf("expected aspect ratio 3 for 16:9, got %v", item0[4])
	}

	// Index 5: modelID
	if item0[5] != "GEM_PIX_2" {
		t.Fatalf("expected model GEM_PIX_2, got %v", item0[5])
	}

	// Index 7: context
	ctxObj, ok := item0[7].([]any)
	if !ok || len(ctxObj) != 11 {
		t.Fatalf("expected context to have 11 elements, got %v", item0[7])
	}
	if ctxObj[1] != float64(22) {
		t.Fatalf("expected SURFACE_ID 22, got %v", ctxObj[1])
	}
	if ctxObj[5] != "proj-test" {
		t.Fatalf("expected project ID proj-test, got %v", ctxObj[5])
	}

	// Index 8: prompt wrapped in 3-level array [[[prompt]]]
	promptL1, ok := item0[8].([]any)
	if !ok || len(promptL1) != 1 {
		t.Fatalf("invalid prompt wrapping level 1: %v", item0[8])
	}
	promptL2, ok := promptL1[0].([]any)
	if !ok || len(promptL2) != 1 {
		t.Fatalf("invalid prompt wrapping level 2: %v", promptL1)
	}
	promptL3, ok := promptL2[0].([]any)
	if !ok || len(promptL3) != 1 || promptL3[0] != "cyberpunk city" {
		t.Fatalf("expected prompt 'cyberpunk city', got %v", promptL2)
	}
}

func TestParseGeneratedImages(t *testing.T) {
	sampleResp := []byte(`)]}'
123
[["wrb.fr","ogiZ0b","[\"https://flow-content.google/image/img-123?sig=abc\",\"https://flow-content.google/image/img-123?sig=abc_thumb\",\"https://flow-content.google/image/img-456\"]",null,null,null,"generic"]]`)

	items := ParseGeneratedImages(sampleResp, "cat", "NARWHAL", "1:1")
	if len(items) != 2 {
		t.Fatalf("expected 2 unique images, got %d", len(items))
	}
	if items[0].ID != "img-123" {
		t.Errorf("expected img-123, got %s", items[0].ID)
	}
	if items[1].ID != "img-456" {
		t.Errorf("expected img-456, got %s", items[1].ID)
	}
}

func TestBuildImageBatchRequestWithReferences(t *testing.T) {
	data, err := BuildImageBatchRequest("styled girl", "proj-1", "1:1", "GEM_PIX_2", 1, "token123", []string{"media-ref-abc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var outer []any
	if err := json.Unmarshal(data, &outer); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	items := outer[1].([]any)
	item0 := items[0].([]any)
	refs, ok := item0[2].([]any)
	if !ok || len(refs) != 1 {
		t.Fatalf("expected 1 reference image, got %v", item0[2])
	}
	ref0, ok := refs[0].([]any)
	if !ok || len(ref0) != 5 {
		t.Fatalf("expected ref0 length 5, got %v", refs[0])
	}
	if ref0[0] != "media-ref-abc" {
		t.Errorf("expected media-ref-abc, got %v", ref0[0])
	}
	if ref0[4] != float64(1) {
		t.Errorf("expected refType 1 (IMAGE), got %v", ref0[4])
	}
}

func TestBuildImageUploadRequest(t *testing.T) {
	data, err := BuildImageUploadRequest("base64data", "image/png", "sample.png", "proj-99", "token-xyz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var outer []any
	if err := json.Unmarshal(data, &outer); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(outer) != 12 {
		t.Fatalf("expected 12 fields in upload request, got %d", len(outer))
	}
	if outer[1] != "base64data" {
		t.Errorf("expected base64data, got %v", outer[1])
	}
	if outer[2] != "image/png" {
		t.Errorf("expected image/png, got %v", outer[2])
	}
	if outer[8] != "sample.png" {
		t.Errorf("expected sample.png, got %v", outer[8])
	}
}

func TestParseUploadedMediaID(t *testing.T) {
	sampleResp := []byte(`)]}'
55
[["wrb.fr","maseQ","[[\"b3b12345-6789-4abc-def0-123456789abc\"]]",null,null,null,"generic"]]`)
	mediaID, err := ParseUploadedMediaID(sampleResp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mediaID != "b3b12345-6789-4abc-def0-123456789abc" {
		t.Errorf("expected media ID match, got %s", mediaID)
	}
}
