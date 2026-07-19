package gemini

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// maxVeoReferenceImages mirrors the Veo 3.1 limit on referenceImages.
const maxVeoReferenceImages = 3

type rawMessage []byte

func (m *rawMessage) UnmarshalJSON(data []byte) error {
	if m == nil {
		return fmt.Errorf("rawMessage: UnmarshalJSON on nil pointer")
	}
	*m = append((*m)[0:0], data...)
	return nil
}

type VeoMetadataFeatures struct {
	HasImage           bool
	HasLastFrame       bool
	HasReferenceImages bool
}

// ApplyVeoMetadataToInstance moves Veo instance-level fields from request
// metadata into the upstream instance. Supported metadata fields are:
// image/firstFrame/first_frame, lastFrame/last_frame, and
// referenceImages/reference_images.
func ApplyVeoMetadataToInstance(metadata map[string]any, instance *VeoInstance) (VeoMetadataFeatures, error) {
	var features VeoMetadataFeatures
	if metadata == nil || instance == nil {
		return features, nil
	}

	raw, err := rawMetadata(metadata)
	if err != nil {
		return features, err
	}

	imageRaw := firstRaw(raw["image"], raw["firstFrame"], raw["first_frame"])
	if hasRaw(imageRaw) {
		image, err := parseVeoImageMetadata(imageRaw)
		if err != nil {
			return features, fmt.Errorf("invalid metadata image: %w", err)
		}
		instance.Image = image
		features.HasImage = true
	}

	lastFrameRaw := firstRaw(raw["lastFrame"], raw["last_frame"])
	if hasRaw(lastFrameRaw) {
		lastFrame, err := parseVeoImageMetadata(lastFrameRaw)
		if err != nil {
			return features, fmt.Errorf("invalid metadata lastFrame: %w", err)
		}
		instance.LastFrame = lastFrame
		features.HasLastFrame = true
	}

	referenceImagesRaw := firstRaw(raw["referenceImages"], raw["reference_images"])
	if hasRaw(referenceImagesRaw) {
		referenceImages, err := parseVeoReferenceImagesMetadata(referenceImagesRaw)
		if err != nil {
			return features, fmt.Errorf("invalid metadata referenceImages: %w", err)
		}
		instance.ReferenceImages = referenceImages
		features.HasReferenceImages = len(referenceImages) > 0
	}

	// Veo 的三种输入模式互斥（官方 schema）：referenceImages 一旦提供就不允许
	// 再带 image/lastFrame；lastFrame 只能作为 image 的配对尾帧存在。按 instance
	// 的最终状态判断而非只看 metadata——image 也可能来自 multipart 上传。
	if len(instance.ReferenceImages) > 0 && (instance.Image != nil || instance.LastFrame != nil) {
		return features, fmt.Errorf("referenceImages cannot be combined with image or lastFrame")
	}
	if instance.LastFrame != nil && instance.Image == nil {
		return features, fmt.Errorf("lastFrame requires image as the first frame")
	}

	return features, nil
}

// veoImageMimeType 归一化图片 MIME 类型。Veo 只接受 image/jpeg 与 image/png，
// 声明缺失或不是图片类型时从 base64 内容嗅探；发 application/octet-stream 或
// text/plain 这类值只会换来一次必然失败的上游往返。
func veoImageMimeType(declared, base64Data string) string {
	if t := strings.TrimSpace(declared); strings.HasPrefix(t, "image/") {
		return t
	}
	if raw, err := base64.StdEncoding.DecodeString(base64Data); err == nil && len(raw) > 0 {
		if detected := http.DetectContentType(raw); strings.HasPrefix(detected, "image/") {
			return detected
		}
	}
	return "image/png"
}

func rawMetadata(metadata map[string]any) (map[string]rawMessage, error) {
	data, err := common.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal metadata failed: %w", err)
	}
	var raw map[string]rawMessage
	if err := common.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal metadata failed: %w", err)
	}
	return raw, nil
}

func firstRaw(values ...rawMessage) rawMessage {
	for _, value := range values {
		if hasRaw(value) {
			return value
		}
	}
	return nil
}

