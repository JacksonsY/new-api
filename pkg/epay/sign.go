package epay

import (
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// secureCompare 恒定时间字符串比较，用于签名校验避免时序侧信道。
func secureCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// signContent 生成两代协议共用的待签名串：参数按键名 ASCII 升序，
// 跳过 sign/sign_type/空值，以 k=v&k=v 原文拼接（值不做 URL 编码）。
// 与 PHP SDK 的 getSign/getSignContent 逐行对齐。
func signContent(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for key, value := range params {
		// 与彩虹 PHP SDK getSignContent 逐字对齐：仅剔除严格空串（不 trim），
		// 否则纯空白值字段在两侧的取舍不同会导致合法回调验签假阴性丢单。
		if key == "sign" || key == "sign_type" || value == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+params[key])
	}
	return strings.Join(parts, "&")
}

// md5Sign v1 签名：待签名串直接拼接商户 key 后取 MD5 小写十六进制。
func md5Sign(params map[string]string, key string) string {
	digest := md5.Sum([]byte(signContent(params) + key))
	return fmt.Sprintf("%x", digest)
}

// md5SignParams v1：为参数补充 sign/sign_type 字段（返回原 map）。
func md5SignParams(params map[string]string, key string) map[string]string {
	params["sign"] = md5Sign(params, key)
	params["sign_type"] = "MD5"
	return params
}

// parseRSAKey 宽容解析密钥输入：接受纯 base64 DER 或完整 PEM（自动剥头尾与空白）。
func parseRSAKeyBytes(input string) ([]byte, error) {
	cleaned := strings.NewReplacer(
		"-----BEGIN PRIVATE KEY-----", "",
		"-----END PRIVATE KEY-----", "",
		"-----BEGIN PUBLIC KEY-----", "",
		"-----END PUBLIC KEY-----", "",
		"-----BEGIN RSA PRIVATE KEY-----", "",
		"-----END RSA PRIVATE KEY-----", "",
		"\r", "", "\n", "", " ", "", "\t", "",
	).Replace(input)
	if cleaned == "" {
		return nil, errors.New("epay: empty rsa key")
	}
	der, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return nil, fmt.Errorf("epay: invalid rsa key base64: %w", err)
	}
	return der, nil
}

// parseMerchantPrivateKey 解析商户私钥（PKCS8，兼容 PKCS1）。
func parseMerchantPrivateKey(input string) (*rsa.PrivateKey, error) {
	der, err := parseRSAKeyBytes(input)
	if err != nil {
		return nil, err
	}
	if key, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("epay: merchant private key is not RSA")
		}
		return rsaKey, nil
	}
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return key, nil
	}
	return nil, errors.New("epay: failed to parse merchant private key")
}

// parsePlatformPublicKey 解析平台公钥（PKIX）。
func parsePlatformPublicKey(input string) (*rsa.PublicKey, error) {
	der, err := parseRSAKeyBytes(input)
	if err != nil {
		return nil, err
	}
	key, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, errors.New("epay: failed to parse platform public key")
	}
	rsaKey, ok := key.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("epay: platform public key is not RSA")
	}
	return rsaKey, nil
}

// rsaSign v2 签名：SHA256WithRSA，base64 输出。
func rsaSign(content string, privateKey *rsa.PrivateKey) (string, error) {
	digest := sha256.Sum256([]byte(content))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

// rsaVerify v2 验签：平台公钥验 SHA256WithRSA 签名。
func rsaVerify(content string, signB64 string, publicKey *rsa.PublicKey) error {
	signature, err := base64.StdEncoding.DecodeString(signB64)
	if err != nil {
		return fmt.Errorf("epay: invalid signature base64: %w", err)
	}
	digest := sha256.Sum256([]byte(content))
	return rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signature)
}
