package muse

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const SelfVoice = "__self"

// Pace is the standard reading instruction ensuring natural rhythm without dragging.
const Pace = "Đọc tốc độ tự nhiên như người thật nói chuyện (khoảng 4–5 chữ mỗi giây), không kéo giãn chữ, không ngắt nghỉ dài giữa câu; lời ngắn hơn cảnh thì phần còn lại để nhạc nền hoặc tiếng động nhẹ, không kéo dài giọng cho đủ thời lượng."

type SceneItem struct {
	Text      string `json:"text"`
	Voiceover string `json:"voiceover,omitempty"`
	Img       string `json:"img,omitempty"`
	Product   string `json:"product,omitempty"`
}

type ScriptConfig struct {
	Prefix       string `json:"prefix"`
	Ratio        string `json:"ratio"`
	Duration     string `json:"duration"` // "10s", "15s", "20s", "30s", "60s", "each"
	Voice        string `json:"voice"`
	Host         string `json:"host"` // "nam", "nữ"
	Style        string `json:"style"`
	Audio        string `json:"audio"`
	Merge        bool   `json:"merge"`
	Music        bool   `json:"music"`
	Board        string `json:"board"` // "" | "auto" | "review"
	CharacterTag string `json:"characterTag,omitempty"`
}

type VoiceOption struct {
	Value  string `json:"value"`
	Label  string `json:"label"`
	Gender string `json:"gender"`
}

var AllVoices = []VoiceOption{
	{Value: "", Label: "Không lồng tiếng (giữ tiếng gốc)", Gender: ""},
	{Value: SelfVoice, Label: "Nhân vật tự nói — giọng Việt, khớp môi (như reel review)", Gender: ""},
	{Value: "Chipper Tambourine", Label: "Chipper Tambourine — Nam · trẻ trung, vui tươi", Gender: "nam"},
	{Value: "Upbeat Buoy", Label: "Upbeat Buoy — Nam · trầm ấm, hào hứng, hợp chốt sale", Gender: "nam"},
	{Value: "Warm Pebble", Label: "Warm Pebble — Nam · ấm, chuyên nghiệp, tin cậy", Gender: "nam"},
	{Value: "Husky Campfire", Label: "Husky Campfire — Nam · khàn nhẹ, ấm, thuyết phục", Gender: "nam"},
	{Value: "Spirited Canyon", Label: "Spirited Canyon — Nam · trầm, mạnh mẽ, nhiều năng lượng", Gender: "nam"},
	{Value: "Aria", Label: "Aria — Nữ · thân thiện, ấm áp (Meta AI)", Gender: "nữ"},
	{Value: "Dynamic Sparkler", Label: "Dynamic Sparkler — Nữ · tự tin, năng động", Gender: "nữ"},
	{Value: "Cheerful Sunflower", Label: "Cheerful Sunflower — Nữ · tươi sáng, dễ nghe", Gender: "nữ"},
	{Value: "Bubbly Pompom", Label: "Bubbly Pompom — Nữ · sôi nổi, bắt tai", Gender: "nữ"},
	{Value: "Zippy Cork", Label: "Zippy Cork — Nữ · nhanh nhẹn, hợp TikTok", Gender: "nữ"},
	{Value: "Fizzy Bicycle", Label: "Fizzy Bicycle — Nam · cao, nhiều năng lượng", Gender: "nam"},
	{Value: "Sprightly Windchime", Label: "Sprightly Windchime — Nam · sáng, tinh nghịch", Gender: "nam"},
	{Value: "Warm", Label: "Warm — Meta AI · ấm", Gender: ""},
	{Value: "Smooth", Label: "Smooth — Meta AI · mượt", Gender: ""},
}

var durRe = regexp.MustCompile(`^(\d+)s$`)

func cfgSecs(cfg ScriptConfig) int {
	if cfg.Duration == "each" {
		return 10
	}
	if m := durRe.FindStringSubmatch(cfg.Duration); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			return n
		}
	}
	return 15
}

func uniqueImgs(scenes []SceneItem, refs []string) []string {
	var list []string
	seen := make(map[string]bool)
	for _, r := range refs {
		if r != "" && !seen[r] {
			seen[r] = true
			list = append(list, r)
		}
	}
	for _, s := range scenes {
		if s.Img != "" && !seen[s.Img] {
			seen[s.Img] = true
			list = append(list, s.Img)
		}
	}
	return list
}

