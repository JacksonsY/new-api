// Package s3lite 提供一个极简的 S3 兼容对象写入客户端(sigv4 签名),
// 服务于用户个人存储桶(Cloudflare R2、阿里云 OSS、腾讯云 COS、AWS S3、MinIO 等)的媒体归档。
// 只实现 PutObject:网关只写不读,公开读取由桶的公开访问或自定义域名承担。
package s3lite

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// unsignedPayload 让签名不依赖请求体哈希,从而支持流式上传(无需先读全量算 sha256)。
const unsignedPayload = "UNSIGNED-PAYLOAD"

type Config struct {
	Endpoint     string // S3 API 地址,如 https://<accountid>.r2.cloudflarestorage.com
	Bucket       string
	Region       string // 选填;为空时按 Endpoint 推导(R2→auto,其余→us-east-1)
	AccessKeyID  string
	SecretKey    string
	PublicDomain string // 选填;桶绑定的公开访问域名,拼接对外 URL 时优先使用
	PathStyle    bool   // true=path-style(MinIO 等);false=virtual-hosted
}

// Validate 校验凭证与地址的基本合法性(不发起网络请求)。
func (c Config) Validate() error {
	if c.AccessKeyID == "" || c.SecretKey == "" {
		return errors.New("missing access key or secret key")
	}
	if c.Bucket == "" {
		return errors.New("missing bucket")
	}
	for _, r := range c.Bucket {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' {
			continue
		}
		return fmt.Errorf("invalid bucket name %q", c.Bucket)
	}
	u, err := url.Parse(c.Endpoint)
	if err != nil {
		return fmt.Errorf("invalid endpoint: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("endpoint must start with http:// or https://")
	}
	if u.Host == "" {
		return errors.New("endpoint host is empty")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return errors.New("endpoint must not contain query or fragment")
	}
	if c.PublicDomain != "" {
		pd, err := url.Parse(c.PublicDomain)
		if err != nil || pd.Scheme != "http" && pd.Scheme != "https" || pd.Host == "" {
			return errors.New("public domain must be a valid http(s) URL")
		}
	}
	return nil
}

// ResolvedRegion 返回签名用 region:显式配置优先;R2 端点默认 auto,其余默认 us-east-1。
// OSS/COS/MinIO 的 S3 兼容层对 credential scope 中的 region 宽松,默认值即可用;更严格的服务可显式填写。
func (c Config) ResolvedRegion() string {
	if r := strings.TrimSpace(c.Region); r != "" {
		return r
	}
	if u, err := url.Parse(c.Endpoint); err == nil && strings.HasSuffix(strings.ToLower(u.Hostname()), ".r2.cloudflarestorage.com") {
		return "auto"
	}
	return "us-east-1"
}

// RequestURL 返回对象在 S3 API 上的请求地址(即 PUT 目标;桶开启公开读取时也可直接 GET)。
func (c Config) RequestURL(key string) (string, error) {
	u, err := url.Parse(c.Endpoint)
	if err != nil {
		return "", fmt.Errorf("invalid endpoint: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", errors.New("endpoint must include scheme and host")
	}
	basePath := strings.TrimSuffix(u.Path, "/")
	if c.PathStyle {
		return u.Scheme + "://" + u.Host + basePath + "/" + c.Bucket + "/" + escapeKey(key), nil
	}
	return u.Scheme + "://" + c.Bucket + "." + u.Host + basePath + "/" + escapeKey(key), nil
}

// PublicURL 返回对象的对外访问地址:配置了公开域名时用公开域名,否则退回 S3 API 地址。
func (c Config) PublicURL(key string) string {
	if domain := strings.TrimSuffix(strings.TrimSpace(c.PublicDomain), "/"); domain != "" {
		return domain + "/" + escapeKey(key)
	}
	u, err := c.RequestURL(key)
	if err != nil {
		return ""
	}
	return u
}

// escapeKey 按路径段转义对象 key,保留 "/" 层级。
func escapeKey(key string) string {
	segments := strings.Split(key, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return strings.Join(segments, "/")
}

// PutObject 以 sigv4 + UNSIGNED-PAYLOAD 上传对象。contentLength 必须为实际字节数(S3 PUT 不支持 chunked)。
func PutObject(ctx context.Context, client *http.Client, cfg Config, key, contentType string, body io.Reader, contentLength int64) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if contentLength < 0 {
		return errors.New("content length must be known for s3 put")
	}
	target, err := cfg.RequestURL(key)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, body)
	if err != nil {
		return err
	}
	req.ContentLength = contentLength
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("x-amz-content-sha256", unsignedPayload)

	signer := v4.NewSigner()
	err = signer.SignHTTP(ctx, aws.Credentials{
		AccessKeyID:     cfg.AccessKeyID,
		SecretAccessKey: cfg.SecretKey,
	}, req, unsignedPayload, "s3", cfg.ResolvedRegion(), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("sign request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("s3 put returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
}
