package model

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPendingEpayClaimsArePreviewableAndRecoverAfterLease(t *testing.T) {
	now := time.Now()
	provider := "epay-page-" + common.GetUUID()[:8]
	topUps := make([]*TopUp, 0, 3)
	orders := make([]*SubscriptionOrder, 0, 3)
	for i := 0; i < 3; i++ {
		topUp := &TopUp{
			UserId: 1, TradeNo: fmt.Sprintf("PAGE-TOPUP-%s-%d", common.GetUUID()[:8], i),
			PaymentProvider: provider, CreateTime: now.Unix() - 120, Status: common.TopUpStatusPending,
		}
		require.NoError(t, DB.Create(topUp).Error)
		topUps = append(topUps, topUp)
		order := &SubscriptionOrder{
			UserId: 1, PlanId: 1, TradeNo: fmt.Sprintf("PAGE-SUB-%s-%d", common.GetUUID()[:8], i),
			PaymentProvider: provider, CreateTime: now.Unix() - 120, Status: common.TopUpStatusPending,
		}
		require.NoError(t, DB.Create(order).Error)
		orders = append(orders, order)
	}
	t.Cleanup(func() {
		for _, topUp := range topUps {
			DB.Unscoped().Delete(&TopUp{}, topUp.Id)
		}
		for _, order := range orders {
			DB.Unscoped().Delete(&SubscriptionOrder{}, order.Id)
		}
	})

	previewTopUps, err := selectRecentPendingTopUps(DB, provider, 60, 600, 2, now, false, nil)
	require.NoError(t, err)
	require.Len(t, previewTopUps, 2)
	firstTopUps, err := selectRecentPendingTopUps(DB, provider, 60, 600, 2, now, true, nil)
	require.NoError(t, err)
	require.Len(t, firstTopUps, 2)
	assert.Equal(t, []int{previewTopUps[0].Id, previewTopUps[1].Id}, []int{firstTopUps[0].Id, firstTopUps[1].Id}, "preview must not advance the real queue")
	nextTopUps, err := selectRecentPendingTopUps(DB, provider, 60, 600, 2, now.Add(time.Nanosecond), true, nil)
	require.NoError(t, err)
	require.Len(t, nextTopUps, 1)
	assert.Equal(t, topUps[2].Id, nextTopUps[0].Id, "unscanned topup must move ahead of the previous unpaid page")
	blockedTopUps, err := selectRecentPendingTopUps(DB, provider, 60, 600, 2, now.Add(2*time.Nanosecond), true, nil)
	require.NoError(t, err)
	assert.Empty(t, blockedTopUps, "active leases must suppress duplicate claims")
	recoveredTopUps, err := selectRecentPendingTopUps(DB, provider, 60, 600, 2, now.Add(epayReconcileClaimLease), true, nil)
	require.NoError(t, err)
	require.Len(t, recoveredTopUps, 2, "crashed claims must become eligible when the lease expires")
	nextPassTopUps, err := selectRecentPendingTopUps(
		DB, provider, 60, 600, 1, now.Add(epayReconcileClaimLease+time.Nanosecond), true,
		[]int{recoveredTopUps[0].Id, recoveredTopUps[1].Id},
	)
	require.NoError(t, err)
	require.Len(t, nextPassTopUps, 1)
	assert.Equal(t, topUps[2].Id, nextPassTopUps[0].Id, "one pass must not reclaim an earlier row after its short lease expires")

	previewOrders, err := selectRecentPendingSubscriptionOrders(DB, provider, 60, 600, 2, now, false, nil)
	require.NoError(t, err)
	require.Len(t, previewOrders, 2)
	firstOrders, err := selectRecentPendingSubscriptionOrders(DB, provider, 60, 600, 2, now, true, nil)
	require.NoError(t, err)
	require.Len(t, firstOrders, 2)
	assert.Equal(t, []int{previewOrders[0].Id, previewOrders[1].Id}, []int{firstOrders[0].Id, firstOrders[1].Id}, "preview must not advance the real queue")
	nextOrders, err := selectRecentPendingSubscriptionOrders(DB, provider, 60, 600, 2, now.Add(time.Nanosecond), true, nil)
	require.NoError(t, err)
	require.Len(t, nextOrders, 1)
	assert.Equal(t, orders[2].Id, nextOrders[0].Id, "unscanned subscription must move ahead of the previous unpaid page")
	blockedOrders, err := selectRecentPendingSubscriptionOrders(DB, provider, 60, 600, 2, now.Add(2*time.Nanosecond), true, nil)
	require.NoError(t, err)
	assert.Empty(t, blockedOrders)
	recoveredOrders, err := selectRecentPendingSubscriptionOrders(DB, provider, 60, 600, 2, now.Add(epayReconcileClaimLease), true, nil)
	require.NoError(t, err)
	require.Len(t, recoveredOrders, 2)
	nextPassOrders, err := selectRecentPendingSubscriptionOrders(
		DB, provider, 60, 600, 1, now.Add(epayReconcileClaimLease+time.Nanosecond), true,
		[]int{recoveredOrders[0].Id, recoveredOrders[1].Id},
	)
	require.NoError(t, err)
	require.Len(t, nextPassOrders, 1)
	assert.Equal(t, orders[2].Id, nextPassOrders[0].Id)
}

