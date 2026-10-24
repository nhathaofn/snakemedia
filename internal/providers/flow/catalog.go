package flow

import (
	"SnakeMedia/internal/providers/flow/session"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"SnakeMedia/internal/models"
	"SnakeMedia/internal/providers/flow/chrome"
)

// FlowModel represents an image generation model available in Google Flow.
type FlowModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Default     bool   `json:"default"`
	// AvailableQualities comes from the account's live Flow model catalogue.
	// 1K is the original generated image; higher qualities require an upsampler.
	AvailableQualities []ImageQuality `json:"availableQualities"`
	// MaxReferenceImages is how many reference images Flow accepts for this model;
	// 0 means the model takes none.
	MaxReferenceImages int `json:"maxReferenceImages"`
}

type modelCache struct {
	buildLabel string
	tier       int
	models     []FlowModel
	expires    time.Time
}

// GetImageModels returns the image models Google Flow reports for the account
// whose turn is next in the rotation.
func (s *Provider) GetImageModels(ctx context.Context) models.Response[[]FlowModel] {
	acc, err := s.pickAccount(false)
	if err != nil {
		return models.ErrorResponse[[]FlowModel](
			"Không thể lấy model: "+err.Error(),
			models.CodeUnavailable,
		)
	}

	modelsList, err := s.modelsFor(ctx, *acc)
	if err != nil {
		return models.ErrorResponse[[]FlowModel](
			"Không lấy được danh sách model từ Google Flow: "+err.Error(),
			models.CodeUnavailable,
		)
	}
	return models.SuccessResponse(modelsList)
}

// modelsFor returns the account's model catalogue, cached for modelCacheTTL.
func (s *Provider) modelsFor(ctx context.Context, acc session.StoredAccount) ([]FlowModel, error) {
	s.mu.Lock()
	if cached, ok := s.models[acc.ID]; ok && time.Now().Before(cached.expires) && cached.buildLabel == acc.Credentials.BuildLabel && cached.tier == accountTier(acc) {
		list := cloneFlowModels(cached.models)
		s.mu.Unlock()
		return list, nil
	}
	s.mu.Unlock()

	list, err := s.fetchLiveModels(ctx, acc)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.models[acc.ID] = modelCache{buildLabel: acc.Credentials.BuildLabel, tier: accountTier(acc), models: list, expires: time.Now().Add(modelCacheTTL)}
	s.mu.Unlock()
	return cloneFlowModels(list), nil
}

func cloneFlowModels(models []FlowModel) []FlowModel {
	cloned := make([]FlowModel, len(models))
	copy(cloned, models)
	for i := range cloned {
		cloned[i].AvailableQualities = append([]ImageQuality(nil), models[i].AvailableQualities...)
	}
	return cloned
}

// findImageModel returns the account's image model with this ID.
func (s *Provider) findImageModel(ctx context.Context, acc session.StoredAccount, modelID string) (FlowModel, error) {
	list, err := s.modelsFor(ctx, acc)
	if err != nil {
		return FlowModel{}, fmt.Errorf("không đọc được danh sách model của tài khoản: %w", err)
	}
	for _, m := range list {
		if m.ID == modelID {
			return m, nil
		}
	}
	return FlowModel{}, fmt.Errorf("Model %q không có trong danh sách model của tài khoản.", modelID)
}

// checkReferenceLimit enforces the model's own reference image limit.
func checkReferenceLimit(m FlowModel, count int) error {
	switch {
	case count == 0:
		return nil
	case m.MaxReferenceImages == 0:
		return fmt.Errorf("Model %s không nhận ảnh tham chiếu.", m.Name)
	case count > m.MaxReferenceImages:
		return fmt.Errorf("Model %s nhận tối đa %d ảnh tham chiếu (đang chọn %d).", m.Name, m.MaxReferenceImages, count)
	}
	return nil
}

