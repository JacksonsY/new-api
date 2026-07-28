package dto_test

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesResponseCreatedAtAcceptsFloat(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		payload  string
		expected int64
	}{
		{
			name:     "float with zero fraction",
			payload:  `{"id":"resp_1","object":"response","created_at":1783476848.0}`,
			expected: 1783476848,
		},
		{
			name:     "float with fraction",
			payload:  `{"id":"resp_1","object":"response","created_at":1783476848.2598}`,
			expected: 1783476848,
		},
		{
			name:     "string float",
			payload:  `{"id":"resp_1","object":"response","created_at":"1783476848.2598"}`,
			expected: 1783476848,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var resp dto.OpenAIResponsesResponse
			err := common.UnmarshalJsonStr(tc.payload, &resp)
			require.NoError(t, err)
			assert.Equal(t, dto.UnixTimestamp(tc.expected), resp.CreatedAt)
		})
	}
}

func TestOpenAIResponsesCompactionCreatedAtAcceptsFloat(t *testing.T) {
	t.Parallel()

	payload := `{"id":"resp_1","object":"response","created_at":1783476848.2598}`
	var resp dto.OpenAIResponsesCompactionResponse
	err := common.UnmarshalJsonStr(payload, &resp)
	require.NoError(t, err)
	assert.Equal(t, dto.UnixTimestamp(1783476848), resp.CreatedAt)
}

func TestOpenAIResponsesResponseCreatedAtRejectsInvalidString(t *testing.T) {
	t.Parallel()

	payload := `{"id":"resp_1","object":"response","created_at":"not-a-number"}`
	var resp dto.OpenAIResponsesResponse
	err := common.UnmarshalJsonStr(payload, &resp)
	require.Error(t, err)
}
