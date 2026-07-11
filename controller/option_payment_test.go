package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUpdatePaymentOptionsRejectsInvalidValuesBeforePersistence(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]string
	}{
		{
			name:   "non-finite price",
			values: map[string]string{"Price": "NaN"},
		},
		{
			name:   "fractional minimum top up",
			values: map[string]string{"MinTopUp": "0.5"},
		},
		{
			name:   "wrong payment methods shape",
			values: map[string]string{"PayMethods": "{}"},
		},
		{
			name:   "wrong amount discount shape",
			values: map[string]string{"payment_setting.amount_discount": "[]"},
		},
		{
			name:   "fractional amount option",
			values: map[string]string{"payment_setting.amount_options": "[10.5]"},
		},
		{
			name:   "discount above one",
			values: map[string]string{"payment_setting.amount_discount": `{"100":1.1}`},
		},
		{
			name:   "payment method missing type",
			values: map[string]string{"PayMethods": `[{"name":"card"}]`},
		},
		{
			name:   "invalid creem product quota",
			values: map[string]string{"CreemProducts": `[{"productId":"p1","name":"P1","price":1,"currency":"USD","quota":0}]`},
		},
		{
			name:   "invalid waffo payment method field",
			values: map[string]string{"WaffoPayMethods": `[{"name":1}]`},
		},
		{
			name:   "invalid boolean",
			values: map[string]string{"WaffoEnabled": "yes"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Error(t, validatePaymentOptionValues(test.values))
		})
	}
}

func TestValidatePaymentOptionValuesAllowsExplicitEpayKeyClears(t *testing.T) {
	assert.NoError(t, validatePaymentOptionValues(map[string]string{
		"EpayPlatformPublicKey":           "",
		"EpayMerchantPrivateKey":          "",
		"payment_setting.amount_options":  "[10,20]",
		"payment_setting.amount_discount": `{"10":0.9}`,
		"PayMethods":                      `[{"name":"Alipay","type":"alipay"}]`,
		"CreemProducts":                   `[{"productId":"p1","name":"P1","price":1,"currency":"USD","quota":1}]`,
		"WaffoPayMethods":                 `[{"name":"Card","payMethodType":"CREDITCARD","payMethodName":""}]`,
	}))
}
