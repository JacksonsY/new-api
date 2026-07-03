package service

// jzlh-agent 反欺诈 IP 快表(user_ip_records)每日清理任务。
// moeacgx 原实现的 CleanOldIPRecords 没有任何调用方，表只增不减——这里补上。

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const ipRecordCleanupInterval = 24 * time.Hour

var ipRecordCleanupOnce sync.Once

func StartIPRecordCleanupTask() {
	ipRecordCleanupOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			ticker := time.NewTicker(ipRecordCleanupInterval)
			defer ticker.Stop()

			runIPRecordCleanupOnce()
			for range ticker.C {
				runIPRecordCleanupOnce()
			}
		})
	})
}

func runIPRecordCleanupOnce() {
	retentionDays := common.AgentIPRecordRetentionDays
	if retentionDays <= 0 {
		return // 0 = 永久保留
	}
	before := common.GetTimestamp() - int64(retentionDays)*86400
	deleted, err := model.CleanOldIPRecords(before)
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("ip record cleanup failed: %v", err))
		return
	}
	if deleted > 0 {
		logger.LogInfo(context.Background(), fmt.Sprintf("ip record cleanup: %d rows older than %d days removed", deleted, retentionDays))
	}
}
