package flow

import (
	"SnakeMedia/internal/models"
	"SnakeMedia/internal/providers/flow/chrome"
	"SnakeMedia/internal/providers/flow/session"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ImageProjectMode separates the daily project used by Single and All image generation.
type ImageProjectMode string

const (
	ImageProjectModeSingle ImageProjectMode = "single"
	ImageProjectModeAll    ImageProjectMode = "all"
)

var AllImageProjectModes = []struct {
	Value  ImageProjectMode
	TSName string
}{
	{ImageProjectModeSingle, "Single"},
	{ImageProjectModeAll, "All"},
}

type dailyImageProjectKey struct {
	AccountID string
	Day       string
	Mode      ImageProjectMode
}

// MaxGeneratedMediaPerImageProject is the per-project cap before the next
// request moves to a fresh project.
const MaxGeneratedMediaPerImageProject = 100

// FlowImageProject identifies a Flow project and the account that owns it.
type FlowImageProject struct {
	AccountID    string           `json:"accountId"`
	AccountEmail string           `json:"accountEmail"`
	ProjectID    string           `json:"projectId"`
	Mode         ImageProjectMode `json:"mode"`
	Title        string           `json:"title"`
}

// FlowProjectImage is an image in a Flow project's uploaded/generated media.
type FlowProjectImage struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PreviewURL string `json:"previewUrl"`
}

// FlowProjectImageReference contains the bytes needed to use a Flow image as
// a generation reference in SnakeMedia.
type FlowProjectImageReference struct {
	Base64   string `json:"base64"`
	MimeType string `json:"mimeType"`
	FileName string `json:"fileName"`
}

func normalizeImageProjectMode(mode ImageProjectMode) (ImageProjectMode, error) {
	if mode == "" {
		return ImageProjectModeSingle, nil
	}
	for _, option := range AllImageProjectModes {
		if mode == option.Value {
			return mode, nil
		}
	}
	return "", fmt.Errorf("chế độ project ảnh %q không hợp lệ", mode)
}

// ListImageProjects lists recent projects for one mode across connected accounts.
// Opening a mode's library creates only that mode's project for today.
func (s *Provider) ListImageProjects(ctx context.Context, requestedMode ImageProjectMode) models.Response[[]FlowImageProject] {
	mode, err := normalizeImageProjectMode(requestedMode)
	if err != nil {
		return models.ErrorResponse[[]FlowImageProject](err.Error(), models.CodeInvalidArgument)
	}
	accounts, err := s.GetValidConnectedAccounts()
	if err != nil {
		return models.ErrorResponse[[]FlowImageProject](err.Error(), models.CodeUnavailable)
	}
	if len(accounts) == 0 {
		return models.ErrorResponse[[]FlowImageProject]("Không có tài khoản Google Flow nào đang kết nối.", models.CodeUnavailable)
	}

	projects := make([]FlowImageProject, 0)
	now := time.Now().In(time.Local)
	for _, account := range accounts {
		client, release, err := s.imageLibraryClient(ctx, account)
		if err != nil {
			return models.ErrorResponse[[]FlowImageProject](fmt.Sprintf("Không đọc được project của %s: %v", account.Email, err), models.CodeUnavailable)
		}
		accountProjects, fetchErr := s.FetchAccountProjects(ctx, client)
		if fetchErr == nil {
			if accountProjects == nil {
				accountProjects = []flowProject{}
			}
			_, accountProjects, fetchErr = s.ensureDailyImageProject(ctx, client, account, mode, accountProjects)
		}
		release()
		if fetchErr != nil {
			return models.ErrorResponse[[]FlowImageProject](fmt.Sprintf("Không đọc được project của %s: %v", account.Email, fetchErr), models.CodeUnavailable)
		}
		for _, project := range accountProjects {
			projectMode, createdAt, ok := s.parseImageProjectTitle(project.Title, now)
			if !ok || projectMode != mode || !isRecentImageProject(createdAt, now) {
				continue
			}
			projects = append(projects, FlowImageProject{
				AccountID: account.ID, AccountEmail: account.Email,
				ProjectID: project.ID, Mode: projectMode, Title: project.Title,
			})
		}
	}
	return models.SuccessResponse(projects)
}

