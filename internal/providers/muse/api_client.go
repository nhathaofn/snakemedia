package muse

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/events"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/models"
)

// MuseAPIStatus reports the connection health and details of muse2api.
type MuseAPIStatus struct {
	OK        bool   `json:"ok"`
	Driver    string `json:"driver,omitempty"`
	Accounts  int    `json:"accounts,omitempty"`
	Tabs      int    `json:"tabs,omitempty"`
	LatencyMS int64  `json:"latencyMs"`
	Message   string `json:"message"`
}

type readyzPayload struct {
	Ready  bool `json:"ready"`
	Driver struct {
		Driver   string `json:"driver"`
		OK       bool   `json:"ok"`
		Tabs     int    `json:"tabs"`
		BusyTabs int    `json:"busy_tabs"`
	} `json:"driver"`
	Accounts struct {
		Total     int `json:"total"`
		Available int `json:"available"`
	} `json:"accounts"`
}

func parseAPIEndpoint(endpoint string) (base, v1 string) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		endpoint = "http://127.0.0.1:18610/v1"
	}
	endpoint = strings.TrimRight(endpoint, "/")
	base = endpoint
	v1 = endpoint
	if strings.HasSuffix(endpoint, "/v1") {
		base = strings.TrimSuffix(endpoint, "/v1")
	} else {
		v1 = endpoint + "/v1"
	}
	return base, v1
}

func fetchReadyz(ctx context.Context, client *http.Client, base string) (readyzPayload, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/readyz", nil)
	if err != nil {
		return readyzPayload{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return readyzPayload{}, err
	}
	defer resp.Body.Close()
	var payload readyzPayload
	decodeErr := json.NewDecoder(resp.Body).Decode(&payload)
	if resp.StatusCode != http.StatusOK && (decodeErr != nil || (!payload.Ready && payload.Driver.Driver == "" && payload.Accounts.Total == 0)) {
		return readyzPayload{}, fmt.Errorf("readyz HTTP %d", resp.StatusCode)
	}
	if decodeErr != nil {
		return readyzPayload{}, decodeErr
	}
	return payload, nil
}

func fetchAPICapacity(ctx context.Context, endpoint string) (apiCapacity, error) {
	base, _ := parseAPIEndpoint(endpoint)
	client := &http.Client{Timeout: 3 * time.Second}
	readyData, err := fetchReadyz(ctx, client, base)
	if err != nil {
		return apiCapacity{}, err
	}
	return apiCapacity{
		driver:   readyData.Driver.Driver,
		accounts: readyData.Accounts.Available,
		tabs:     readyData.Driver.Tabs,
	}, nil
}

func (p *Provider) recordAPICapacity(endpoint string, cap apiCapacity) {
	now := time.Now()
	if p.now != nil {
		now = p.now()
	}
	p.mu.Lock()
	changed := p.apiEndpoint != endpoint || p.apiCap != cap
	p.apiEndpoint = endpoint
	p.apiCap = cap
	p.apiCheckedAt = now
	p.apiAttemptedAt = now
	ctx := p.ctx
	p.mu.Unlock()
	if changed {
		p.logAPICapacity(cap)
		if ctx != nil && p.wake != nil {
			p.trigger()
		}
	}
}

func (p *Provider) logAPICapacity(cap apiCapacity) {
	if p.cfg == nil {
		return
	}
	logger.Get().Info("muse2api capacity updated",
		"driver", cap.driver, "accounts", cap.accounts, "tabs", cap.tabs,
		"concurrency", apiConcurrency(p.cfg.Get().Muse, cap))
}

func (p *Provider) apiCapacityFor(cfg config.MuseConfig, now time.Time) apiCapacity {
	if !cfg.UseAPI {
		return apiCapacity{}
	}
	endpoint, _ := parseAPIEndpoint(cfg.APIEndpoint)
	p.mu.Lock()
	if p.apiEndpoint != endpoint {
		p.apiEndpoint = endpoint
		p.apiCap = apiCapacity{}
		p.apiCheckedAt = time.Time{}
		p.apiAttemptedAt = time.Time{}
	}
	cap := p.apiCap
	stale := p.apiCheckedAt.IsZero() || now.Sub(p.apiCheckedAt) >= apiCapacityTTL
	cooldown := !p.apiAttemptedAt.IsZero() && now.Sub(p.apiAttemptedAt) < apiCapacityRetryCooldown
	probe := p.probeAPI
	ctx := p.ctx
	if stale && !cooldown && !p.apiChecking && probe != nil && ctx != nil && ctx.Err() == nil {
		p.apiChecking = true
		p.apiAttemptedAt = now
		p.wg.Add(1)
		go p.refreshAPICapacity(ctx, endpoint, probe)
	}
	p.mu.Unlock()
	return cap
}

func (p *Provider) refreshAPICapacity(ctx context.Context, endpoint string, probe func(context.Context, string) (apiCapacity, error)) {
	defer p.wg.Done()
	defer logger.Recover("đọc năng lực muse2api")
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cap, err := probe(reqCtx, endpoint)

	now := time.Now()
	if p.now != nil {
		now = p.now()
	}
	trigger := false
	p.mu.Lock()
	p.apiChecking = false
	if p.apiEndpoint == endpoint && err == nil {
		trigger = cap != p.apiCap
		p.apiCap = cap
		p.apiCheckedAt = now
	}
	p.mu.Unlock()
	if trigger {
		p.logAPICapacity(cap)
		if p.wake != nil {
			p.trigger()
		}
	}
}

func imageMimeForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	default:
		return "image/png"
	}
}

