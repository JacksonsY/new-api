package oaichat

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIChatRequestToClaudeMessagesOmitsAbsentToolRequired(t *testing.T) {
	maxTokens := uint(1024)
	request := dto.GeneralOpenAIRequest{
		Model:     "claude-3-5-sonnet-20241022",
		MaxTokens: &maxTokens,
		Messages: []dto.Message{
			{Role: "user", Content: "hello"},
		},
		Tools: []dto.ToolCallRequest{
			{
				Type: "function",
				Function: dto.FunctionRequest{
					Name:        "list_mcp_resources",
					Description: "Lists resources provided by MCP servers.",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"server": map[string]any{"type": "string"},
						},
						"additionalProperties": false,
						// 无 required 键：模拟 Codex 的 list_mcp_resources 工具。
					},
				},
			},
			{
				Type: "function",
				Function: dto.FunctionRequest{
					Name: "read_mcp_resource",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"server": map[string]any{"type": "string"},
							"uri":    map[string]any{"type": "string"},
						},
						"required": []any{"server", "uri"},
					},
				},
			},
		},
	}

	claudeRequest, err := OpenAIChatRequestToClaudeMessages(context.Background(), nil, request)
	require.NoError(t, err)

	toolList, ok := claudeRequest.Tools.([]any)
	require.True(t, ok)
	tools, webSearchTools := dto.ProcessTools(toolList)
	require.Empty(t, webSearchTools)
	require.Len(t, tools, 2)

	// 未声明 required 的工具不应序列化出该键（旧代码会写出 "required": null）。
	assert.NotContains(t, tools[0].InputSchema, "required")
	assert.Contains(t, tools[0].InputSchema, "properties")

	// 声明了 required 的工具应原样保留。
	assert.Equal(t, []any{"server", "uri"}, tools[1].InputSchema["required"])
}

func TestOpenAIChatRequestToClaudeMessagesNormalizesToolInputSchema(t *testing.T) {
	tests := []struct {
		name       string
		parameters any
		wantSchema map[string]any
	}{
		{
			name:       "omitted parameters",
			parameters: nil,
			wantSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			name: "missing type and properties",
			parameters: map[string]any{
				"additionalProperties": false,
			},
			wantSchema: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
		{
			name: "non-string type",
			parameters: map[string]any{
				"type":       123,
				"properties": map[string]any{},
			},
			wantSchema: map[string]any{
				"type":       123,
				"properties": map[string]any{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maxTokens := uint(1024)
			got, err := OpenAIChatRequestToClaudeMessages(context.Background(), nil, dto.GeneralOpenAIRequest{
				Model:     "claude-test",
				MaxTokens: &maxTokens,
				Messages: []dto.Message{
					{Role: "user", Content: "Call the tool."},
				},
				Tools: []dto.ToolCallRequest{
					{
						Type: "function",
						Function: dto.FunctionRequest{
							Name:        "get_current_time",
							Description: "Get the current time",
							Parameters:  tt.parameters,
						},
					},
				},
			})

			require.NoError(t, err)
			tools, ok := got.Tools.([]any)
			require.True(t, ok)
			require.Len(t, tools, 1)
			tool, ok := tools[0].(*dto.Tool)
			require.True(t, ok)
			assert.Equal(t, "get_current_time", tool.Name)
			assert.Equal(t, tt.wantSchema, tool.InputSchema)
		})
	}
}
