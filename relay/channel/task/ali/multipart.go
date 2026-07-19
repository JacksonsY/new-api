package ali

import (
	"strings"

	"github.com/gin-gonic/gin"
)

func newFormatDefaultResolution(model string) (string, bool) {
	switch {
	case strings.HasPrefix(model, "wan2.7"):
		return "720P", true
	case strings.HasPrefix(model, "happyhorse"):
		return "1080P", true
	}
	return "", false
}

func isNewFormatModel(model string) bool {
	_, ok := newFormatDefaultResolution(model)
	return ok
}

// isVideoEditModel 同时匹配 wan2.7-videoedit（无连字符）与 happyhorse-1.0-video-edit（有连字符）。
func isVideoEditModel(model string) bool {
	return strings.Contains(model, "videoedit") || strings.Contains(model, "video-edit")
}

// isKeyFrameModel 标记走旧版扁平字段、且真正支持首尾帧生视频的模型。其余旧版
// 图生视频模型只认 img_url,给它们发 first_frame_url/last_frame_url 会被上游拒绝。
func isKeyFrameModel(model string) bool {
	return strings.Contains(model, "kf2v")
}

// isReferenceMediaModel 区分两类新协议模型:参考生视频与视频编辑把多张图当作
// 同类参考素材,图生视频则要按首帧/尾帧分型。
func isReferenceMediaModel(model string) bool {
	return strings.Contains(model, "r2v") || isVideoEditModel(model)
}

func imageMediaType(model string) string {
	if isReferenceMediaModel(model) {
		return "reference_image"
	}
	return "first_frame"
}

func appendImageURLsAsMedia(aliReq *AliVideoRequest, mediaType string, urls []string) {
	for _, u := range urls {
		aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{
			Type: mediaType,
			URL:  u,
		})
	}
}

// appendTaskImagesAsMedia 为新协议(input.media)模型构建媒体数组。参考类模型的多张
// 图同型追加;图生视频则首张为 first_frame、次张为 last_frame——与 wan2.7-i2v 的
// normalizeWan27I2VInput 一致,否则两张图会被打成同一型,首尾帧语义丢失。
func appendTaskImagesAsMedia(aliReq *AliVideoRequest, images []string) {
	if isReferenceMediaModel(aliReq.Model) {
		appendImageURLsAsMedia(aliReq, "reference_image", images)
		return
	}
	frameTypes := []string{"first_frame", "last_frame"}
	for i, u := range images {
		if i >= len(frameTypes) {
			break
		}
		aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{
			Type: frameTypes[i],
			URL:  u,
		})
	}
}

type mediaFieldDef struct {
	fieldName   string
	mediaTypeFn func(model string) string
}

var mediaFields = []mediaFieldDef{
	{
		fieldName:   "image_url",
		mediaTypeFn: imageMediaType,
	},
	{
		fieldName: "video_url",
		mediaTypeFn: func(model string) string {
			if isVideoEditModel(model) {
				return "video"
			}
			return "reference_video"
		},
	},
	{
		fieldName: "audio_url",
		mediaTypeFn: func(_ string) string {
			return "driving_audio"
		},
	},
}

func appendMultipartMediaToRequest(c *gin.Context, aliReq *AliVideoRequest) {
	if !isNewFormatModel(aliReq.Model) {
		return
	}
	for _, mf := range mediaFields {
		urls := c.PostFormArray(mf.fieldName)
		if len(urls) == 0 {
			continue
		}
		appendImageURLsAsMedia(aliReq, mf.mediaTypeFn(aliReq.Model), urls)
	}
}
