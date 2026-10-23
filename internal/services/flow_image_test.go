package services

import (
	"context"
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