func apiErrorMessage(respData []byte) string {
	var errResp struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Detail string `json:"detail"`
	}
	_ = json.Unmarshal(respData, &errResp)
	if errResp.Error.Message != "" {
		return errResp.Error.Message
	}
	if errResp.Detail != "" {
		return errResp.Detail
	}
	return string(respData)
}

// CheckAPI tests connectivity to the configured muse2api gateway.
func (p *Provider) CheckAPI(ctx context.Context, endpoint, apiKey string) models.Response[MuseAPIStatus] {
	base, v1 := parseAPIEndpoint(endpoint)

	start := time.Now()
	client := &http.Client{Timeout: 5 * time.Second}

	// 1. Check readyz
	readyData, err := fetchReadyz(ctx, client, base)
	if err != nil {
		var urlErr error
		if _, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, base+"/readyz", nil); reqErr != nil {
			urlErr = reqErr
		}
		if urlErr != nil {
			return fail[MuseAPIStatus](urlErr, models.CodeInvalidArgument)
		}
		return models.SuccessResponse(MuseAPIStatus{
			OK:      false,
			Message: fmt.Sprintf("Không thể kết nối tới %s: %v", base, err),
		})
	}

	// 2. Check /v1/models with API Key
	modelsReq, err := http.NewRequestWithContext(ctx, http.MethodGet, v1+"/models", nil)
	if err == nil && apiKey != "" {
		modelsReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if err == nil {
		modelsResp, err := client.Do(modelsReq)
		if err == nil {
			modelsResp.Body.Close()
			if modelsResp.StatusCode == http.StatusUnauthorized || modelsResp.StatusCode == http.StatusForbidden {
				return models.SuccessResponse(MuseAPIStatus{
					OK:        false,
					Driver:    readyData.Driver.Driver,
					LatencyMS: time.Since(start).Milliseconds(),
					Message:   "API Key không chính xác hoặc không có quyền truy cập",
				})
			}
		}
	}

	cap := apiCapacity{
		driver:   readyData.Driver.Driver,
		accounts: readyData.Accounts.Available,
		tabs:     readyData.Driver.Tabs,
	}
	threadsInfo := ""
	if p.cfg != nil {
		cfg := p.cfg.Get().Muse
		cfgBase, _ := parseAPIEndpoint(cfg.APIEndpoint)
		if base == cfgBase {
			p.recordAPICapacity(base, cap)
		}
		threadsInfo = fmt.Sprintf(", Luồng chạy: %d", apiConcurrency(cfg, cap))
	}

	latency := time.Since(start).Milliseconds()
	msg := fmt.Sprintf("Kết nối thành công (%dms). Driver: %s, Tài khoản khả dụng: %d/%d, Tabs: %d%s",
		latency, readyData.Driver.Driver, readyData.Accounts.Available, readyData.Accounts.Total, readyData.Driver.Tabs, threadsInfo)

	return models.SuccessResponse(MuseAPIStatus{
		OK:        true,
		Driver:    readyData.Driver.Driver,
		Accounts:  readyData.Accounts.Available,
		Tabs:      readyData.Driver.Tabs,
		LatencyMS: latency,
		Message:   msg,
	})
}

