package service

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/pkg/epay"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func epayTestRSAKeys(t *testing.T) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	require.NoError(t, err)
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(privateDER), base64.StdEncoding.EncodeToString(publicDER)
}

func TestBuildEpayClientHonorsConfiguredProtocolVersion(t *testing.T) {
	originalAddress := operation_setting.PayAddress
	originalID := operation_setting.EpayId
	originalKey := operation_setting.EpayKey
	originalVersion := operation_setting.EpayApiVersion
	originalPublicKey := operation_setting.EpayPlatformPublicKey
	originalPrivateKey := operation_setting.EpayMerchantPrivateKey
	t.Cleanup(func() {
		operation_setting.PayAddress = originalAddress
		operation_setting.EpayId = originalID
		operation_setting.EpayKey = originalKey
		operation_setting.EpayApiVersion = originalVersion
		operation_setting.EpayPlatformPublicKey = originalPublicKey
		operation_setting.EpayMerchantPrivateKey = originalPrivateKey
	})

	privateKey, publicKey := epayTestRSAKeys(t)
	operation_setting.PayAddress = "https://pay.example.com"
	operation_setting.EpayId = "1000"
	operation_setting.EpayKey = "MD5_SECRET"
	operation_setting.EpayPlatformPublicKey = publicKey
	operation_setting.EpayMerchantPrivateKey = privateKey

	args := &epay.PurchaseArgs{
		Type: "alipay", ServiceTradeNo: "ORDER-1", Name: "test", Money: "1.00",
		NotifyUrl: &url.URL{Scheme: "https", Host: "gateway.example.com", Path: "/notify"},
		ReturnUrl: &url.URL{Scheme: "https", Host: "gateway.example.com", Path: "/return"},
	}

	operation_setting.EpayApiVersion = epay.VersionV1
	client, err := buildEpayClient()
	require.NoError(t, err)
	purchaseURL, params, err := client.Purchase(args)
	require.NoError(t, err)
	assert.Equal(t, "https://pay.example.com/submit.php", purchaseURL)
	assert.Equal(t, "MD5", params["sign_type"])

	operation_setting.EpayApiVersion = epay.VersionV2
	client, err = buildEpayClient()
	require.NoError(t, err)
	purchaseURL, params, err = client.Purchase(args)
	require.NoError(t, err)
	assert.Equal(t, "https://pay.example.com/api/pay/submit", purchaseURL)
	assert.Equal(t, "RSA", params["sign_type"])

	operation_setting.EpayMerchantPrivateKey = "invalid-rsa-key"
	_, err = buildEpayClient()
	assert.Error(t, err, "selected v2 must fail instead of silently switching real payments to v1")
}
