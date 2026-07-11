package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type midjourneyBillingFixture struct {
	ctx       *gin.Context
	info      *relaycommon.RelayInfo
	task      *model.Midjourney
	priceData types.PriceData
	agentID   int
}

func newMidjourneyBillingFixture(t *testing.T) midjourneyBillingFixture {
	t.Helper()
	truncate(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Midjourney{}, &model.Commission{}))
	require.NoError(t, model.DB.Exec("DELETE FROM midjourneys").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM commissions").Error)
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM midjourneys")
		model.DB.Exec("DELETE FROM commissions")
	})

	paymentSetting := operation_setting.GetPaymentSetting()
	originalCompliance := paymentSetting.ComplianceConfirmed
	originalTermsVersion := paymentSetting.ComplianceTermsVersion
	originalMaturity := common.AgentCommissionMatureMinutes
	originalMinAge := common.AgentInviteeMinAgeDays
	originalLogConsume := common.LogConsumeEnabled
	originalDataExport := common.DataExportEnabled
	originalMemoryCache := common.MemoryCacheEnabled
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	common.AgentCommissionMatureMinutes = 0
	common.AgentInviteeMinAgeDays = 0
	common.LogConsumeEnabled = true
	common.DataExportEnabled = false
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalCompliance
		paymentSetting.ComplianceTermsVersion = originalTermsVersion
		common.AgentCommissionMatureMinutes = originalMaturity
		common.AgentInviteeMinAgeDays = originalMinAge
		common.LogConsumeEnabled = originalLogConsume
		common.DataExportEnabled = originalDataExport
		common.MemoryCacheEnabled = originalMemoryCache
	})

	agent := &model.User{
		Username:        "mj-bill-agent",
		AffCode:         "mj_bill_agent",
		Status:          common.UserStatusEnabled,
		AgentType:       "normal",
		UsageProfitRate: 0.2,
	}
	require.NoError(t, model.DB.Create(agent).Error)
	user := &model.User{
		Username:     "mj-bill-user",
		AffCode:      "mj_bill_user",
		Status:       common.UserStatusEnabled,
		InviterId:    agent.Id,
		Quota:        1000,
		UsedQuota:    17,
		RequestCount: 3,
	}
	require.NoError(t, model.DB.Create(user).Error)
	channel := &model.Channel{
		Name:      "mj-bill-channel",
		Key:       "sk-mj-channel",
		Status:    common.ChannelStatusEnabled,
		UsedQuota: 23,
	}
	require.NoError(t, model.DB.Create(channel).Error)
	token := &model.Token{
		UserId:      user.Id,
		Key:         "mj-bill-token",
		Name:        "mj billing token",
		Status:      common.TokenStatusEnabled,
		RemainQuota: 500,
		UsedQuota:   11,
	}
	require.NoError(t, model.DB.Create(token).Error)
	task := &model.Midjourney{
		UserId:        user.Id,
		Code:          1,
		Action:        "IMAGINE",
		MjId:          "mj-billing-job",
		ChannelId:     channel.Id,
		Quota:         100,
		BillingStatus: model.MidjourneyBillingPending,
		Progress:      "0%",
	}
	require.NoError(t, task.Insert())

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/mj/submit/imagine", nil)
	ctx.Set("username", user.Username)
	ctx.Set("token_name", token.Name)
	ctx.Set(common.RequestIdKey, "mj-billing-request")

	return midjourneyBillingFixture{
		ctx: ctx,
		info: &relaycommon.RelayInfo{
			UserId:         user.Id,
			TokenId:        token.Id,
			TokenKey:       token.Key,
			UsingGroup:     "default",
			RequestURLPath: "/mj/submit/imagine",
			ChannelMeta:    &relaycommon.ChannelMeta{ChannelId: channel.Id},
		},
		task: task,
		priceData: types.PriceData{
			Quota:          100,
			ModelPrice:     0.01,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
		agentID: agent.Id,
	}
}