// executeAPI runs a queued job through muse2api instead of the local browser.
func (p *Provider) executeAPI(ctx context.Context, jobID string) error {
	job, ok := p.jobs.get(jobID)
	if !ok || job.finished() {
		return nil
	}
	cfg := p.cfg.Get().Muse
	_, v1 := parseAPIEndpoint(cfg.APIEndpoint)
	apiKey := cfg.APIKey

	if job.Status == MuseJobQueued {
		started := false
		var err error
		job, err = p.updateJob(jobID, func(j *MuseJob) {
			if j.Status != MuseJobQueued {
				return
			}
			started = true
			resetForQueue(j)
			j.NextCheckAt = ""
			j.Status, j.AccountID, j.Step = MuseJobProcessing, "api", "Gửi yêu cầu tới muse2api"
			j.StartedAt = p.now().UTC().Format(time.RFC3339)
			j.Attempts++
		})
		if err != nil {
			return err
		}
		if !started {
			return nil
		}
	}

	if job.Kind == MuseJobImage {
		return p.executeAPIImage(ctx, job, v1, apiKey, cfg)
	}
	if job.Kind == MuseJobVideo {
		return p.executeAPIVideo(ctx, job, v1, apiKey, cfg)
	}
	return fmt.Errorf("Loại tác vụ không hỗ trợ qua API: %s", job.Kind)
}

func apiHTTPError(prefix string, status int, respData []byte) error {
	msg := apiErrorMessage(respData)
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		return &notSentError{err: fmt.Errorf("muse2api đang bận (%d): %s", status, msg)}
	}
	return fmt.Errorf("%s (%d): %s", prefix, status, msg)
}

func (p *Provider) executeAPIImage(ctx context.Context, job MuseJob, v1, apiKey string, cfg config.MuseConfig) error {
	p.updateJob(job.ID, func(j *MuseJob) { j.Step = "Đang gửi yêu cầu tạo ảnh đến muse2api" })

	reqBody := map[string]any{
		"prompt":          job.Prompt,
		"size":            job.Ratio,
		"response_format": "url",
	}
	if reqBody["size"] == "" {
		reqBody["size"] = "1:1"
	}

	if len(job.References) > 0 {
		var images []string
		for _, ref := range job.References {
			path, err := p.media.Path(ref)
			if err != nil {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			dataURI := fmt.Sprintf("data:%s;base64,%s", imageMimeForPath(path), base64.StdEncoding.EncodeToString(data))
			images = append(images, dataURI)
		}
		if len(images) > 0 {
			reqBody["image"] = images
		}
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	url := v1 + "/images/generations"

	timeout := time.Duration(cfg.ImageTimeoutMinutes) * time.Minute
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	client := &http.Client{Timeout: timeout}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	p.updateJob(job.ID, func(j *MuseJob) { j.Step = "muse2api đang tạo ảnh..." })

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Lỗi kết nối muse2api: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("Lỗi đọc phản hồi từ muse2api: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return apiHTTPError("muse2api trả về lỗi", resp.StatusCode, respData)
	}

	var res struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respData, &res); err != nil {
		return fmt.Errorf("Lỗi parse phản hồi JSON: %w", err)
	}
	if len(res.Data) == 0 || res.Data[0].URL == "" {
		return errors.New("muse2api không trả về link ảnh nào")
	}

	p.updateJob(job.ID, func(j *MuseJob) { j.Step = "Đang tải ảnh về máy..." })

	var files []string
	for k, item := range res.Data {
		imgReq, err := http.NewRequestWithContext(ctx, http.MethodGet, item.URL, nil)
		if err != nil {
			return err
		}
		if apiKey != "" {
			imgReq.Header.Set("Authorization", "Bearer "+apiKey)
		}
		imgResp, err := client.Do(imgReq)
		if err != nil {
			return fmt.Errorf("Lỗi tải ảnh từ %s: %w", item.URL, err)
		}
		imgBytes, readErr := io.ReadAll(imgResp.Body)
		imgResp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("Lỗi đọc dữ liệu ảnh: %w", readErr)
		}
		if imgResp.StatusCode != http.StatusOK {
			return fmt.Errorf("Lỗi tải ảnh từ muse2api (%d): %s", imgResp.StatusCode, apiErrorMessage(imgBytes))
		}

		cType := strings.TrimSpace(imgResp.Header.Get("Content-Type"))
		if cType == "" || strings.HasPrefix(strings.ToLower(cType), "application/octet-stream") {
			detected := http.DetectContentType(imgBytes)
			if strings.HasPrefix(detected, "image/") {
				cType = detected
			} else {
				cType = "image/webp"
			}
		}
		localURL, err := p.media.Put(fmt.Sprintf("muse-%s-%d", job.ID, k+1), cType, ".webp", imgBytes)
		if err != nil {
			return fmt.Errorf("Lỗi lưu ảnh vào media: %w", err)
		}
		files = append(files, localURL)
	}

	return p.finishAPIJob(ctx, job, files, fmt.Sprintf("Đã tạo %d ảnh thành công", len(files)))
}

