package controller

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// validateSupplierPayoutInfo 守两条契约：三项必填 + 按后端列宽限长（account/contact
// 128、name 64，rune 计），防止空信息入库或超长被 DB 截断。用 rune 覆盖 CJK 边界。
func TestValidateSupplierPayoutInfo(t *testing.T) {
	ok := supplierPayoutInfoRequest{
		Method:  "alipay",
		Account: "user@example.com",
		Name:    "张三",
		Contact: "wx:zhangsan",
	}

	cases := []struct {
		name    string
		req     supplierPayoutInfoRequest
		wantErr bool
	}{
		{"valid", ok, false},
		{"valid trims to non-empty", supplierPayoutInfoRequest{Account: "  a  ", Name: " b ", Contact: " c "}, false},
		{"missing account", supplierPayoutInfoRequest{Account: "", Name: "b", Contact: "c"}, true},
		{"blank account (spaces only)", supplierPayoutInfoRequest{Account: "   ", Name: "b", Contact: "c"}, true},
		{"missing name", supplierPayoutInfoRequest{Account: "a", Name: "", Contact: "c"}, true},
		{"missing contact", supplierPayoutInfoRequest{Account: "a", Name: "b", Contact: ""}, true},
		{"account at limit (128)", supplierPayoutInfoRequest{Account: strings.Repeat("a", 128), Name: "b", Contact: "c"}, false},
		{"account over limit (129)", supplierPayoutInfoRequest{Account: strings.Repeat("a", 129), Name: "b", Contact: "c"}, true},
		{"name at limit (64)", supplierPayoutInfoRequest{Account: "a", Name: strings.Repeat("b", 64), Contact: "c"}, false},
		{"name over limit (65)", supplierPayoutInfoRequest{Account: "a", Name: strings.Repeat("b", 65), Contact: "c"}, true},
		{"contact over limit (129)", supplierPayoutInfoRequest{Account: "a", Name: "b", Contact: strings.Repeat("c", 129)}, true},
		{"CJK name within 64 runes counts by rune not byte", supplierPayoutInfoRequest{Account: "a", Name: strings.Repeat("张", 64), Contact: "c"}, false},
		{"CJK name over 64 runes", supplierPayoutInfoRequest{Account: "a", Name: strings.Repeat("张", 65), Contact: "c"}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSupplierPayoutInfo(tc.req)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// validateSupplierProfile 守商户资料契约：名称+联系方式必填，按后端列宽限长
// （name 64、contact 128、intro 255，rune 计），防空信息入库或超长被 DB 截断。
func TestValidateSupplierProfile(t *testing.T) {
	cases := []struct {
		name    string
		req     supplierProfileRequest
		wantErr bool
	}{
		{"valid", supplierProfileRequest{Name: "A6 供应商", Contact: "wx:a6", Intro: "官方直连"}, false},
		{"intro optional", supplierProfileRequest{Name: "A6", Contact: "wx:a6"}, false},
		{"missing name", supplierProfileRequest{Name: "", Contact: "c"}, true},
		{"blank name (spaces only)", supplierProfileRequest{Name: "   ", Contact: "c"}, true},
		{"missing contact", supplierProfileRequest{Name: "n", Contact: ""}, true},
		{"name at limit (64 runes)", supplierProfileRequest{Name: strings.Repeat("张", 64), Contact: "c"}, false},
		{"name over limit (65 runes)", supplierProfileRequest{Name: strings.Repeat("张", 65), Contact: "c"}, true},
		{"contact over limit (129)", supplierProfileRequest{Name: "n", Contact: strings.Repeat("c", 129)}, true},
		{"intro over limit (256)", supplierProfileRequest{Name: "n", Contact: "c", Intro: strings.Repeat("i", 256)}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSupplierProfile(tc.req)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// validateSupplierChannel 守渠道要约契约：名称 1-10 字、无首尾空格、不含逗号/引号
// （逗号会破坏 models 的逗号分隔），key/models 必填，报价率封顶。
func TestValidateSupplierChannel(t *testing.T) {
	base := func() supplierChannelRequest {
		return supplierChannelRequest{Name: "acme-01", Type: 1, Key: "sk-x", Models: "gpt-4o", ChannelRatio: 1}
	}
	with := func(mut func(*supplierChannelRequest)) supplierChannelRequest {
		r := base()
		mut(&r)
		return r
	}
	cases := []struct {
		name    string
		req     supplierChannelRequest
		wantErr bool
	}{
		{"valid", base(), false},
		{"ratio 0 allowed (apply omits; server defaults)", with(func(r *supplierChannelRequest) { r.ChannelRatio = 0 }), false},
		{"name empty", with(func(r *supplierChannelRequest) { r.Name = "" }), true},
		{"name 10 chars boundary", with(func(r *supplierChannelRequest) { r.Name = "abcde12345" }), false},
		{"name 11 chars over", with(func(r *supplierChannelRequest) { r.Name = "abcde123456" }), true},
		{"name 10 CJK runes ok", with(func(r *supplierChannelRequest) { r.Name = strings.Repeat("模", 10) }), false},
		{"name 11 CJK runes over", with(func(r *supplierChannelRequest) { r.Name = strings.Repeat("模", 11) }), true},
		{"name leading space", with(func(r *supplierChannelRequest) { r.Name = " acme" }), true},
		{"name trailing space", with(func(r *supplierChannelRequest) { r.Name = "acme " }), true},
		{"name with ascii comma", with(func(r *supplierChannelRequest) { r.Name = "a,b" }), true},
		{"name with fullwidth comma", with(func(r *supplierChannelRequest) { r.Name = "a，b" }), true},
		{"name with double quote", with(func(r *supplierChannelRequest) { r.Name = "a\"b" }), true},
		{"name with single quote", with(func(r *supplierChannelRequest) { r.Name = "a'b" }), true},
		{"missing key", with(func(r *supplierChannelRequest) { r.Key = "" }), true},
		{"missing models", with(func(r *supplierChannelRequest) { r.Models = "" }), true},
		{"ratio negative", with(func(r *supplierChannelRequest) { r.ChannelRatio = -0.1 }), true},
		{"ratio over max", with(func(r *supplierChannelRequest) { r.ChannelRatio = 1.5 }), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSupplierChannel(tc.req)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