func TestCommitMidjourneyTaskBillingSkipsBookkeepingWhenTokenDebitFails(t *testing.T) {
	fixture := newMidjourneyBillingFixture(t)
	originalBatchUpdate := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = true
	t.Cleanup(func() {
		common.BatchUpdateEnabled = originalBatchUpdate
	})

	injectedErr := errors.New("injected token debit failure")
	const callbackName = "test:fail_midjourney_token_debit"
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "tokens" {
			tx.AddError(injectedErr)
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, model.DB.Callback().Update().Remove(callbackName))
	})

	err := CommitMidjourneyTaskBilling(
		fixture.ctx,
		fixture.info,
		fixture.task,
		fixture.priceData,
		"mj_imagine",
		"midjourney billing test",
		model.BuildTaskCommissionSourceKey(
			fixture.info.ChannelId, fixture.task.MjId, "initial", fixture.priceData.Quota,
		),
	)
	require.ErrorIs(t, err, injectedErr)

	var user model.User
	require.NoError(t, model.DB.First(&user, fixture.info.UserId).Error)
	assert.Equal(t, 1000, user.Quota, "the wallet debit must be compensated")
	assert.Equal(t, 17, user.UsedQuota)
	assert.Equal(t, 3, user.RequestCount)

	var token model.Token
	require.NoError(t, model.DB.First(&token, fixture.info.TokenId).Error)
	assert.Equal(t, 500, token.RemainQuota)
	assert.Equal(t, 11, token.UsedQuota)

	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, fixture.info.ChannelId).Error)
	assert.Equal(t, int64(23), channel.UsedQuota)

	var task model.Midjourney
	require.NoError(t, model.DB.First(&task, fixture.task.Id).Error)
	assert.Zero(t, task.Quota, "an uncommitted task must never become refundable")
	assert.Equal(t, model.MidjourneyBillingReady, task.BillingStatus)

	var commissionCount int64
	require.NoError(t, model.DB.Model(&model.Commission{}).
		Where("from_user_id = ?", fixture.info.UserId).
		Count(&commissionCount).Error)
	assert.Zero(t, commissionCount)

	var agent model.User
	require.NoError(t, model.DB.First(&agent, fixture.agentID).Error)
	assert.Zero(t, agent.CommissionQuota)
	assert.Zero(t, agent.CommissionHistoryQuota)

	var logCount int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).
		Where("user_id = ? AND type = ?", fixture.info.UserId, model.LogTypeConsume).
		Count(&logCount).Error)
	assert.Zero(t, logCount)
}

func TestCommitMidjourneyTaskBillingRollsBackSubscriptionWhenTokenDebitFails(t *testing.T) {
	fixture := newMidjourneyBillingFixture(t)
	subscription := &model.UserSubscription{
		UserId:      fixture.info.UserId,
		AmountTotal: 1000,
		AmountUsed:  200,
		Status:      "active",
	}
	require.NoError(t, model.DB.Create(subscription).Error)
	fixture.info.BillingSource = BillingSourceSubscription
	fixture.info.SubscriptionId = subscription.Id

	injectedErr := errors.New("injected subscription token debit failure")
	const callbackName = "test:fail_midjourney_subscription_token_debit"
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "tokens" {
			tx.AddError(injectedErr)
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, model.DB.Callback().Update().Remove(callbackName))
	})

	err := CommitMidjourneyTaskBilling(
		fixture.ctx,
		fixture.info,
		fixture.task,
		fixture.priceData,
		"mj_imagine",
		"midjourney subscription billing test",
		model.BuildTaskCommissionSourceKey(
			fixture.task.ChannelId, fixture.task.MjId, "initial", fixture.priceData.Quota,
		),
	)
	require.ErrorIs(t, err, injectedErr)

	var storedSubscription model.UserSubscription
	require.NoError(t, model.DB.First(&storedSubscription, subscription.Id).Error)
	assert.Equal(t, int64(200), storedSubscription.AmountUsed)
	assert.Zero(t, fixture.info.SubscriptionPostDelta)

	var user model.User
	require.NoError(t, model.DB.First(&user, fixture.info.UserId).Error)
	assert.Equal(t, 1000, user.Quota)

	var task model.Midjourney
	require.NoError(t, model.DB.First(&task, fixture.task.Id).Error)
	assert.Zero(t, task.Quota)
	assert.Equal(t, model.MidjourneyBillingReady, task.BillingStatus)
}