func (p *Provider) finishAPIJob(ctx context.Context, job MuseJob, files []string, step string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	updated, err := p.jobs.update(job.ID, func(j *MuseJob) {
		if j.Status != MuseJobProcessing {
			return
		}
		j.Files = files
	})
	if err != nil {
		if err == errJobNotFound {
			return nil
		}
		return err
	}
	if updated.ID == "" || updated.Status != MuseJobProcessing {
		return nil
	}

	savedJob, err := p.saveCollected(updated, files)
	if err != nil {
		return err
	}
	if savedJob.ID == "" || savedJob.Status != MuseJobProcessing {
		return nil
	}

	var settled bool
	done, err := p.jobs.update(savedJob.ID, func(j *MuseJob) {
		if j.Status != MuseJobProcessing {
			return
		}
		settled = true
		j.Status = MuseJobSucceeded
		j.Files = files
		j.SavedFiles = savedJob.SavedFiles
		j.Error = ""
		j.Step = step
		j.FinishedAt = p.now().UTC().Format(time.RFC3339)
	})
	if err != nil {
		if err == errJobNotFound {
			return nil
		}
		return err
	}
	if !settled {
		return nil
	}

	p.addToLibrary(done)
	p.emit(events.EventMuseJobUpdated, done)
	if done.BoardFrames > 0 && done.Kind == MuseJobImage {
		p.releaseAfter(done)
	}
	return nil
}

