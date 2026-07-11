package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 进程内 IP 埋点去重契约:窗口内同键只放行一次(其余请求零 DB 查询),
// 窗口过期后重新放行;容量护栏触发时整表重建而不是无限增长。
func TestIPRecordSeenRecently(t *testing.T) {
	ipRecordSeenMu.Lock()
	ipRecordSeen = make(map[string]int64)
	ipRecordSeenMu.Unlock()

	assert.False(t, ipRecordSeenRecently("1|1.2.3.4|api_call", 1000))
	assert.True(t, ipRecordSeenRecently("1|1.2.3.4|api_call", 1000+ipRecordDedupSeconds-1))
	assert.False(t, ipRecordSeenRecently("1|1.2.3.4|api_call", 1000+ipRecordDedupSeconds))
	assert.False(t, ipRecordSeenRecently("2|1.2.3.4|api_call", 1000), "不同用户互不去重")
}
