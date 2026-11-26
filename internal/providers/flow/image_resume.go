package flow

import (
	"context"
	"slices"
	"strings"

	"SnakeMedia/internal/browser"

	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/models"
	"SnakeMedia/internal/providers/flow/chrome"
)

// Resuming image requests whose outcome is unknown, without sending them again.

func (s *Provider) resumeImageGeneration(ctx context.Context, job generationJob, req GenerateImageRequest) models.Response[[]GeneratedImageItem] {
	pending := pendingImageItems(job, req)
	if job.ProjectID == "" || len(job.ClientIDs) == 0 || job.AccountID == "" {
		return models.SuccessResponse(pending)
	}
	acc, release, err := s.readyOwnedAccount(ctx, job.AccountID)
	if err != nil {
		return models.SuccessResponse(pending)
	}
	defer release()
	limits := s.cfg.Get().FlowPagePolicy()
	if req.Quality == ImageQuality1K && completeAcceptedImages(job) {
		// The acceptance response already identifies every image. Flow's
		// project listing can lag or omit the client IDs after a restart.
		// Signed URLs can expire, so keep project lookup as a fallback.
		if acceptedClient, err := s.flowClient(ctx, *acc); err == nil {
			items := s.materializeGeneratedImages(ctx, acceptedClient, nil, limits, job.ProjectID, req, slices.Clone(job.Images))
			if imageJobPhase(items) == generationComplete {
				for i := range items {
					items[i].JobID, items[i].ModelName = job.ID, job.ImageTemplate.ModelName
					if items[i].ModelName == "" {
						items[i].ModelName = req.ModelID
					}
				}
				job.Phase, job.Images, job.ImageResult, job.Message = generationComplete, items, items, ""
				if err := s.saveGenerationJob(job); err != nil {
					return models.ErrorResponse[[]GeneratedImageItem](err.Error(), models.CodeInternal)
				}
				return models.SuccessResponse(items)
			}
		}
	}
	methods := []string{chrome.MethodProjectContents, chrome.MethodMedia}
	if req.Quality != ImageQuality1K {
		methods = append(methods, chrome.MethodUpsampleImage)
	}
	client, err := s.flowClient(ctx, *acc, methods...)
	if err != nil {
		client, err = s.projectClient(ctx, *acc, job.ProjectID, methods...)
		if err != nil {
			return models.SuccessResponse(pending)
		}
	}
	defer s.keepCredentials(*acc, client)

	var browser *browser.Browser
	if req.Quality != ImageQuality1K {
		browser, err = s.generationBrowser(ctx, *acc, job.ProjectID)
		if err != nil || browser == nil {
			return models.SuccessResponse(pending)
		}
		if completeAcceptedImages(job) {
			items := s.materializeGeneratedImages(ctx, client, browser, limits, job.ProjectID, req, slices.Clone(job.Images))
			for i := range items {
				items[i].JobID, items[i].ModelName = job.ID, job.ImageTemplate.ModelName
				if items[i].ModelName == "" {
					items[i].ModelName = req.ModelID
				}
			}
			phase := imageJobPhase(items)
			job.Phase, job.Images = phase, items
			if phase == generationComplete {
				job.ImageResult, job.Message = items, ""
			} else if idx := slices.IndexFunc(items, func(it GeneratedImageItem) bool { return it.Error != "" }); idx >= 0 {
				job.Message = items[idx].Error
			}
			if err := s.saveGenerationJob(job); err != nil {
				return models.ErrorResponse[[]GeneratedImageItem](err.Error(), models.CodeInternal)
			}
			return models.SuccessResponse(items)
		}
	}
	listing, err := s.projectContentsPayload(ctx, client, job.ProjectID)
	if err != nil {
		return models.SuccessResponse(pending)
	}
	var found []GeneratedImageItem
	foundClientIDs := make(map[string]bool, len(job.ClientIDs))
	listed := false
	workflows := parseWorkflows(listing)
	for _, clientID := range job.ClientIDs {
		match := workflows.forClient(clientID)
		if match.name == "" {
			continue
		}
		// Flow lists the client ID, so it accepted the request: whatever
		// GetMedia says now, the job is never treated as lost.
		listed = true
		payload, err := s.getProjectMedia(ctx, client, match.name)
		if err != nil {
			logger.Get().Warn("cannot read listed generated image", "job", job.ID, "media", match.name, "error", err)
			continue
		}
		items := ParseGeneratedImages(payload, req.Prompt, req.ModelID, req.AspectRatio)
		if len(items) == 0 {
			continue
		}
		item := items[0]
		item.JobID = job.ID
		item.ModelName = job.ImageTemplate.ModelName
		if item.ModelName == "" {
			item.ModelName = req.ModelID
		}
		found = append(found, item)
		foundClientIDs[strings.ToLower(clientID)] = true
	}
	if len(found) == 0 && !listed && s.submissionLost(job) {
		err := s.rejectLostSubmission(job)
		return models.ErrorResponse[[]GeneratedImageItem](err.Error(), models.CodeUnavailable)
	}
	if len(found) == 0 {
		if listed {
			job.Phase = generationAccepted
		}
		job.Message = pending[0].Error
		_ = s.saveGenerationJob(job)
		return models.SuccessResponse(pending)
	}
	found = s.materializeGeneratedImages(ctx, client, browser, limits, job.ProjectID, req, found)
	results := found
	for i, clientID := range job.ClientIDs {
		if !foundClientIDs[strings.ToLower(clientID)] {
			waiting := pendingImageItems(job, req)
			if i < len(waiting) {
				results = append(results, waiting[i])
			}
		}
	}
	if len(found) >= len(job.Images) {
		job.Images = slices.Clone(found)
	}
	if len(foundClientIDs) == len(job.ClientIDs) {
		job.Phase, job.ImageResult, job.Message = imageJobPhase(results), append([]GeneratedImageItem(nil), results...), ""
	} else {
		job.Phase = generationAccepted
	}
	if err := s.saveGenerationJob(job); err != nil {
		return models.ErrorResponse[[]GeneratedImageItem](err.Error(), models.CodeInternal)
	}
	return models.SuccessResponse(results)
}