func (p *Provider) executeAPIVideo(ctx context.Context, job MuseJob, v1, apiKey string, cfg config.MuseConfig) error {
	taskID := job.SessionID
	client := &http.Client{Timeout: 30 * time.Second}

	if taskID == "" {
		p.updateJob(job.ID, func(j *MuseJob) { j.Step = "Đang gửi yêu cầu tạo video đến muse2api" })

		duration := job.Duration
		if duration <= 0 {
			duration = 5
		}
		size := job.Ratio
		if size == "" {
			size = "16:9"
		}

		reqBody := map[string]any{
			"prompt":   job.Prompt,
			"model":    "sora-2",
			"seconds":  duration,
			"duration": duration,
			"size":     size,
		}

		refURL := ""
		if len(job.Frames) > 0 {
			refURL = job.Frames[0]
		} else if len(job.References) > 0 {
			refURL = job.References[0]
		}
		if refURL != "" {
			if path, err := p.media.Path(refURL); err == nil {
				if data, err := os.ReadFile(path); err == nil {
					reqBody["image"] = fmt.Sprintf("data:%s;base64,%s", imageMimeForPath(path), base64.StdEncoding.EncodeToString(data))
				}
			}
		}

		jsonBytes, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}

		postURL := v1 + "/videos"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, postURL, bytes.NewReader(jsonBytes))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("Lỗi kết nối muse2api: %w", err)
		}
		respData, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return apiHTTPError("muse2api tạo video lỗi", resp.StatusCode, respData)
		}

		var taskResp struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(respData, &taskResp); err != nil {
			return fmt.Errorf("Lỗi parse task ID: %w", err)
		}
		if taskResp.ID == "" {
			return errors.New("muse2api không trả về task ID")
		}

		taskID = taskResp.ID
		job, _ = p.updateJob(job.ID, func(j *MuseJob) {
			j.SessionID = taskID
			j.Step = "muse2api đang chuẩn bị render video (0%)"
		})
	}

	pollURL := fmt.Sprintf("%s/videos/%s", v1, taskID)
	interval := time.Duration(cfg.PollIntervalMS) * time.Millisecond
	if interval < 2*time.Second {
		interval = 3 * time.Second
	}

	videoLimit := time.Duration(cfg.VideoTimeoutMinutes) * time.Minute
	if videoLimit <= 0 {
		videoLimit = 30 * time.Minute
	}
	pollStart := p.now()

	for {
		if err := p.sleep(ctx, interval); err != nil {
			return err
		}
		if p.now().Sub(pollStart) > videoLimit {
			return &museError{msg: fmt.Sprintf("Quá thời gian render video (%v)", videoLimit)}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
		if err != nil {
			return err
		}
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}

		resp, err := client.Do(req)
		if err != nil {
			logger.Get().Warn("muse2api poll error", "error", err)
			continue
		}
		respData, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			logger.Get().Warn("muse2api poll status != 200", "status", resp.StatusCode)
			continue
		}

		var statusResp struct {
			ID       string `json:"id"`
			Status   string `json:"status"`
			Progress int    `json:"progress"`
			Result   struct {
				URL string `json:"url"`
			} `json:"result"`
			Error any `json:"error"`
		}
		if err := json.Unmarshal(respData, &statusResp); err != nil {
			continue
		}

		if statusResp.Status == "queued" || statusResp.Status == "in_progress" || statusResp.Status == "running" {
			job, _ = p.updateJob(job.ID, func(j *MuseJob) {
				j.Step = fmt.Sprintf("muse2api đang render video (%d%%)", statusResp.Progress)
			})
			continue
		}

		if statusResp.Status == "failed" {
			return &museError{msg: fmt.Sprintf("muse2api render video thất bại: %v", statusResp.Error)}
		}

		if statusResp.Status == "cancelled" || statusResp.Status == "canceled" {
			return &museError{msg: "muse2api đã huỷ render video"}
		}

		if statusResp.Status == "completed" || statusResp.Status == "succeeded" {
			videoURL := statusResp.Result.URL
			if videoURL == "" {
				videoURL = fmt.Sprintf("%s/videos/%s/content", v1, taskID)
			}

			p.updateJob(job.ID, func(j *MuseJob) {
				j.Step = "Đang tải video về máy..."
			})

			dlReq, err := http.NewRequestWithContext(ctx, http.MethodGet, videoURL, nil)
			if err != nil {
				return err
			}
			if apiKey != "" {
				dlReq.Header.Set("Authorization", "Bearer "+apiKey)
			}
			dlClient := &http.Client{Timeout: max(2*time.Minute, videoLimit)}
			dlResp, err := dlClient.Do(dlReq)
			if err != nil {
				return fmt.Errorf("Lỗi tải video từ muse2api: %w", err)
			}
			videoBytes, readErr := io.ReadAll(dlResp.Body)
			dlResp.Body.Close()
			if readErr != nil {
				return fmt.Errorf("Lỗi đọc dữ liệu video: %w", readErr)
			}
			if dlResp.StatusCode != http.StatusOK {
				return fmt.Errorf("Lỗi tải video từ muse2api (%d): %s", dlResp.StatusCode, apiErrorMessage(videoBytes))
			}

			localURL, err := p.media.Put(fmt.Sprintf("muse-%s-1", job.ID), "video/mp4", ".mp4", videoBytes)
			if err != nil {
				return fmt.Errorf("Lỗi lưu video vào media: %w", err)
			}

			return p.finishAPIJob(ctx, job, []string{localURL}, "Đã render video thành công")
		}
	}
}
