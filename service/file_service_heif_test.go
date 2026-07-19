package service

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// box 拼装一个 ISO-BMFF box：4 字节大小 + 4 字节类型（boxType 须为 4 字节）+ payload。
func box(boxType string, payload []byte) []byte {
	buf := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(buf[0:4], uint32(8+len(payload)))
	copy(buf[4:8], boxType)
	copy(buf[8:], payload)
	return buf
}

func TestFindISPEStopsAtDepthLimit(t *testing.T) {
	// 构造深度超过限制的嵌套 ipco 链，末端塞一个合法 ispe。
	// 加了深度上限后应在触底前放弃并返回 false，而不是无界递归耗尽栈。
	ispe := make([]byte, 12)
	binary.BigEndian.PutUint32(ispe[4:8], 640)
	binary.BigEndian.PutUint32(ispe[8:12], 480)
	payload := box("ispe", ispe)
	for i := 0; i < 64; i++ {
		payload = box("ipco", payload)
	}

	_, _, ok := findISPE(payload, 0)
	assert.False(t, ok, "深度超限时必须放弃解析，防止递归 DoS")
}

func TestFindISPEResolvesWithinDepthLimit(t *testing.T) {
	// 正常的 iprp -> ipco -> ispe 链应仍能解析出尺寸。
	ispe := make([]byte, 12)
	binary.BigEndian.PutUint32(ispe[4:8], 1920)
	binary.BigEndian.PutUint32(ispe[8:12], 1080)
	payload := box("iprp", box("ipco", box("ispe", ispe)))

	w, h, ok := findISPE(payload, 0)
	require.True(t, ok)
	assert.Equal(t, 1920, w)
	assert.Equal(t, 1080, h)
}
