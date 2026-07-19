package model

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GetModelContextLength 应从模型元数据取回上下文窗口，未标注模型返回 0。
func TestGetModelContextLengthFromMeta(t *testing.T) {
	resetPricingEndpointTestTables(t)

	insertPricingEndpointChannel(t, 301, constant.ChannelTypeOpenAI, dto.ChannelOtherSettings{})
	insertPricingEndpointAbility(t, 301, "gpt-ctx-model")
	insertPricingEndpointAbility(t, 301, "gpt-no-ctx-model")

	require.NoError(t, DB.Create(&Model{
		ModelName:     "gpt-ctx-model",
		Status:        1,
		ContextLength: 128000,
	}).Error)

	InvalidatePricingCache()
	// 触发一次定价重建，填充 modelContextLength。
	GetPricing()

	assert.Equal(t, 128000, GetModelContextLength("gpt-ctx-model"))
	assert.Equal(t, 0, GetModelContextLength("gpt-no-ctx-model"),
		"未标注上下文的模型应返回 0")
	assert.Equal(t, 0, GetModelContextLength(""),
		"空模型名应返回 0")
}
