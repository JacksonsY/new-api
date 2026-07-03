package common

import (
	"crypto/subtle"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type verificationValue struct {
	code string
	time time.Time
}

const (
	EmailVerificationPurpose = "v"
	PasswordResetPurpose     = "r"
)

var verificationMutex sync.Mutex
var verificationMap map[string]verificationValue
var verificationMapMaxSize = 10

// verificationMapHardLimit 是验证码 map 的硬上限，防止恶意批量请求
// 验证码导致内存无限增长。达到上限且清理过期项后仍然超限时，淘汰最旧的一条。
const verificationMapHardLimit = 100000

var VerificationValidMinutes = 10

func GenerateVerificationCode(length int) string {
	code := uuid.New().String()
	code = strings.Replace(code, "-", "", -1)
	if length == 0 {
		return code
	}
	return code[:length]
}

func RegisterVerificationCodeWithKey(key string, code string, purpose string) {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap[purpose+key] = verificationValue{
		code: code,
		time: time.Now(),
	}
	if len(verificationMap) > verificationMapMaxSize {
		removeExpiredPairs()
	}
	// 硬上限保护：清理后仍超限则逐出最旧的条目
	for len(verificationMap) > verificationMapHardLimit {
		removeOldestPair()
	}
}

func VerifyCodeWithKey(key string, code string, purpose string) bool {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	value, okay := verificationMap[purpose+key]
	now := time.Now()
	if !okay || int(now.Sub(value.time).Seconds()) >= VerificationValidMinutes*60 {
		return false
	}
	// 常数时间比较，防止基于时序的验证码逐位猜测
	return subtle.ConstantTimeCompare([]byte(code), []byte(value.code)) == 1
}

func DeleteKey(key string, purpose string) {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	delete(verificationMap, purpose+key)
}

// no lock inside, so the caller must lock the verificationMap before calling!
func removeExpiredPairs() {
	now := time.Now()
	for key := range verificationMap {
		if int(now.Sub(verificationMap[key].time).Seconds()) >= VerificationValidMinutes*60 {
			delete(verificationMap, key)
		}
	}
}

// no lock inside, so the caller must lock the verificationMap before calling!
func removeOldestPair() {
	var oldestKey string
	var oldestTime time.Time
	for key, value := range verificationMap {
		if oldestKey == "" || value.time.Before(oldestTime) {
			oldestKey = key
			oldestTime = value.time
		}
	}
	if oldestKey != "" {
		delete(verificationMap, oldestKey)
	}
}

func init() {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap = make(map[string]verificationValue)
}
