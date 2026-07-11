package service

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostConsumeQuotaConcurrentWalletDebitDoesNotOverdraw(t *testing.T) {
	truncate(t)
	user := &model.User{
		Username: "quota-concurrency-user",
		Status:   common.UserStatusEnabled,
		Quota:    100,
	}
	require.NoError(t, model.DB.Create(user).Error)

	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			results <- PostConsumeQuota(&relaycommon.RelayInfo{
				UserId:       user.Id,
				IsPlayground: true,
			}, 100, 0, false)
		}()
	}
	close(start)
	workers.Wait()
	close(results)

	successes := 0
	failures := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			failures++
		}
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, failures)

	var stored model.User
	require.NoError(t, model.DB.First(&stored, user.Id).Error)
	assert.Zero(t, stored.Quota)
}

func TestPostConsumeQuotaConcurrentTokenDebitDoesNotOverdraw(t *testing.T) {
	truncate(t)
	user := &model.User{
		Username: "token-concurrency-user",
		Status:   common.UserStatusEnabled,
		Quota:    200,
	}
	require.NoError(t, model.DB.Create(user).Error)
	token := &model.Token{
		UserId:      user.Id,
		Key:         "token-concurrency-key",
		Name:        "token concurrency",
		Status:      common.TokenStatusEnabled,
		RemainQuota: 100,
	}
	require.NoError(t, model.DB.Create(token).Error)

	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			results <- PostConsumeQuota(&relaycommon.RelayInfo{
				UserId:   user.Id,
				TokenId:  token.Id,
				TokenKey: token.Key,
			}, 100, 0, false)
		}()
	}
	close(start)
	workers.Wait()
	close(results)

	successes := 0
	failures := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			failures++
			assert.ErrorIs(t, err, model.ErrInsufficientTokenQuota)
		}
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, failures)

	var storedUser model.User
	require.NoError(t, model.DB.First(&storedUser, user.Id).Error)
	assert.Equal(t, 100, storedUser.Quota)
	var storedToken model.Token
	require.NoError(t, model.DB.First(&storedToken, token.Id).Error)
	assert.Zero(t, storedToken.RemainQuota)
	assert.Equal(t, 100, storedToken.UsedQuota)
}