// ListProjectImages lists image media for one project after confirming that
// the project belongs to the selected connected account.
func (s *Provider) ListProjectImages(ctx context.Context, accountID, projectID string, mode ImageProjectMode) models.Response[[]FlowProjectImage] {
	client, release, err := s.projectImageClient(ctx, accountID, projectID, mode)
	if err != nil {
		return models.ErrorResponse[[]FlowProjectImage](err.Error(), models.CodeUnavailable)
	}
	defer release()

	mediaEntries, err := s.projectMediaEntries(ctx, client, projectID)
	if err != nil {
		return models.ErrorResponse[[]FlowProjectImage]("Không đọc được tệp trong project Flow: "+err.Error(), models.CodeUnavailable)
	}
	images := make([]FlowProjectImage, 0, len(mediaEntries))
	seen := make(map[string]bool)
	for _, entry := range mediaEntries {
		payload, err := s.getProjectMedia(ctx, client, entry.name)
		if err != nil {
			return models.ErrorResponse[[]FlowProjectImage]("Không đọc được tệp trong project Flow: "+err.Error(), models.CodeUnavailable)
		}
		if flowProjectContainsVideo(payload) {
			continue
		}
		for _, imageURL := range flowProjectImageURLs(payload) {
			if seen[imageURL] {
				continue
			}
			seen[imageURL] = true
			name := flowProjectMediaName(entry.name, payload)
			images = append(images, FlowProjectImage{ID: entry.name, Name: name, PreviewURL: imageURL})
			break
		}
	}
	return models.SuccessResponse(images)
}

// GetProjectImageReference downloads one project image and returns its bytes
// only after verifying that the media belongs to the requested project.
func (s *Provider) GetProjectImageReference(ctx context.Context, accountID, projectID string, mode ImageProjectMode, mediaID string) models.Response[FlowProjectImageReference] {
	client, release, err := s.projectImageClient(ctx, accountID, projectID, mode)
	if err != nil {
		return models.ErrorResponse[FlowProjectImageReference](err.Error(), models.CodeUnavailable)
	}
	defer release()

	entries, err := s.projectMediaEntries(ctx, client, projectID)
	if err != nil {
		return models.ErrorResponse[FlowProjectImageReference]("Không đọc được tệp trong project Flow: "+err.Error(), models.CodeUnavailable)
	}
	var selected *projectMediaEntry
	for i := range entries {
		if entries[i].name == mediaID {
			selected = &entries[i]
			break
		}
	}
	if selected == nil {
		return models.ErrorResponse[FlowProjectImageReference]("Ảnh không thuộc project Flow đã chọn.", models.CodeInvalidArgument)
	}

	payload, err := s.getProjectMedia(ctx, client, selected.name)
	if err != nil {
		return models.ErrorResponse[FlowProjectImageReference]("Không đọc được ảnh từ Flow: "+err.Error(), models.CodeUnavailable)
	}
	imageURLs := flowProjectImageURLs(payload)
	for _, imageURL := range imageURLs {
		limits := s.cfg.Get().Accounts
		data, mimeType, err := client.FetchMedia(ctx, imageURL, []string{"image/"}, MaxReferenceImageBytes, time.Duration(limits.RequestTimeoutSeconds)*time.Second)
		if err != nil {
			return models.ErrorResponse[FlowProjectImageReference]("Không tải được ảnh từ Flow: "+err.Error(), models.CodeUnavailable)
		}
		mimeType = strings.TrimSpace(strings.Split(mimeType, ";")[0])
		if !containsString(ReferenceMimeTypes, mimeType) || detectedImageType(data) == "" {
			return models.ErrorResponse[FlowProjectImageReference]("Định dạng ảnh trong project Flow không được hỗ trợ.", models.CodeInvalidArgument)
		}
		return models.SuccessResponse(FlowProjectImageReference{
			Base64: base64.StdEncoding.EncodeToString(data), MimeType: mimeType,
			FileName: flowProjectMediaName(selected.name, payload),
		})
	}
	return models.ErrorResponse[FlowProjectImageReference]("Tệp trong project Flow không phải ảnh tham chiếu hợp lệ.", models.CodeInvalidArgument)
}

