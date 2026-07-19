package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ModelExistsInAbilities 必须含 disabled 记录：某模型渠道被 auto-ban 后
// abilities.enabled 全置 false，此时仍应判定「模型存在」(→ 上游返回 503 可重试)，
// 而非「不存在」(→ 404 停止重试)。
func TestModelExistsInAbilitiesIncludesDisabled(t *testing.T) {
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
	})

	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "banned-model",
		ChannelId: 1,
		Enabled:   false, // 渠道被 auto-ban，能力禁用
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "live-model",
		ChannelId: 2,
		Enabled:   true,
	}).Error)

	// disabled 记录也算存在
	known, err := ModelExistsInAbilities("banned-model")
	require.NoError(t, err)
	assert.True(t, known, "全部渠道被禁用的模型仍应判定为存在")

	known, err = ModelExistsInAbilities("live-model")
	require.NoError(t, err)
	assert.True(t, known)

	// 完全无记录才算不存在
	known, err = ModelExistsInAbilities("never-offered-model")
	require.NoError(t, err)
	assert.False(t, known, "从未上架的模型应判定为不存在")

	known, err = ModelExistsInAbilities("")
	require.NoError(t, err)
	assert.False(t, known)
}