func hasRaw(raw rawMessage) bool {
	trimmed := bytes.TrimSpace([]byte(raw))
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func parseVeoImageMetadata(raw rawMessage) (*VeoImageInput, error) {
	trimmed := bytes.TrimSpace([]byte(raw))
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty image")
	}

	if trimmed[0] == '"' {
		var imageStr string
		if err := common.Unmarshal(trimmed, &imageStr); err != nil {
			return nil, err
		}
		imageStr = strings.TrimSpace(imageStr)
		if strings.HasPrefix(imageStr, "{") {
			return parseVeoImageMetadata(rawMessage(imageStr))
		}
		if image := ParseImageInput(imageStr); image != nil {
			return image, nil
		}
		return nil, fmt.Errorf("image string must be a data URI or base64 image")
	}

	var wire struct {
		InlineData         *VeoInlineData `json:"inlineData"`
		BytesBase64Encoded string         `json:"bytesBase64Encoded"`
		MimeType           string         `json:"mimeType"`
		Data               string         `json:"data"`
	}
	if err := common.Unmarshal(trimmed, &wire); err != nil {
		return nil, err
	}
	if wire.InlineData != nil {
		if wire.InlineData.Data == "" {
			return nil, fmt.Errorf("inlineData.data is required")
		}
		return NewVeoImageInput(wire.InlineData.Data, veoImageMimeType(wire.InlineData.MimeType, wire.InlineData.Data)), nil
	}
	if wire.BytesBase64Encoded != "" {
		return NewVeoImageInput(wire.BytesBase64Encoded, veoImageMimeType(wire.MimeType, wire.BytesBase64Encoded)), nil
	}
	if wire.Data != "" {
		return NewVeoImageInput(wire.Data, veoImageMimeType(wire.MimeType, wire.Data)), nil
	}
	return nil, fmt.Errorf("image.inlineData is required")
}

func parseVeoReferenceImagesMetadata(raw rawMessage) ([]VeoReferenceImage, error) {
	trimmed := bytes.TrimSpace([]byte(raw))
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] == '"' {
		var rawStr string
		if err := common.Unmarshal(trimmed, &rawStr); err != nil {
			return nil, err
		}
		rawStr = strings.TrimSpace(rawStr)
		if !strings.HasPrefix(rawStr, "[") {
			return nil, fmt.Errorf("referenceImages string must contain a JSON array")
		}
		trimmed = []byte(rawStr)
	}

	var items []rawMessage
	if err := common.Unmarshal(trimmed, &items); err != nil {
		return nil, err
	}
	// Veo accepts at most 3 reference images. Reject oversized arrays here so a
	// request carrying hundreds of base64 payloads never reaches the upstream.
	if len(items) > maxVeoReferenceImages {
		return nil, fmt.Errorf("at most %d reference images are supported, got %d", maxVeoReferenceImages, len(items))
	}

	out := make([]VeoReferenceImage, 0, len(items))
	for i, itemRaw := range items {
		imageRaw, referenceType, err := parseVeoReferenceImageItem(itemRaw)
		if err != nil {
			return nil, fmt.Errorf("referenceImages[%d]: %w", i, err)
		}
		image, err := parseVeoImageMetadata(imageRaw)
		if err != nil {
			return nil, fmt.Errorf("referenceImages[%d].image: %w", i, err)
		}
		out = append(out, VeoReferenceImage{
			Image:         image,
			ReferenceType: referenceType,
		})
	}
	return out, nil
}

func parseVeoReferenceImageItem(raw rawMessage) (rawMessage, string, error) {
	trimmed := bytes.TrimSpace([]byte(raw))
	if len(trimmed) == 0 {
		return nil, "", fmt.Errorf("empty reference image")
	}
	if trimmed[0] == '"' {
		return raw, "asset", nil
	}

	var item struct {
		Image              rawMessage `json:"image"`
		ReferenceType      string     `json:"referenceType"`
		ReferenceTypeSnake string     `json:"reference_type"`
	}
	if err := common.Unmarshal(trimmed, &item); err != nil {
		return nil, "", err
	}
	if hasRaw(item.Image) {
		referenceType := item.ReferenceType
		if referenceType == "" {
			referenceType = item.ReferenceTypeSnake
		}
		return item.Image, referenceType, nil
	}

	// Allow a bare image object as a shorthand reference image.
	return raw, "asset", nil
}
