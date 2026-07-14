package ratio_setting

import "sync"

// VideoResolutionRatioTier 描述某视频模型在一个输出分辨率档下、相对基准价的倍率。
// 由各视频适配器在包 init 时注册（源头是适配器自己的价目表），定价展示据此计算
// 「基准价(ModelPrice) × 倍率」得到每档的每秒价——这样管理员调整 ModelPrice 时，
// 各分辨率展示价会等比例跟随，不会与实际计费脱节。
type VideoResolutionRatioTier struct {
	Resolution   string  // 480p / 720p / 1080p / 4k
	NoRefRatio   float64 // 无参考视频（纯生成）相对基准价的倍率
	WithRefRatio float64 // 带参考视频（视频编辑）相对基准价的倍率
	HasWithRef   bool    // 该分辨率是否提供「带参考视频」计价
}

var (
	videoResolutionRatiosMu sync.RWMutex
	videoResolutionRatios   = map[string][]VideoResolutionRatioTier{}
)

// RegisterVideoResolutionRatios 注册某模型的分辨率倍率档（覆盖式）。适配器 init 调用。
func RegisterVideoResolutionRatios(model string, tiers []VideoResolutionRatioTier) {
	videoResolutionRatiosMu.Lock()
	defer videoResolutionRatiosMu.Unlock()
	if len(tiers) == 0 {
		delete(videoResolutionRatios, model)
		return
	}
	videoResolutionRatios[model] = tiers
}

// GetVideoResolutionRatios 返回某模型的分辨率倍率档；第二个返回值表示是否已注册。
func GetVideoResolutionRatios(model string) ([]VideoResolutionRatioTier, bool) {
	videoResolutionRatiosMu.RLock()
	defer videoResolutionRatiosMu.RUnlock()
	tiers, ok := videoResolutionRatios[model]
	return tiers, ok
}