func (s *Provider) projectImageClient(ctx context.Context, accountID, projectID string, requestedMode ImageProjectMode) (*chrome.FlowClient, func(), error) {
	mode, err := normalizeImageProjectMode(requestedMode)
	if err != nil {
		return nil, nil, err
	}
	if _, err := uuid.Parse(projectID); err != nil {
		return nil, nil, errors.New("Project ID Flow không hợp lệ.")
	}
	accounts, err := s.GetValidConnectedAccounts()
	if err != nil {
		return nil, nil, err
	}
	var account *session.StoredAccount
	for i := range accounts {
		if accounts[i].ID == accountID {
			account = &accounts[i]
			break
		}
	}
	if account == nil {
		return nil, nil, errors.New("Tài khoản Flow không còn kết nối.")
	}
	client, release, err := s.imageLibraryClient(ctx, *account)
	if err != nil {
		return nil, nil, err
	}
	projects, err := s.FetchAccountProjects(ctx, client)
	if err != nil {
		release()
		return nil, nil, err
	}
	now := time.Now().In(time.Local)
	for _, project := range projects {
		if project.ID == projectID {
			projectMode, createdAt, ok := s.parseImageProjectTitle(project.Title, now)
			if ok && projectMode == mode && isRecentImageProject(createdAt, now) {
				return client, release, nil
			}
			break
		}
	}
	release()
	return nil, nil, errors.New("Project không tồn tại trong chế độ ảnh đã chọn hoặc đã cũ hơn 3 ngày.")
}

func (s *Provider) ensureDailyImageProject(
	ctx context.Context,
	client *chrome.FlowClient,
	account session.StoredAccount,
	requestedMode ImageProjectMode,
	projects []flowProject,
) (string, []flowProject, error) {
	if strings.TrimSpace(s.productName) == "" {
		return "", projects, errors.New("chưa có tên sản phẩm để tạo tên project Flow")
	}
	mode, err := normalizeImageProjectMode(requestedMode)
	if err != nil {
		return "", projects, err
	}
	now := time.Now().In(time.Local)
	day := now.Format("2006-01-02")
	key := dailyImageProjectKey{AccountID: account.ID, Day: day, Mode: mode}
	s.mu.Lock()
	cachedID := s.dailyImageProjects[key]
	s.mu.Unlock()
	if cachedID != "" {
		if projects == nil {
			return cachedID, nil, nil
		}
		present := make(map[string]bool, len(projects))
		for _, project := range projects {
			present[project.ID] = true
		}
		if present[cachedID] {
			return cachedID, projects, nil
		}
		s.mu.Lock()
		delete(s.dailyImageProjects, key)
		s.mu.Unlock()
	}
	if projects == nil {
		projects, err = s.FetchAccountProjects(ctx, client)
		if err != nil {
			return "", nil, fmt.Errorf("không lấy được danh sách project: %w", err)
		}
	}

	if project, found := s.findDailyImageProject(projects, mode, now); found {
		s.mu.Lock()
		s.dailyImageProjects[key] = project.ID
		s.mu.Unlock()
		return project.ID, projects, nil
	}

	projectID, project, err := s.createDailyImageProject(ctx, client, account, mode)
	if err != nil {
		return "", projects, err
	}
	projects = append(projects, project)
	return projectID, projects, nil
}

func (s *Provider) createDailyImageProject(
	ctx context.Context,
	client *chrome.FlowClient,
	account session.StoredAccount,
	mode ImageProjectMode,
) (string, flowProject, error) {
	if strings.TrimSpace(s.productName) == "" {
		return "", flowProject{}, errors.New("chưa có tên sản phẩm để tạo tên project Flow")
	}
	createdAt := time.Now().In(time.Local)
	day := createdAt.Format("2006-01-02")
	title := fmt.Sprintf("%s - %s - %s - %s", s.productName, imageProjectModeLabel(mode), createdAt.Format("02/01"), createdAt.Format("15:04:05"))
	projectID, err := s.CreateAccountProject(ctx, client, title)
	if err != nil {
		return "", flowProject{}, fmt.Errorf("không tạo được project %s trong ngày: %w", imageProjectModeLabel(mode), err)
	}
	project := flowProject{ID: projectID, Title: title, UpdatedAt: createdAt}
	key := dailyImageProjectKey{AccountID: account.ID, Day: day, Mode: mode}
	s.mu.Lock()
	if s.dailyImageProjects == nil {
		s.dailyImageProjects = make(map[dailyImageProjectKey]string)
	}
	s.dailyImageProjects[key] = projectID
	s.mu.Unlock()
	return projectID, project, nil
}