// displayName drops the decoration Flow puts before model names ("🍌 Nano Banana Pro").
func displayName(raw string) string {
	return strings.TrimLeftFunc(raw, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// queryModelsFromAccount reads the account's image models through RPC HTrJv.
func (s *Provider) queryModelsFromAccount(ctx context.Context, acc session.StoredAccount) ([]FlowModel, error) {
	payload, err := s.fetchModelsPayload(ctx, acc)
	if err != nil {
		return nil, err
	}
	return parseImageModels(payload, accountTier(acc))
}

// parseImageModels lists the generation models of every image family. The default
// is the family Flow preselects for the account's tier (or for the first tier it
// lists when the account tier is unknown or absent).
func parseImageModels(payload string, tier int) ([]FlowModel, error) {
	var outer []any
	if err := json.Unmarshal([]byte(payload), &outer); err != nil || len(outer) == 0 {
		return nil, errors.New("phản hồi GetModels không đúng cấu trúc")
	}
	root := asList(outer[0])
	if len(root) <= modelsImageFamiliesIndex {
		return nil, errors.New("phản hồi GetModels không có nhóm model ảnh")
	}

	defaultAlias := ""
	for _, d := range asList(root[modelsTierDefaultsIndex]) {
		entry := asList(d)
		if len(entry) < 2 {
			continue
		}
		cfg := asList(entry[1])
		if len(cfg) <= 2 {
			continue
		}
		alias, _ := cfg[2].(string)
		if code, ok := entry[0].(float64); ok && int(code) == tier {
			defaultAlias = alias
			break
		}
		if defaultAlias == "" {
			defaultAlias = alias
		}
	}

	var list []FlowModel
	availableQualities := imageQualitiesFromFamilies(asList(root[modelsImageFamiliesIndex]))
	for _, f := range asList(root[modelsImageFamiliesIndex]) {
		family := asList(f)
		if len(family) < 4 {
			continue
		}
		name, _ := family[0].(string)
		alias, _ := family[3].(string)
		for _, m := range asList(family[1]) {
			entry := asList(m)
			if len(entry) <= modelCapabilitiesIndex {
				continue
			}
			id, _ := entry[0].(string)
			if id == "" || !hasCapability(entry[modelCapabilitiesIndex], capabilityTextToImage) {
				continue
			}
			maxRefs := 0
			if len(entry) > modelMaxReferencesIndex {
				if n, ok := entry[modelMaxReferencesIndex].(float64); ok && n > 0 {
					maxRefs = int(n)
				}
			}
			list = append(list, FlowModel{
				ID:                 id,
				Name:               displayName(name),
				Default:            alias != "" && alias == defaultAlias,
				AvailableQualities: append([]ImageQuality(nil), availableQualities...),
				MaxReferenceImages: maxRefs,
			})
		}
	}
	if len(list) == 0 {
		return nil, errors.New("Google Flow không trả về model tạo ảnh nào cho tài khoản này")
	}
	return list, nil
}

func imageQualitiesFromFamilies(families []any) []ImageQuality {
	available := map[ImageQuality]bool{ImageQuality1K: true}
	for _, rawFamily := range families {
		family := asList(rawFamily)
		if len(family) < 2 {
			continue
		}
		name, _ := family[0].(string)
		var quality ImageQuality
		switch strings.ToUpper(strings.TrimSpace(name)) {
		case string(ImageQuality2K):
			quality = ImageQuality2K
		case string(ImageQuality4K):
			quality = ImageQuality4K
		default:
			continue
		}
		for _, rawModel := range asList(family[1]) {
			entry := asList(rawModel)
			if len(entry) <= modelCapabilitiesIndex {
				continue
			}
			id, _ := entry[0].(string)
			// A resolution family is offered as an upsampler when the registry
			// reports a model variant with a non-generation capability.
			if id != "" && !hasCapability(entry[modelCapabilitiesIndex], capabilityTextToImage) {
				available[quality] = true
				break
			}
		}
	}
	qualities := []ImageQuality{ImageQuality1K}
	if available[ImageQuality2K] {
		qualities = append(qualities, ImageQuality2K)
	}
	if available[ImageQuality4K] {
		qualities = append(qualities, ImageQuality4K)
	}
	return qualities
}

// FlowVideoModel is a video model family offering text-to-video for the account's
// tier, with the durations, ratios and qualities Flow lists for it.
type FlowVideoModel struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Default      bool     `json:"default"`
	Durations    []int    `json:"durations"`
	AspectRatios []string `json:"aspectRatios"`
	Resolutions  []string `json:"resolutions"`
	// MaxReferenceImages is how many reference images the family's
	// reference-to-video variants accept on this tier; 0 means none.
	MaxReferenceImages int `json:"maxReferenceImages"`
}

type videoUsage struct {
	key         string
	caps        [][]int
	aspects     []int
	tiers       map[int]bool
	costs       map[int]int
	duration    int
	anyDuration bool
	resolutions []int
	hidden      bool
	maxRefs     int
	// maxInputSeconds is the longest source video an edit usage reads.
	maxInputSeconds int
}

type videoFamily struct {
	id     string
	name   string
	usages []videoUsage
}

type videoModelCache struct {
	buildLabel string
	tier       int
	families   []videoFamily
	defaultID  string
	expires    time.Time
}

// parseVideoFamilies reads the video families and the tier's preselected family.
func parseVideoFamilies(payload string, tier int) ([]videoFamily, string, error) {
	var outer []any
	if err := json.Unmarshal([]byte(payload), &outer); err != nil || len(outer) == 0 {
		return nil, "", errors.New("phản hồi GetModels không đúng cấu trúc")
	}
	root := asList(outer[0])
	if len(root) <= modelsVideoFamiliesIndex {
		return nil, "", errors.New("phản hồi GetModels không có nhóm model video")
	}
	defaultID := ""
	for _, d := range asList(root[modelsTierDefaultsIndex]) {
		entry := asList(d)
		if len(entry) < 2 {
			continue
		}
		cfg := asList(entry[1])
		if len(cfg) <= tierDefaultVideoIndex {
			continue
		}
		id, _ := cfg[tierDefaultVideoIndex].(string)
		if code, ok := entry[0].(float64); ok && int(code) == tier {
			defaultID = id
			break
		}
		if defaultID == "" {
			defaultID = id
		}
	}

	var families []videoFamily
	for _, f := range asList(root[modelsVideoFamiliesIndex]) {
		family := asList(f)
		if len(family) < 4 {
			continue
		}
		name, _ := family[0].(string)
		id, _ := family[3].(string)
		if id == "" {
			continue
		}
		vf := videoFamily{id: id, name: name}
		for _, u := range asList(family[1]) {
			if usage, ok := parseVideoUsage(asList(u)); ok {
				vf.usages = append(vf.usages, usage)
			}
		}
		families = append(families, vf)
	}
	if len(families) == 0 {
		return nil, "", errors.New("Google Flow không trả về model video nào cho tài khoản này")
	}
	return families, defaultID, nil
}

func parseVideoUsage(entry []any) (videoUsage, bool) {
	at := func(i int) any {
		if i < len(entry) {
			return entry[i]
		}
		return nil
	}
	key, _ := at(0).(string)
	if key == "" {
		return videoUsage{}, false
	}
	u := videoUsage{key: key, tiers: map[int]bool{}, costs: map[int]int{}, duration: videoDefaultDuration}
	for _, c := range asList(firstOf(at(videoCapabilitiesIndex))) {
		set := intList(firstOf(c))
		if slices.Contains(set, 20) {
			set = slices.DeleteFunc(set, func(v int) bool { return v == 17 })
		}
		set = slices.DeleteFunc(set, func(v int) bool { return v == 0 })
		if len(set) > 0 {
			u.caps = append(u.caps, set)
		}
	}
	u.aspects = intList(firstOf(at(videoAspectsIndex)))
	for _, t := range asList(at(videoTierCostsIndex)) {
		pair := asList(t)
		if len(pair) < 2 {
			continue
		}
		code, ok := pair[0].(float64)
		// An available tier carries its cost in field 1; field 2 marks it unavailable.
		value := asList(pair[1])
		u.tiers[int(code)] = ok && len(value) > 0 && value[0] != nil
		if !u.tiers[int(code)] {
			continue
		}
		cost := asList(value[0])
		if len(cost) > 1 {
			if credits, ok := cost[1].(float64); ok && credits >= 0 {
				u.costs[int(code)] = int(credits)
			}
		}
	}
	if d, ok := at(videoDurationIndex).(float64); ok && d > 0 {
		u.duration = int(d)
	}
	u.anyDuration, _ = at(videoAnyDurationIndex).(bool)
	u.resolutions = intList(firstOf(at(videoResolutionsIndex)))
	u.hidden, _ = at(videoHiddenIndex).(bool)
	if n, ok := at(modelMaxReferencesIndex).(float64); ok && n > 0 {
		u.maxRefs = int(n)
	}
	u.maxInputSeconds = videoDefaultInputSeconds
	if limits := asList(at(videoInputLimitsIndex)); len(limits) > videoMaxInputSecondsSlot {
		if n, ok := limits[videoMaxInputSecondsSlot].(float64); ok && n > 0 {
			u.maxInputSeconds = int(n)
		}
	}
	return u, true
}

// Capability sets Flow's variant picker (iO) tries, most specific first. A
// prompt alone needs {1}; reference images add 6 plus the reference kinds 18
// and 19 (ordered by _.jWa), which are dropped from the end until a usage offers
// that exact set. {1} is never a fallback for references: they would be lost.
var (
	textCapabilitySets      = [][]int{{1}}
	referenceCapabilitySets = [][]int{{1, 6, 18, 19}, {1, 6, 18}, {1, 6}}
)

// offered reports whether the account's tier may use the usage.
func (u videoUsage) offered(tier int) bool {
	return !u.hidden && u.tiers[tier]
}

func (u videoUsage) creditCost(tier int) (int, bool) {
	cost, ok := u.costs[tier]
	return cost, ok
}

// hasCapabilities reports whether one of the usage's capability sets is exactly want.
func (u videoUsage) hasCapabilities(want []int) bool {
	for _, set := range u.caps {
		if len(set) == len(want) && !slices.ContainsFunc(want, func(c int) bool { return !slices.Contains(set, c) }) {
			return true
		}
	}
	return false
}

// textToVideo reports whether the usage generates from a prompt alone for the tier.
func (u videoUsage) textToVideo(tier int) bool {
	return u.offered(tier) && u.hasCapabilities(textCapabilitySets[0])
}

// referenceToVideo reports whether the usage takes reference images on the tier.
func (u videoUsage) referenceToVideo(tier int) bool {
	return u.offered(tier) && slices.ContainsFunc(referenceCapabilitySets, u.hasCapabilities)
}

// resolutionCodes returns the usage's resolutions, 720p when it lists none.
func (u videoUsage) resolutionCodes() []int {
	if len(u.resolutions) == 0 {
		return []int{videoResolution720p}
	}
	return u.resolutions
}

// videoModels lists families usable for text-to-video on the tier.
func videoModels(families []videoFamily, defaultID string, tier int) []FlowVideoModel {
	ratioName := map[int]string{1: "9:16", 2: "16:9"}
	var list []FlowVideoModel
	for _, f := range families {
		durations := map[int]bool{}
		ratios := map[string]bool{}
		resolutions := map[int]bool{}
		for _, u := range f.usages {
			if !u.textToVideo(tier) {
				continue
			}
			for _, r := range u.resolutionCodes() {
				resolutions[r] = true
			}
			if !u.anyDuration {
				durations[u.duration] = true
			}
			for _, a := range u.aspects {
				if name, ok := ratioName[a]; ok {
					ratios[name] = true
				}
			}
		}
		if len(durations) == 0 || len(ratios) == 0 {
			continue
		}
		m := FlowVideoModel{ID: f.id, Name: displayName(f.name), Default: f.id == defaultID}
		for _, u := range f.usages {
			if u.referenceToVideo(tier) {
				m.MaxReferenceImages = max(m.MaxReferenceImages, u.maxRefs)
			}
		}
		for d := range durations {
			m.Durations = append(m.Durations, d)
		}
		sort.Ints(m.Durations)
		for _, name := range []string{"16:9", "9:16"} {
			if ratios[name] {
				m.AspectRatios = append(m.AspectRatios, name)
			}
		}
		for _, name := range videoResolutionOrder {
			if resolutions[FlowVideoResolution[name]] {
				m.Resolutions = append(m.Resolutions, name)
			}
		}
		list = append(list, m)
	}
	return list
}

// selectVideoVariant picks the usage Flow would send for these settings,
// trying capability sets in the order Flow's own picker does.
func selectVideoVariant(f videoFamily, tier, aspect, duration, resolution int, withReferences bool) (videoUsage, error) {
	sets := textCapabilitySets
	if withReferences {
		sets = referenceCapabilitySets
	}
	for _, want := range sets {
		for _, u := range f.usages {
			if u.offered(tier) && u.hasCapabilities(want) && slices.Contains(u.aspects, aspect) &&
				(u.anyDuration || u.duration == duration) && slices.Contains(u.resolutionCodes(), resolution) {
				return u, nil
			}
		}
	}
	if withReferences {
		return videoUsage{}, fmt.Errorf("Model %s không nhận ảnh tham chiếu với %d giây, chất lượng và tỉ lệ đã chọn ở gói tài khoản hiện tại.", displayName(f.name), duration)
	}
	return videoUsage{}, fmt.Errorf("Model %s không có biến thể %d giây, chất lượng này cho tỉ lệ đã chọn ở gói tài khoản hiện tại.", displayName(f.name), duration)
}

// videoFamiliesFor returns the account's video catalogue, cached for modelCacheTTL.
func (s *Provider) videoFamiliesFor(ctx context.Context, acc session.StoredAccount) ([]videoFamily, string, error) {
	s.mu.Lock()
	cached, ok := s.videoModels[acc.ID]
	s.mu.Unlock()
	if ok && time.Now().Before(cached.expires) && cached.buildLabel == acc.Credentials.BuildLabel && cached.tier == accountTier(acc) {
		return cached.families, cached.defaultID, nil
	}
	payload, err := s.fetchModelsPayload(ctx, acc)
	if err != nil {
		return nil, "", err
	}
	families, defaultID, err := parseVideoFamilies(payload, accountTier(acc))
	if err != nil {
		return nil, "", err
	}
	s.mu.Lock()
	s.videoModels[acc.ID] = videoModelCache{buildLabel: acc.Credentials.BuildLabel, tier: accountTier(acc), families: families, defaultID: defaultID, expires: time.Now().Add(modelCacheTTL)}
	s.mu.Unlock()
	return families, defaultID, nil
}

// fetchModelsPayloadFromAccount reads the raw GetModels payload through RPC HTrJv.
// A stale stored session (HTTP 401) is refreshed from the account's Chrome
// profile and the call retried once.
func (s *Provider) fetchModelsPayloadFromAccount(ctx context.Context, acc session.StoredAccount) (string, error) {
	if s.accounts == nil {
		return "", errors.New("dịch vụ tài khoản chưa được khởi tạo")
	}
	payload, err := s.readModelsPayload(ctx, acc)
	if !isUnauthorized(err) {
		return payload, err
	}
	synced, syncErr := s.syncSession(ctx, acc)
	if syncErr != nil {
		return "", fmt.Errorf("phiên Flow đã hết hạn và không đồng bộ lại được: %w", syncErr)
	}
	return s.readModelsPayload(ctx, synced)
}

func (s *Provider) readModelsPayload(ctx context.Context, acc session.StoredAccount) (string, error) {
	limits := s.cfg.Get().Accounts
	client, err := s.catalogueClient(ctx, acc)
	if err != nil {
		return "", err
	}
	defer s.keepCredentials(acc, client)
	rpcID := client.Credentials().RPCs.ID(chrome.MethodModels)
	fReq, err := BuildBatchEnvelope(rpcID, "[]")
	if err != nil {
		return "", err
	}
	raw, err := client.Do(ctx, rpcID, fReq, time.Duration(limits.RequestTimeoutSeconds)*time.Second)
	if err != nil {
		return "", err
	}
	return chrome.ParseRPCPayload(string(raw), rpcID)
}

// GetVideoModels returns the text-to-video families of the account whose turn is next.
func (s *Provider) GetVideoModels(ctx context.Context) models.Response[[]FlowVideoModel] {
	acc, err := s.pickAccount(false)
	if err != nil {
		return models.ErrorResponse[[]FlowVideoModel]("Không thể lấy model video: "+err.Error(), models.CodeUnavailable)
	}
	families, defaultID, err := s.videoFamiliesFor(ctx, *acc)
	if err != nil {
		return models.ErrorResponse[[]FlowVideoModel]("Không lấy được danh sách model video từ Google Flow: "+err.Error(), models.CodeUnavailable)
	}
	list := videoModels(families, defaultID, accountTier(*acc))
	if len(list) == 0 {
		return models.ErrorResponse[[]FlowVideoModel]("Gói tài khoản này không có model tạo video từ văn bản.", models.CodeUnavailable)
	}
	return models.SuccessResponse(list)
}