// BuildPrompt constructs the prompt for Muse following HaoHao guidelines:
// separates scene visuals from scene voiceover so Muse does not confuse them.
func BuildPrompt(items []SceneItem, cfg ScriptConfig, refs []string, hostLine string) string {
	secs := cfgSecs(cfg)
	voice := strings.TrimSpace(cfg.Voice)
	self := voice == SelfVoice
	prefix := strings.TrimSpace(cfg.Prefix)
	prefix = strings.TrimRight(prefix, ":. ")
	if prefix == "" {
		prefix = "Tạo video chân thực"
	}
	n := len(items)
	if n == 0 {
		return prefix
	}
	keys := uniqueImgs(items, refs)

	var product string
	for _, it := range items {
		if it.Product != "" {
			product = it.Product
			break
		}
	}

	at := func(k int) int {
		return int(math.Round(float64(k) * float64(secs) / float64(n)))
	}

	var head string
	if n > 1 {
		ratioStr := ""
		if cfg.Ratio != "" {
			ratioStr = fmt.Sprintf("Tỉ lệ %s, ", cfg.Ratio)
		}
		styleStr := ""
		if cfg.Style != "" {
			styleStr = fmt.Sprintf(", phong cách %s", cfg.Style)
		}
		head = fmt.Sprintf("%s. %stổng %ds, gồm %d cảnh%s.", prefix, ratioStr, secs, n, styleStr)
	} else {
		ratioStr := ""
		if cfg.Ratio != "" {
			ratioStr = fmt.Sprintf("Tỉ lệ %s, ", cfg.Ratio)
		}
		styleStr := ""
		if cfg.Style != "" {
			styleStr = fmt.Sprintf(", phong cách %s", cfg.Style)
		}
		head = fmt.Sprintf("%s. %sthời lượng %ds%s.", prefix, ratioStr, secs, styleStr)
	}

	var subjectParts []string
	if product != "" {
		if len(keys) > 0 {
			subjectParts = append(subjectParts, fmt.Sprintf("Sản phẩm: %s (giống ảnh đính kèm).", product))
		} else {
			subjectParts = append(subjectParts, fmt.Sprintf("Sản phẩm: %s.", product))
		}
	} else if len(keys) > 0 {
		subjectParts = append(subjectParts, "Giữ đúng chủ thể trong ảnh đính kèm.")
	}

	if hostLine != "" {
		if cfg.Host != "" {
			subjectParts = append(subjectParts, fmt.Sprintf("%s; giọng nói/lồng tiếng cũng là giọng %s, khớp với người dẫn.", hostLine, cfg.Host))
		} else {
			subjectParts = append(subjectParts, hostLine+".")
		}
	}
	subject := strings.Join(subjectParts, "\n")

	var sceneBlocks []string
	for k, it := range items {
		imgStr := ""
		if len(keys) > 1 && it.Img != "" {
			idx := -1
			for ki, kv := range keys {
				if kv == it.Img {
					idx = ki + 1
					break
				}
			}
			if idx > 0 {
				imgStr = fmt.Sprintf(", dùng ảnh %d", idx)
			}
		}

		lines := []string{
			fmt.Sprintf("Cảnh %d (%d–%ds%s)", k+1, at(k), at(k+1), imgStr),
			fmt.Sprintf("Hình: %s", it.Text),
		}
		if voice != "" {
			speaker := "Lời"
			if self {
				speaker = "Nhân vật nói vào máy"
			}
			if it.Voiceover != "" {
				lines = append(lines, fmt.Sprintf("%s: \"%s\"", speaker, it.Voiceover))
			} else {
				words := int(math.Round(float64(secs) / float64(n) * 4.4))
				scSecs := int(math.Round(float64(secs) / float64(n)))
				lines = append(lines, fmt.Sprintf("%s: tự viết lời hợp cảnh, khoảng %d chữ để nói kín %ds.", speaker, words, scSecs))
			}
		}
		sceneBlocks = append(sceneBlocks, strings.Join(lines, "\n"))
	}

	var tailParts []string
	if self {
		hostSpec := ""
		if cfg.Host != "" {
			hostSpec = fmt.Sprintf(" bằng giọng %s", cfg.Host)
		}
		tailParts = append(tailParts, fmt.Sprintf("Âm thanh: Người dẫn trong video tự nói tiếng Việt%s vào máy, khẩu hình khớp từng lời (lip-sync), giọng tự nhiên như người thật quay review, cùng một giọng xuyên suốt; cảnh không thấy mặt người nói thì vẫn giọng đó nói tiếp. Nói đúng và đủ lời từng cảnh, không cắt bớt. %s Giữ tiếng động thật nhỏ của cảnh phía sau, không nhạc to át giọng.", hostSpec, Pace))
	} else if voice != "" {
		tailParts = append(tailParts, fmt.Sprintf("Âm thanh: Tắt tiếng gốc. Lồng tiếng tiếng Việt giọng \"%s\" đọc liền mạch cả video như một bài thuyết minh: đọc đúng và đủ lời từng cảnh, không cắt bớt hay rút gọn, chuyển cảnh thì nối giọng sang lời cảnh sau ngay. %s", voice, Pace))
	} else if cfg.Audio != "" {
		tailParts = append(tailParts, fmt.Sprintf("Âm thanh: %s", cfg.Audio))
	}

	tailParts = append(tailParts, "Không chèn phụ đề, chữ hay logo lên video.")
	reqNote := "Yêu cầu: video MỚI hoàn toàn, không gửi lại video cũ, làm ngay không hỏi lại."
	if n > 1 {
		reqNote += " Tự ghép các cảnh thành 1 video, chỉ gửi 1 video cuối cùng."
	}
	tailParts = append(tailParts, reqNote)
	tail := strings.Join(tailParts, "\n")

	var allBlocks []string
	headerPart := strings.TrimSpace(head)
	if subject != "" {
		headerPart += "\n" + subject
	}
	allBlocks = append(allBlocks, headerPart)
	allBlocks = append(allBlocks, sceneBlocks...)
	allBlocks = append(allBlocks, tail)
	return strings.Join(allBlocks, "\n\n")
}