// completeAcceptedImages identifies an entire accepted batch from an older or
// current journal without relying on the order in which the project lists it.
func completeAcceptedImages(job generationJob) bool {
	if job.Phase != generationAccepted || len(job.ClientIDs) == 0 || len(job.Images) != len(job.ClientIDs) {
		return false
	}
	seen := make(map[string]bool, len(job.Images))
	for _, item := range job.Images {
		if item.Pending || item.ID == "" || item.URL == "" || seen[item.ID] {
			return false
		}
		seen[item.ID] = true
	}
	return true
}

// ResumeImageGeneration reconciles a saved image request without sending a
// second generation mutation to Flow.
func (s *Provider) ResumeImageGeneration(ctx context.Context, id string) models.Response[[]GeneratedImageItem] {
	busy := []GeneratedImageItem{{JobID: id, ID: id, Pending: true, Error: "Job đang được đối soát."}}
	return resumeBillable(s, s.imageBillable(ctx, GenerateImageRequest{}), id, busy)
}

// imageBillable describes image generation to openBillable/resumeBillable.
// A resume rebuilds the request from the journal when req carries no prompt.
func (s *Provider) imageBillable(ctx context.Context, req GenerateImageRequest) billableJob[GeneratedImageItem] {
	return billableJob[GeneratedImageItem]{
		kind:      generationJobImage,
		wrongKind: "Mã này không phải job tạo ảnh.",
		pending:   func(job generationJob) []GeneratedImageItem { return pendingImageItems(job, req) },
		result:    func(job generationJob) []GeneratedImageItem { return job.ImageResult },
		resume: func(job generationJob) models.Response[[]GeneratedImageItem] {
			r := req
			if r.Prompt == "" {
				t := job.ImageTemplate
				r = GenerateImageRequest{RequestID: job.ID, Prompt: t.Prompt, ModelID: t.ModelID, AspectRatio: t.AspectRatio, Quality: t.Quality, Count: len(job.ClientIDs)}
			}
			return s.resumeImageGeneration(ctx, job, r)
		},
	}
}