func (s *Provider) findDailyImageProject(projects []flowProject, mode ImageProjectMode, now time.Time) (flowProject, bool) {
	day := now.In(time.Local).Format("2006-01-02")
	for _, project := range projects {
		projectMode, createdAt, ok := s.parseImageProjectTitle(project.Title, now)
		if ok && projectMode == mode && createdAt.Format("2006-01-02") == day {
			return project, true
		}
	}
	return flowProject{}, false
}

func (s *Provider) resolveImageProject(
	ctx context.Context,
	client *chrome.FlowClient,
	account session.StoredAccount,
	requested string,
	mode ImageProjectMode,
) (string, error) {
	if requested != "" {
		return requested, nil
	}
	mode, err := normalizeImageProjectMode(mode)
	if err != nil {
		return "", err
	}
	projectID, _, err := s.ensureDailyImageProject(ctx, client, account, mode, nil)
	return projectID, err
}

func (s *Provider) resolveImageProjectForRequest(
	ctx context.Context,
	client *chrome.FlowClient,
	account session.StoredAccount,
	requested string,
	mode ImageProjectMode,
	requestedMediaCount int,
) (string, func(int), error) {
	mode, err := normalizeImageProjectMode(mode)
	if err != nil {
		return "", nil, err
	}
	key := dailyImageProjectKey{
		AccountID: account.ID,
		Day:       time.Now().In(time.Local).Format("2006-01-02"),
		Mode:      mode,
	}
	projectLock := s.imageProjectLock(key)
	projectLock.Lock()
	projectID, err := s.resolveImageProject(ctx, client, account, requested, mode)
	if err != nil {
		projectLock.Unlock()
		return "", nil, err
	}
	generatedCount, err := s.countGeneratedImageMedia(ctx, client, projectID)
	if err != nil {
		projectLock.Unlock()
		return "", nil, fmt.Errorf("không kiểm tra được số ảnh đã tạo trong project: %w", err)
	}
	s.mu.Lock()
	if s.generatedImageCounts == nil {
		s.generatedImageCounts = make(map[string]int)
	}
	if s.reservedImageCounts == nil {
		s.reservedImageCounts = make(map[string]int)
	}
	confirmedCount := s.generatedImageCounts[projectID]
	reservedCount := s.reservedImageCounts[projectID]
	projectedCount := confirmedCount + reservedCount
	if generatedCount > projectedCount {
		confirmedCount = generatedCount - reservedCount
		s.generatedImageCounts[projectID] = confirmedCount
	}
	effectiveCount := max(generatedCount, confirmedCount+reservedCount)
	s.mu.Unlock()
	if shouldRotateImageProject(effectiveCount, requestedMediaCount) {
		projectID, _, err = s.createDailyImageProject(ctx, client, account, mode)
		if err != nil {
			projectLock.Unlock()
			return "", nil, fmt.Errorf("đã đạt giới hạn %d ảnh tạo trong project và không tạo được project tiếp theo: %w", MaxGeneratedMediaPerImageProject, err)
		}
	}
	s.mu.Lock()
	if _, exists := s.generatedImageCounts[projectID]; !exists {
		s.generatedImageCounts[projectID] = 0
	}
	s.reservedImageCounts[projectID] += requestedMediaCount
	s.mu.Unlock()
	projectLock.Unlock()

	finish := func(actualGeneratedCount int) {
		s.mu.Lock()
		if s.generatedImageCounts == nil {
			s.generatedImageCounts = make(map[string]int)
		}
		if s.reservedImageCounts == nil {
			s.reservedImageCounts = make(map[string]int)
		}
		remaining := s.reservedImageCounts[projectID] - requestedMediaCount
		if remaining <= 0 {
			delete(s.reservedImageCounts, projectID)
		} else {
			s.reservedImageCounts[projectID] = remaining
		}
		if actualGeneratedCount > 0 {
			s.generatedImageCounts[projectID] += actualGeneratedCount
		}
		s.mu.Unlock()
	}
	return projectID, finish, nil
}

