package controller

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/oauth"

	"github.com/stretchr/testify/assert"
)

// GitHub 账号年龄门禁:仅 GitHub 生效,开启门槛后拿不到/太新的账号被拒,够老的放行。
func TestValidateOAuthAccountAgeForNewAssociation(t *testing.T) {
	original := common.GitHubMinimumAccountAgeSeconds
	t.Cleanup(func() { common.GitHubMinimumAccountAgeSeconds = original })

	github := &oauth.GitHubProvider{}
	now := time.Now()

	// 非 GitHub provider:即便门槛开着也永远放行
	common.GitHubMinimumAccountAgeSeconds = 30 * 86400
	assert.NoError(t, validateOAuthAccountAgeForNewAssociation(&oauth.DiscordProvider{}, &oauth.OAuthUser{}, now),
		"非 GitHub provider 不受门禁约束")

	// 门槛关闭(<=0):放行
	common.GitHubMinimumAccountAgeSeconds = 0
	assert.NoError(t, validateOAuthAccountAgeForNewAssociation(github, &oauth.OAuthUser{}, now), "门槛关闭时放行")

	// 门槛开启:30 天
	common.GitHubMinimumAccountAgeSeconds = 30 * 86400

	// 拿不到创建时间 → 拒绝(保守)
	assert.Error(t, validateOAuthAccountAgeForNewAssociation(github, &oauth.OAuthUser{AccountCreatedAt: nil}, now),
		"创建时间缺失时拒绝")

	// 账号太新(10 天 <= 30 天)→ 拒绝
	young := now.Add(-10 * 24 * time.Hour)
	assert.Error(t, validateOAuthAccountAgeForNewAssociation(github, &oauth.OAuthUser{AccountCreatedAt: &young}, now),
		"账号年龄不足时拒绝")

	// 账号够老(60 天 > 30 天)→ 放行
	old := now.Add(-60 * 24 * time.Hour)
	assert.NoError(t, validateOAuthAccountAgeForNewAssociation(github, &oauth.OAuthUser{AccountCreatedAt: &old}, now),
		"账号年龄达标时放行")
}