func TestCommitMidjourneyTaskBillingKeepsRefundableQuotaWhenCompensationFails(t *testing.T) {
	fixture := newMidjourneyBillingFixture(t)

	tokenErr := errors.New("injected token debit failure")
	rollbackErr := errors.New("injected wallet compensation failure")
	var userUpdates atomic.Int32
	const callbackName = "test:fail_midjourney_wallet_compensation"
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		switch tx.Statement.Table {
		case "tokens":
			tx.AddError(tokenErr)
		case "users":
			if userUpdates.Add(1) == 2 {
				tx.AddError(rollbackErr)
			}
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, model.DB.Callback().Update().Remove(callbackName))
	})

	err := CommitMidjourneyTaskBilling(
		fixture.ctx,
		fixture.info,
		fixture.task,
		fixture.priceData,
		"mj_imagine",
		"midjourney compensation failure test",
		model.BuildTaskCommissionSourceKey(
			fixture.task.ChannelId, fixture.task.MjId, "initial", fixture.priceData.Quota,
		),
	)
	require.ErrorIs(t, err, tokenErr)
	assert.ErrorIs(t, err, rollbackErr)

	var user model.User
	require.NoError(t, model.DB.First(&user, fixture.info.UserId).Error)
	assert.Equal(t, 900, user.Quota, "the failed compensation leaves the wallet charged")

	var task model.Midjourney
	require.NoError(t, model.DB.First(&task, fixture.task.Id).Error)
	assert.Equal(t, 100, task.Quota, "a possibly charged task must remain refundable")
	assert.Equal(t, model.MidjourneyBillingReady, task.BillingStatus)
}

func TestCommitMidjourneyTaskBillingRecordsBookkeepingAfterSuccessfulDebit(t *testing.T) {
	fixture := newMidjourneyBillingFixture(t)
	fixture.info.ChannelMeta.ChannelId = fixture.task.ChannelId + 1000
	assert.Empty(t, model.GetAllUnFinishTasks(), "pending billing must not be visible to the poller")
	assert.False(t, model.HasUnfinishedMidjourneyTasks())

	err := CommitMidjourneyTaskBilling(
		fixture.ctx,
		fixture.info,
		fixture.task,
		fixture.priceData,
		"mj_imagine",
		"midjourney billing test",
		model.BuildTaskCommissionSourceKey(
			fixture.task.ChannelId, fixture.task.MjId, "initial", fixture.priceData.Quota,
		),
	)
	require.NoError(t, err)

	var user model.User
	require.NoError(t, model.DB.First(&user, fixture.info.UserId).Error)
	assert.Equal(t, 900, user.Quota)
	assert.Equal(t, 117, user.UsedQuota)
	assert.Equal(t, 4, user.RequestCount)

	var token model.Token
	require.NoError(t, model.DB.First(&token, fixture.info.TokenId).Error)
	assert.Equal(t, 400, token.RemainQuota)
	assert.Equal(t, 111, token.UsedQuota)

	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, fixture.task.ChannelId).Error)
	assert.Equal(t, int64(123), channel.UsedQuota)

	var task model.Midjourney
	require.NoError(t, model.DB.First(&task, fixture.task.Id).Error)
	assert.Equal(t, 100, task.Quota)
	assert.Equal(t, model.MidjourneyBillingReady, task.BillingStatus)
	assert.Len(t, model.GetAllUnFinishTasks(), 1)
	assert.True(t, model.HasUnfinishedMidjourneyTasks())

	var commission model.Commission
	require.NoError(t, model.DB.Where("from_user_id = ?", fixture.info.UserId).First(&commission).Error)
	assert.Equal(t, fixture.agentID, commission.AgentId)
	assert.Equal(t, 20, commission.Quota)

	var log model.Log
	require.NoError(t, model.LOG_DB.
		Where("user_id = ? AND type = ?", fixture.info.UserId, model.LogTypeConsume).
		First(&log).Error)
	assert.Equal(t, 100, log.Quota)
}

func TestFinishMidjourneyBillingRetriesTransientPublicationFailure(t *testing.T) {
	fixture := newMidjourneyBillingFixture(t)

	injectedErr := errors.New("transient midjourney billing publication failure")
	var attempts atomic.Int32
	const callbackName = "test:retry_midjourney_billing_publication"
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "midjourneys" && attempts.Add(1) <= 2 {
			tx.AddError(injectedErr)
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, model.DB.Callback().Update().Remove(callbackName))
	})

	require.NoError(t, fixture.task.FinishBilling(fixture.priceData.Quota))
	assert.Equal(t, int32(3), attempts.Load())

	var task model.Midjourney
	require.NoError(t, model.DB.First(&task, fixture.task.Id).Error)
	assert.Equal(t, model.MidjourneyBillingReady, task.BillingStatus)
	assert.Equal(t, fixture.priceData.Quota, task.Quota)
}