func shouldRotateImageProject(generatedCount, requestedMediaCount int) bool {
	return generatedCount+requestedMediaCount > MaxGeneratedMediaPerImageProject
}

func (s *Provider) imageProjectLock(key dailyImageProjectKey) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.imageProjectLocks == nil {
		s.imageProjectLocks = make(map[dailyImageProjectKey]*sync.Mutex)
	}
	projectLock := s.imageProjectLocks[key]
	if projectLock == nil {
		projectLock = &sync.Mutex{}
		s.imageProjectLocks[key] = projectLock
	}
	return projectLock
}

func (s *Provider) forgetDailyImageProjects(accountID string) {
	s.mu.Lock()
	for key := range s.dailyImageProjects {
		if key.AccountID == accountID {
			delete(s.dailyImageProjects, key)
		}
	}
	s.mu.Unlock()
}

func (s *Provider) parseImageProjectTitle(title string, now time.Time) (ImageProjectMode, time.Time, bool) {
	if strings.TrimSpace(s.productName) == "" {
		return "", time.Time{}, false
	}
	for _, option := range AllImageProjectModes {
		prefix := fmt.Sprintf("%s - %s - ", s.productName, option.TSName)
		if !strings.HasPrefix(title, prefix) {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(title, prefix), " - ")
		if len(parts) != 2 {
			return "", time.Time{}, false
		}
		parsed, err := time.ParseInLocation("02/01 15:04:05", parts[0]+" "+parts[1], time.Local)
		if err != nil {
			return "", time.Time{}, false
		}
		localNow := now.In(time.Local)
		createdAt := time.Date(localNow.Year(), parsed.Month(), parsed.Day(), parsed.Hour(), parsed.Minute(), parsed.Second(), 0, time.Local)
		if createdAt.After(localNow.Add(time.Minute)) {
			createdAt = createdAt.AddDate(-1, 0, 0)
		}
		return option.Value, createdAt, true
	}
	return "", time.Time{}, false
}

func isRecentImageProject(createdAt, now time.Time) bool {
	localNow := now.In(time.Local)
	startOfToday := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.Local)
	oldest := startOfToday.AddDate(0, 0, -2)
	return !createdAt.Before(oldest) && !createdAt.After(localNow.Add(time.Minute))
}

func imageProjectModeLabel(mode ImageProjectMode) string {
	for _, option := range AllImageProjectModes {
		if option.Value == mode {
			return option.TSName
		}
	}
	return ""
}

func (s *Provider) imageLibraryClient(ctx context.Context, account session.StoredAccount) (*chrome.FlowClient, func(), error) {
	lease, err := s.acquireSpecificAccount(ctx, account)
	if err != nil {
		return nil, nil, err
	}
	releaseProfile := s.accounts.AcquireProfile(account.ProfileDir)
	fresh, err := s.syncSession(ctx, account)
	if err != nil {
		releaseProfile()
		lease.release()
		return nil, nil, err
	}
	client, err := s.flowClient(ctx, fresh, chrome.MethodProjects, chrome.MethodCreateProject, chrome.MethodProjectContents, chrome.MethodMedia)
	if err != nil {
		releaseProfile()
		lease.release()
		return nil, nil, err
	}
	return client, func() {
		s.keepCredentials(fresh, client)
		releaseProfile()
		lease.release()
	}, nil
}

type projectMediaEntry struct {
	name      string
	operation string
}

func (s *Provider) projectContentsPayload(ctx context.Context, client *chrome.FlowClient, projectID string) (string, error) {
	rpcID := client.Credentials().RPCs.ID(chrome.MethodProjectContents)
	fReq, err := BuildBatchEnvelope(rpcID, []any{"projects/" + projectID, nil, nil, nil, []any{1}})
	if err != nil {
		return "", err
	}
	limits := s.cfg.Get().Accounts
	body, err := client.Do(ctx, rpcID, fReq, time.Duration(limits.RequestTimeoutSeconds)*time.Second)
	if err != nil {
		return "", err
	}
	payload, err := chrome.ParseRPCPayload(string(body), rpcID)
	if err != nil {
		return "", err
	}
	return payload, nil
}

