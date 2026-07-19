package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterPricingByUsableGroupsTrimsEnableGroups(t *testing.T) {
	pricing := []model.Pricing{
		{ModelName: "model-mixed", EnableGroup: []string{"default", "internal"}},
		{ModelName: "model-internal-only", EnableGroup: []string{"internal"}},
		{ModelName: "model-all", EnableGroup: []string{"all", "internal"}},
	}
	usableGroup := map[string]string{"default": "default group"}

	filtered := filterPricingByUsableGroups(pricing, usableGroup)

	require.Len(t, filtered, 2)

	assert.Equal(t, "model-mixed", filtered[0].ModelName)
	assert.Equal(t, []string{"default"}, filtered[0].EnableGroup,
		"groups outside the user's usable groups must not be exposed")

	// "all" 条目对所有用户可见，但随行的内部分组名(internal)用户无权使用，必须裁掉。
	assert.Equal(t, "model-all", filtered[1].ModelName)
	assert.Equal(t, []string{"all"}, filtered[1].EnableGroup,
		"entries enabled for all must not leak accompanying unusable internal group names")

	assert.Equal(t, []string{"default", "internal"}, pricing[0].EnableGroup,
		"the shared pricing cache must stay untouched")
}

// 用户确实可用某个随行分组时，"all" 之外仍保留该可用分组名。
func TestFilterPricingByUsableGroupsKeepsUsableGroupAlongsideAll(t *testing.T) {
	pricing := []model.Pricing{
		{ModelName: "model-all-team", EnableGroup: []string{"all", "team-x", "agent-vip"}},
	}
	usableGroup := map[string]string{"team-x": "team x"}

	filtered := filterPricingByUsableGroups(pricing, usableGroup)

	require.Len(t, filtered, 1)
	assert.Equal(t, []string{"all", "team-x"}, filtered[0].EnableGroup,
		"keeps 'all' plus the user's own usable group, strips other internal names")
}

func TestFilterPricingByUsableGroupsEmptyInputs(t *testing.T) {
	pricing := []model.Pricing{
		{ModelName: "model-a", EnableGroup: []string{"default"}},
	}

	assert.Empty(t, filterPricingByUsableGroups(pricing, map[string]string{}))
	assert.Empty(t, filterPricingByUsableGroups(nil, map[string]string{"default": ""}))
}
