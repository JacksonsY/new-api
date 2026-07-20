package gemini

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// convertGeminiImageResponse 是 gemini 图片模型经 /v1/images/generations 出图的核心
// 契约:把 generateContent 的内联图转成 OpenAI Images 的 data[].b64_json。
func TestConvertGeminiImageResponse(t *testing.T) {
	t.Run("extracts inline image as b64_json and ignores text parts", func(t *testing.T) {
		resp := &dto.GeminiChatResponse{
			Candidates: []dto.GeminiChatCandidate{
				{Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{
					{Text: "here you go"},
					{InlineData: &dto.GeminiInlineData{MimeType: "image/png", Data: "AAAA"}},
				}}},
			},
		}
		out, err := convertGeminiImageResponse(resp)
		require.NoError(t, err)
		require.Len(t, out.Data, 1)
		assert.Equal(t, "AAAA", out.Data[0].B64Json)
		assert.Empty(t, out.Data[0].Url)
	})

	t.Run("collects multiple images across candidates in order", func(t *testing.T) {
		resp := &dto.GeminiChatResponse{
			Candidates: []dto.GeminiChatCandidate{
				{Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{
					{InlineData: &dto.GeminiInlineData{MimeType: "image/png", Data: "one"}},
				}}},
				{Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{
					{InlineData: &dto.GeminiInlineData{MimeType: "image/jpeg", Data: "two"}},
				}}},
			},
		}
		out, err := convertGeminiImageResponse(resp)
		require.NoError(t, err)
		require.Len(t, out.Data, 2)
		assert.Equal(t, "one", out.Data[0].B64Json)
		assert.Equal(t, "two", out.Data[1].B64Json)
	})

	t.Run("errors when no image part is present", func(t *testing.T) {
		resp := &dto.GeminiChatResponse{
			Candidates: []dto.GeminiChatCandidate{
				{Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{
					{Text: "text only"},
					{InlineData: &dto.GeminiInlineData{MimeType: "text/plain", Data: "notanimage"}},
				}}},
			},
		}
		_, err := convertGeminiImageResponse(resp)
		require.Error(t, err)
	})
}

// geminiImageAspectRatio 只映射安全的宽高比,不产出 imageSize(避免 gemini-2.5-flash-image 拒绝)。
func TestGeminiImageAspectRatio(t *testing.T) {
	cases := []struct {
		size string
		want string
	}{
		{"", ""},
		{"  ", ""},
		{"16:9", "16:9"},
		{"1024x1024", "1:1"},
		{"256x256", "1:1"},
		{"1792x1024", "16:9"},
		{"1024x1792", "9:16"},
		{"1536x1024", "3:2"},
		{"1024x1536", "2:3"},
		{"640x480", ""},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, geminiImageAspectRatio(tc.size), "size=%q", tc.size)
	}
}