func (s *Provider) projectMediaEntries(ctx context.Context, client *chrome.FlowClient, projectID string) ([]projectMediaEntry, error) {
	payload, err := s.projectContentsPayload(ctx, client, projectID)
	if err != nil {
		return nil, err
	}
	media, err := parseVideoMedia(payload, 2)
	if err != nil {
		return nil, err
	}
	entries := make([]projectMediaEntry, 0, len(media))
	for _, item := range media {
		if item.name != "" {
			entries = append(entries, projectMediaEntry{name: item.name, operation: item.operation})
		}
	}
	return entries, nil
}

func (s *Provider) countGeneratedImageMedia(ctx context.Context, client *chrome.FlowClient, projectID string) (int, error) {
	payload, err := s.projectContentsPayload(ctx, client, projectID)
	if err != nil {
		return 0, err
	}
	return generatedImageMediaCount(payload)
}

func generatedImageMediaCount(payload string) (int, error) {
	var root []any
	if err := json.Unmarshal([]byte(payload), &root); err != nil {
		return 0, errors.New("phản hồi danh sách media trong project không đúng cấu trúc")
	}
	if len(root) <= 2 || root[2] == nil {
		return 0, nil
	}
	entries, ok := root[2].([]any)
	if !ok {
		return 0, errors.New("danh sách media trong project không đúng cấu trúc")
	}
	count := 0
	for _, media := range entries {
		// User uploads are not generation output and do not consume this limit.
		if projectMediaHasMarker(media, "useruploadedimage", "useruploadedvideo", "useruploadedaudio", "uploadedimage", "uploadedvideo", "uploadedmedia") {
			continue
		}
		if projectMediaHasMarker(media, "generatedimage", "generatedvideo", "generatedaudio") || projectMediaHasOperation(media) {
			count++
		}
	}
	return count, nil
}

func projectMediaHasMarker(value any, markers ...string) bool {
	switch item := value.(type) {
	case string:
		trimmed := strings.TrimSpace(item)
		if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
			var nested any
			if json.Unmarshal([]byte(trimmed), &nested) == nil {
				return projectMediaHasMarker(nested, markers...)
			}
		}
	case []any:
		for _, child := range item {
			if projectMediaHasMarker(child, markers...) {
				return true
			}
		}
	case map[string]any:
		for key, child := range item {
			normalizedKey := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
			for _, marker := range markers {
				if normalizedKey == marker {
					return true
				}
			}
			if projectMediaHasMarker(child, markers...) {
				return true
			}
		}
	}
	return false
}

func projectMediaHasOperation(value any) bool {
	switch item := value.(type) {
	case string:
		trimmed := strings.TrimSpace(item)
		if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
			var nested any
			if json.Unmarshal([]byte(trimmed), &nested) == nil {
				return projectMediaHasOperation(nested)
			}
		}
	case []any:
		if len(item) > 2 {
			mediaID, _ := item[0].(string)
			if operation, ok := item[2].(string); ok && validProjectMediaID(mediaID) && strings.TrimSpace(operation) != "" {
				return true
			}
		}
		if len(item) > 5 {
			mediaID, _ := item[4].(string)
			if operation, ok := item[5].(string); ok && validProjectMediaID(mediaID) && strings.TrimSpace(operation) != "" {
				return true
			}
		}
		for _, child := range item {
			if projectMediaHasOperation(child) {
				return true
			}
		}
	case map[string]any:
		hasOperation := false
		hasMediaID := false
		for key, child := range item {
			normalizedKey := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
			if normalizedKey == "operation" || normalizedKey == "operationid" || normalizedKey == "workflowid" {
				if operation, ok := child.(string); ok && strings.TrimSpace(operation) != "" {
					hasOperation = true
				}
			}
			if normalizedKey == "mediaid" || normalizedKey == "primarymediaid" || normalizedKey == "name" {
				if mediaID, ok := child.(string); ok && validProjectMediaID(mediaID) {
					hasMediaID = true
				}
			}
			if projectMediaHasOperation(child) {
				return true
			}
		}
		return hasOperation && hasMediaID
	}
	return false
}

