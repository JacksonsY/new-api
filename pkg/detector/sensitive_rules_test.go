package detector

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScanSensitiveData(t *testing.T) {
	// A clean benign reply matches nothing.
	assert.Empty(t, scanSensitiveData("OK"))
	assert.Empty(t, scanSensitiveData(""))

	cases := []struct {
		name     string
		text     string
		wantRule string
		wantCat  string
	}{
		{"aws_key", "key=AKIAIOSFODNN7EXAMPLE", "AWS Access Key", sensCategoryCredential},
		{"private_key", "-----BEGIN RSA PRIVATE KEY-----", "私钥文件", sensCategoryCredential},
		{"jwt", "token eyJhbGciOiJIUzI1.eyJzdWIiOiIxMjM0NTY", "JWT Token", sensCategoryCredential},
		{"internal_ip", "host 192.168.1.100 down", "IPv4 内网地址", sensCategoryNetwork},
		{"jdbc", "jdbc:mysql://10.0.0.5:3306/prod", "JDBC 连接串", sensCategoryNetwork},
		{"bank_card", "card 4111 1111 1111 1111 ok", "银行卡号", sensCategoryFinancial},
		{"phone", "call 13812345678 now", "手机号", sensCategoryPII},
		{"email", "mail admin@corp.local please", "邮箱地址", sensCategoryPII},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hits := scanSensitiveData(c.text)
			var found *sensitiveHit
			for i := range hits {
				if hits[i].Rule == c.wantRule {
					found = &hits[i]
				}
			}
			if assert.NotNil(t, found, "expected rule %q to match %q", c.wantRule, c.text) {
				assert.Equal(t, c.wantCat, found.Category)
				assert.GreaterOrEqual(t, found.Count, 1)
			}
		})
	}
}

func TestSensitiveWorstSeverity(t *testing.T) {
	assert.Equal(t, "", sensitiveWorstSeverity(nil))
	assert.Equal(t, sevMajor, sensitiveWorstSeverity([]sensitiveHit{{Rule: "手机号", Category: sensCategoryPII}}))
	assert.Equal(t, sevCritical, sensitiveWorstSeverity([]sensitiveHit{{Rule: "银行卡号", Category: sensCategoryFinancial}}))
	assert.Equal(t, sevCritical, sensitiveWorstSeverity([]sensitiveHit{
		{Rule: "手机号", Category: sensCategoryPII},
		{Rule: "AWS Access Key", Category: sensCategoryCredential},
	}))
}

func TestSensitiveLeakVerdict(t *testing.T) {
	assert.Equal(t, "pass", sensitiveLeakVerdict(nil).Status)
	credential := sensitiveLeakVerdict([]sensitiveHit{{Rule: "AWS Access Key", Category: sensCategoryCredential, Count: 1}})
	assert.Equal(t, "fail", credential.Status)
	assert.Equal(t, float64(0), credential.Score)
	pii := sensitiveLeakVerdict([]sensitiveHit{{Rule: "手机号", Category: sensCategoryPII, Count: 1}})
	assert.Equal(t, "fail", pii.Status)
	assert.Equal(t, float64(40), pii.Score)
}