const BoardMax = 6

// BuildBoardPrompt asks Muse to draw starting frames of each scene first.
func BuildBoardPrompt(items []SceneItem, cfg ScriptConfig, refs []string, hostLine string) string {
	n := len(items)
	if n > BoardMax {
		n = BoardMax
	}
	ratio := cfg.Ratio
	if ratio == "" {
		ratio = "9:16"
	}
	styleStr := ""
	if cfg.Style != "" {
		styleStr = fmt.Sprintf(", phong cách %s", cfg.Style)
	}

	var product string
	for _, it := range items {
		if it.Product != "" {
			product = it.Product
			break
		}
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("Tạo %d ảnh storyboard tỉ lệ %s, chân thực như ảnh chụp điện thoại, mỗi ảnh là khung hình mở đầu của 1 cảnh trong video %ds%s.", n, ratio, cfgSecs(cfg), styleStr))
	if product != "" {
		parts = append(parts, fmt.Sprintf("Sản phẩm: %s.", product))
	}
	if len(uniqueImgs(items, refs)) > 0 {
		parts = append(parts, "Sản phẩm phải giữ ĐÚNG như ảnh đính kèm — không vẽ lại, không đổi chữ, màu hay chi tiết sản phẩm.")
	}
	if hostLine != "" {
		parts = append(parts, fmt.Sprintf("%s, cùng trang phục, có mặt trong cả %d ảnh; cùng bối cảnh xuyên suốt %d ảnh.", hostLine, n, n))
	} else {
		parts = append(parts, fmt.Sprintf("Nếu có người thì cùng 1 người, cùng trang phục; cùng bối cảnh xuyên suốt %d ảnh.", n))
	}

	for k := 0; k < n; k++ {
		parts = append(parts, fmt.Sprintf("Ảnh %d: %s", k+1, items[k].Text))
	}
	parts = append(parts, fmt.Sprintf("Chỉ tạo %d ảnh, chưa làm video. Không chèn thêm chữ, phụ đề hay logo.", n))

	return strings.Join(parts, "\n")
}

// BoardVideoPrompt generates the video command after storyboard images are already drawn.
func BoardVideoPrompt(items []SceneItem, cfg ScriptConfig, m int, inThread bool, hostLine string) string {
	threadNote := "vừa tạo (gửi kèm lại)"
	if !inThread {
		threadNote = "đính kèm"
	}
	prefix := fmt.Sprintf("Dùng đúng %d ảnh storyboard %s làm khung hình mở đầu của %d cảnh, theo thứ tự ảnh 1 đến %d. Giữ nguyên người, trang phục, bối cảnh và sản phẩm như trong ảnh.\n\n", m, threadNote, m, m)

	itemsNoImg := make([]SceneItem, len(items))
	for i, it := range items {
		itemsNoImg[i] = it
		itemsNoImg[i].Img = ""
	}
	return prefix + BuildPrompt(itemsNoImg, cfg, nil, hostLine)
}