func validProjectMediaID(value string) bool {
	_, err := uuid.Parse(strings.TrimSpace(value))
	return err == nil
}

func (s *Provider) getProjectMedia(ctx context.Context, client *chrome.FlowClient, mediaID string) (string, error) {
	rpcID := client.Credentials().RPCs.ID(chrome.MethodMedia)
	fReq, err := BuildBatchEnvelope(rpcID, []any{mediaID})
	if err != nil {
		return "", err
	}
	limits := s.cfg.Get().Accounts
	body, err := client.Do(ctx, rpcID, fReq, time.Duration(limits.RequestTimeoutSeconds)*time.Second)
	if err != nil {
		return "", err
	}
	return chrome.ParseRPCPayload(string(body), rpcID)
}

func flowProjectMediaName(mediaID, payload string) string {
	var root any
	if json.Unmarshal([]byte(payload), &root) == nil {
		if name := flowProjectDisplayName(root, mediaID); name != "" {
			return name
		}
	}
	name := path.Base(strings.TrimSpace(mediaID))
	if name == "." || name == "/" || name == "" {
		return ""
	}
	return name
}

func flowProjectDisplayName(value any, mediaID string) string {
	keys := []string{"displayName", "display_name", "fileName", "file_name", "filename", "title", "name"}
	switch item := value.(type) {
	case map[string]any:
		for _, key := range keys {
			if name, ok := item[key].(string); ok && validFlowDisplayName(name, mediaID) {
				return strings.TrimSpace(name)
			}
		}
		for _, child := range item {
			if name := flowProjectDisplayName(child, mediaID); name != "" {
				return name
			}
		}
	case []any:
		for _, child := range item {
			if name := flowProjectDisplayName(child, mediaID); name != "" {
				return name
			}
		}
	case string:
		trimmed := strings.TrimSpace(item)
		if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
			var nested any
			if json.Unmarshal([]byte(trimmed), &nested) == nil {
				return flowProjectDisplayName(nested, mediaID)
			}
		}
	}
	return ""
}

func validFlowDisplayName(name, mediaID string) bool {
	name = strings.TrimSpace(name)
	return name != "" && name != mediaID && !strings.Contains(name, "://") && !strings.HasPrefix(name, "projects/")
}

func flowProjectContainsVideo(payload string) bool {
	var root any
	if json.Unmarshal([]byte(payload), &root) != nil {
		return false
	}
	var visit func(any) bool
	visit = func(value any) bool {
		switch item := value.(type) {
		case []any:
			for _, child := range item {
				if visit(child) {
					return true
				}
			}
		case map[string]any:
			for _, child := range item {
				if visit(child) {
					return true
				}
			}
		case string:
			trimmed := strings.TrimSpace(item)
			if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
				var nested any
				if json.Unmarshal([]byte(trimmed), &nested) == nil && visit(nested) {
					return true
				}
			}
			if isFlowProjectVideoURL(trimmed) {
				return true
			}
		}
		return false
	}
	return visit(root)
}

func isFlowProjectVideoURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || !strings.Contains(u.Path, "/video/") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "flow-content.google" || strings.HasSuffix(host, ".googleusercontent.com")
}

func flowProjectImageURLs(payload string) []string {
	var root any
	if json.Unmarshal([]byte(payload), &root) != nil {
		return nil
	}
	seen := make(map[string]bool)
	var urls []string
	var visit func(any)
	visit = func(value any) {
		switch item := value.(type) {
		case []any:
			for _, child := range item {
				visit(child)
			}
		case map[string]any:
			for _, child := range item {
				visit(child)
			}
		case string:
			trimmed := strings.TrimSpace(item)
			if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
				var nested any
				if json.Unmarshal([]byte(trimmed), &nested) == nil {
					visit(nested)
					return
				}
			}
			if !seen[trimmed] && isFlowProjectImageURL(trimmed) {
				seen[trimmed] = true
				urls = append(urls, trimmed)
			}
		}
	}
	visit(root)
	return urls
}

func isFlowProjectImageURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host != "flow-content.google" && !strings.HasSuffix(host, ".googleusercontent.com") {
		return false
	}
	return strings.Contains(u.Path, "/image/")
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
