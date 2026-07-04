package model

// 回归：ChannelInfo.Scan 必须兼容不同驱动/存储类别返回的类型。
// glebarez/sqlite(纯 Go) 对 TEXT 存储的 json 列返回 string；Go 侧 Value()
// 写入走 []byte(BLOB)。手工 SQL、外部工具、跨库迁移会产生 TEXT 行，
// 旧实现只断言 []byte 导致这类行解析为零值并报 JSON 错误。
import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelInfoScanDriverCompat(t *testing.T) {
	payload := `{"is_multi_key":true,"multi_key_size":3,"multi_key_mode":"random"}`

	t.Run("string (SQLite TEXT storage)", func(t *testing.T) {
		var ci ChannelInfo
		require.NoError(t, ci.Scan(payload))
		assert.True(t, ci.IsMultiKey)
		assert.Equal(t, 3, ci.MultiKeySize)
	})

	t.Run("[]byte (BLOB / MySQL / PG)", func(t *testing.T) {
		var ci ChannelInfo
		require.NoError(t, ci.Scan([]byte(payload)))
		assert.True(t, ci.IsMultiKey)
		assert.Equal(t, 3, ci.MultiKeySize)
	})

	t.Run("nil and empty degrade to zero value", func(t *testing.T) {
		for _, v := range []interface{}{nil, "", []byte{}} {
			ci := ChannelInfo{IsMultiKey: true}
			require.NoError(t, ci.Scan(v))
			assert.False(t, ci.IsMultiKey)
		}
	})

	t.Run("unsupported type errors instead of silent zero", func(t *testing.T) {
		var ci ChannelInfo
		require.Error(t, ci.Scan(12345))
	})
}