func TestPendingEpayClaimsDoNotOverlapConcurrently(t *testing.T) {
	dsn := fmt.Sprintf("file:epay-claim-%s?mode=memory&cache=shared&_pragma=busy_timeout(5000)", common.GetUUID())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&TopUp{}, &SubscriptionOrder{}))
	for _, table := range []string{"top_ups", "subscription_orders"} {
		var columns []struct {
			Name         string  `gorm:"column:name"`
			NotNull      int     `gorm:"column:notnull"`
			DefaultValue *string `gorm:"column:dflt_value"`
		}
		require.NoError(t, db.Raw("PRAGMA table_info("+table+")").Scan(&columns).Error)
		found := false
		for _, column := range columns {
			if column.Name != "reconcile_time" {
				continue
			}
			found = true
			assert.Zero(t, column.NotNull, "startup migration must add a nullable column")
			assert.Nil(t, column.DefaultValue, "startup migration must not rewrite old rows with a default")
		}
		assert.True(t, found)
		var indexes []struct {
			Name string `gorm:"column:name"`
		}
		require.NoError(t, db.Raw("PRAGMA index_list("+table+")").Scan(&indexes).Error)
		for _, index := range indexes {
			assert.NotContains(t, index.Name, "reconcile_time", "startup migration must not build a blocking single-column index")
		}
	}
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Now()
	provider := "epay-concurrent-" + common.GetUUID()[:8]
	for i := 0; i < 4; i++ {
		require.NoError(t, db.Create(&TopUp{
			UserId: 1, TradeNo: fmt.Sprintf("CONCURRENT-TOPUP-%d-%s", i, common.GetUUID()[:8]),
			PaymentProvider: provider, CreateTime: now.Unix() - 120, Status: common.TopUpStatusPending,
		}).Error)
		require.NoError(t, db.Create(&SubscriptionOrder{
			UserId: 1, PlanId: 1, TradeNo: fmt.Sprintf("CONCURRENT-SUB-%d-%s", i, common.GetUUID()[:8]),
			PaymentProvider: provider, CreateTime: now.Unix() - 120, Status: common.TopUpStatusPending,
		}).Error)
	}

	type result struct {
		topUps []*TopUp
		orders []*SubscriptionOrder
		err    error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			topUps, claimErr := selectRecentPendingTopUps(db, provider, 60, 600, 2, now, true, nil)
			if claimErr != nil {
				results <- result{err: claimErr}
				return
			}
			orders, claimErr := selectRecentPendingSubscriptionOrders(db, provider, 60, 600, 2, now, true, nil)
			results <- result{topUps: topUps, orders: orders, err: claimErr}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	topUpIDs := make(map[int]struct{}, 4)
	orderIDs := make(map[int]struct{}, 4)
	for claimed := range results {
		require.NoError(t, claimed.err)
		for _, topUp := range claimed.topUps {
			_, duplicate := topUpIDs[topUp.Id]
			assert.False(t, duplicate, "the same topup was claimed by two workers")
			topUpIDs[topUp.Id] = struct{}{}
		}
		for _, order := range claimed.orders {
			_, duplicate := orderIDs[order.Id]
			assert.False(t, duplicate, "the same subscription was claimed by two workers")
			orderIDs[order.Id] = struct{}{}
		}
	}
	assert.Len(t, topUpIDs, 4)
	assert.Len(t, orderIDs, 4)
}

func TestPendingEpayClaimsRespectLimitWithExcludedIDs(t *testing.T) {
	now := time.Now()
	provider := "epay-limit-" + common.GetUUID()
	topUps := make([]*TopUp, 0, 3)
	orders := make([]*SubscriptionOrder, 0, 3)
	for i := 0; i < 3; i++ {
		topUp := &TopUp{
			UserId: 1, TradeNo: fmt.Sprintf("limit-topup-%s-%d", provider, i),
			PaymentProvider: provider, Status: common.TopUpStatusPending,
			CreateTime: now.Add(-2 * time.Minute).Unix(),
		}
		require.NoError(t, DB.Create(topUp).Error)
		topUps = append(topUps, topUp)
		order := &SubscriptionOrder{
			UserId: 1, PlanId: 1, TradeNo: fmt.Sprintf("limit-order-%s-%d", provider, i),
			PaymentProvider: provider, Status: common.TopUpStatusPending,
			CreateTime: now.Add(-2 * time.Minute).Unix(),
		}
		require.NoError(t, DB.Create(order).Error)
		orders = append(orders, order)
	}
	t.Cleanup(func() {
		for _, topUp := range topUps {
			DB.Unscoped().Delete(&TopUp{}, topUp.Id)
		}
		for _, order := range orders {
			DB.Unscoped().Delete(&SubscriptionOrder{}, order.Id)
		}
	})

	claimedTopUps, err := selectRecentPendingTopUps(
		DB, provider, 60, 600, 1, now, true, []int{-1, -2},
	)
	require.NoError(t, err)
	require.Len(t, claimedTopUps, 1)

	claimedOrders, err := selectRecentPendingSubscriptionOrders(
		DB, provider, 60, 600, 1, now, true, []int{-1, -2},
	)
	require.NoError(t, err)
	require.Len(t, claimedOrders, 1)
}
