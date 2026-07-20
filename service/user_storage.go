package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/s3lite"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	// maxUserStorageImageBytes 单张图片归档上限。
	maxUserStorageImageBytes = int64(64 << 20)
	// maxUserStorageVideoBytes 单个视频归档上限(Content-Length 已知时流式上传)。
	maxUserStorageVideoBytes = int64(2 << 30)
	// maxUserStorageBufferBytes 上游未返回 Content-Length 时的内存缓冲上限。
	maxUserStorageBufferBytes = int64(256 << 20)
)

// GetUserStorageConfig 返回用户的个人存储桶配置;未配置或配置不完整时返回 nil。
func GetUserStorageConfig(userId int) *s3lite.Config {
	if userId <= 0 {
		return nil
	}
	setting, err := model.GetUserSetting(userId, false)
	if err != nil || setting.Storage == nil {
		return nil
	}
	st := setting.Storage
	if st.Endpoint == "" || st.Bucket == "" || st.AccessKeyID == "" || st.SecretKey == "" {
		return nil
	}
	return &s3lite.Config{
		Endpoint:     st.Endpoint,
		Bucket:       st.Bucket,
		Region:       st.Region,
		AccessKeyID:  st.AccessKeyID,
		SecretKey:    st.SecretKey,
		PublicDomain: st.PublicDomain,
		PathStyle:    st.PathStyle,
	}
}

// VerifyUserStorage 向桶内试写一个测试文件验证凭证与权限。
// 先按 cfg.PathStyle 指定的寻址方式尝试,失败后退回另一种(MinIO 常需 path-style),
// 探测到的可用方式写回 cfg.PathStyle 供调用方持久化。
func VerifyUserStorage(ctx context.Context, cfg *s3lite.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	content := []byte(fmt.Sprintf("new-api storage verification %s", time.Now().UTC().Format(time.RFC3339)))
	key := fmt.Sprintf("new-api/verify/%d.txt", time.Now().Unix())
	var firstErr error
	for _, pathStyle := range []bool{cfg.PathStyle, !cfg.PathStyle} {
		attempt := *cfg
		attempt.PathStyle = pathStyle
		err := putToUserStorage(ctx, attempt, key, "text/plain; charset=utf-8", bytes.NewReader(content), int64(len(content)))
		if err == nil {
			cfg.PathStyle = pathStyle
			return nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// putToUserStorage 校验 PUT 目标未被 SSRF 策略拦截后写入对象。
func putToUserStorage(ctx context.Context, cfg s3lite.Config, key, contentType string, body io.Reader, length int64) error {
	target, err := cfg.RequestURL(key)
	if err != nil {
		return err
	}
	if err := ValidateSSRFProtectedFetchURL(target); err != nil {
		return fmt.Errorf("storage endpoint blocked: %w", err)
	}
	return s3lite.PutObject(ctx, GetSSRFProtectedHTTPClient(), cfg, key, contentType, body, length)
}

// uploadUserStorageObject 上传对象并返回对外访问 URL。
func uploadUserStorageObject(ctx context.Context, cfg *s3lite.Config, key, contentType string, body io.Reader, length int64) (string, error) {
	if err := putToUserStorage(ctx, *cfg, key, contentType, body, length); err != nil {
		return "", err
	}
	return cfg.PublicURL(key), nil
}

// ArchiveImageResponseBody 把 OpenAI 格式图片响应中的产物转存到用户个人存储桶并改写 URL。
// data[i].url(http 直链,通常有时效)下载后转存,url 改写为桶地址;
// data[i].b64_json 或 data: URL 解码上传,补充 url 字段(b64_json 原样保留,不改变客户端契约)。
// 未配置个人桶时原样返回;任一项失败保持该项不变(fail-open),仅记录日志。
func ArchiveImageResponseBody(c *gin.Context, userId int, body []byte) []byte {
	cfg := GetUserStorageConfig(userId)
	if cfg == nil {
		return body
	}
	items := gjson.GetBytes(body, "data")
	if !items.IsArray() {
		return body
	}
	arr := items.Array()
	if len(arr) == 0 {
		return body
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	out := body
	archived := 0
	for i, item := range arr {
		data, contentType, err := loadImageArchiveSource(ctx, item)
		if err != nil {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("user storage: image %d not archived: %s", i, err.Error()))
			continue
		}
		if data == nil {
			continue
		}
		key := fmt.Sprintf("new-api/images/%s/%s.%s", time.Now().UTC().Format("20060102"), common.GetUUID(), mediaExtension(contentType, "png"))
		publicURL, err := uploadUserStorageObject(ctx, cfg, key, contentType, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("user storage: image %d not archived: %s", i, err.Error()))
			continue
		}
		if newOut, setErr := sjson.SetBytes(out, fmt.Sprintf("data.%d.url", i), publicURL); setErr == nil {
			out = newOut
			archived++
		}
	}
	if archived > 0 {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("user storage: archived %d image(s) for user %d", archived, userId))
	}
	return out
}

// loadImageArchiveSource 取单个 data[i] 的图片字节与 MIME:优先 url(http 直链下载 / data: 解码),
// 其次 b64_json 解码;两者皆空时返回 (nil, "", nil) 表示无可归档内容。
func loadImageArchiveSource(ctx context.Context, item gjson.Result) ([]byte, string, error) {
	if rawURL := strings.TrimSpace(item.Get("url").String()); rawURL != "" {
		if strings.HasPrefix(rawURL, "data:") {
			return decodeBase64DataURL(rawURL, maxUserStorageImageBytes)
		}
		return fetchRemoteMedia(ctx, rawURL, maxUserStorageImageBytes)
	}
	if b64 := item.Get("b64_json").String(); b64 != "" {
		raw, err := decodeBase64Payload(b64)
		if err != nil {
			return nil, "", fmt.Errorf("decode b64_json: %w", err)
		}
		if int64(len(raw)) > maxUserStorageImageBytes {
			return nil, "", fmt.Errorf("image size %d exceeds limit %d", len(raw), maxUserStorageImageBytes)
		}
		return raw, http.DetectContentType(raw), nil
	}
	return nil, "", nil
}

// ArchiveTaskVideoToUserStorage 任务成功时把视频结果转存至用户个人桶,成功后改写
// task.PrivateData.ResultURL 并置 StorageArchived(只改内存,由调用方随任务落库持久化)。
// 覆盖三类来源:data: URI(Vertex 内联 base64)、公开直链(Kling/阿里/豆包等)、
// generativelanguage.googleapis.com 直链(自动携带任务私存的 Gemini key)。失败保持原状(fail-open)。
func ArchiveTaskVideoToUserStorage(ctx context.Context, task *model.Task, rawURL string) {
	if task == nil || task.PrivateData.StorageArchived {
		return
	}
	cfg := GetUserStorageConfig(task.UserId)
	if cfg == nil {
		return
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	var body io.Reader
	var length int64
	var contentType string

	if strings.HasPrefix(rawURL, "data:") {
		data, ct, err := decodeBase64DataURL(rawURL, maxUserStorageBufferBytes)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("user storage: video %s not archived: %s", task.TaskID, err.Error()))
			return
		}
		body, length, contentType = bytes.NewReader(data), int64(len(data)), ct
	} else {
		resp, err := openRemoteTaskVideo(ctx, task, rawURL)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("user storage: video %s not archived: %s", task.TaskID, err.Error()))
			return
		}
		defer resp.Body.Close()
		contentType = resp.Header.Get("Content-Type")
		if resp.ContentLength >= 0 {
			if resp.ContentLength > maxUserStorageVideoBytes {
				logger.LogWarn(ctx, fmt.Sprintf("user storage: video %s size %d exceeds limit %d", task.TaskID, resp.ContentLength, maxUserStorageVideoBytes))
				return
			}
			body, length = resp.Body, resp.ContentLength
		} else {
			data, err := io.ReadAll(io.LimitReader(resp.Body, maxUserStorageBufferBytes+1))
			if err != nil {
				logger.LogWarn(ctx, fmt.Sprintf("user storage: video %s not archived: %s", task.TaskID, err.Error()))
				return
			}
			if int64(len(data)) > maxUserStorageBufferBytes {
				logger.LogWarn(ctx, fmt.Sprintf("user storage: video %s exceeds buffer limit %d", task.TaskID, maxUserStorageBufferBytes))
				return
			}
			body, length = bytes.NewReader(data), int64(len(data))
		}
	}

	if contentType == "" || strings.HasPrefix(contentType, "application/octet-stream") || strings.HasPrefix(contentType, "text/") {
		contentType = "video/mp4"
	}
	key := fmt.Sprintf("new-api/videos/%s/%s.%s", time.Now().UTC().Format("20060102"), task.TaskID, mediaExtension(contentType, "mp4"))
	publicURL, err := uploadUserStorageObject(ctx, cfg, key, contentType, body, length)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("user storage: video %s not archived: %s", task.TaskID, err.Error()))
		return
	}
	task.PrivateData.ResultURL = publicURL
	task.PrivateData.StorageArchived = true
	logger.LogInfo(ctx, fmt.Sprintf("user storage: archived video for task %s to %s", task.TaskID, publicURL))
}

// openRemoteTaskVideo 经 SSRF 防护打开视频直链;generativelanguage 域名自动携带任务私存的 key。
func openRemoteTaskVideo(ctx context.Context, task *model.Task, rawURL string) (*http.Response, error) {
	if err := ValidateSSRFProtectedFetchURL(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if u, parseErr := url.Parse(rawURL); parseErr == nil &&
		strings.EqualFold(u.Hostname(), "generativelanguage.googleapis.com") && task.PrivateData.Key != "" {
		req.Header.Set("x-goog-api-key", task.PrivateData.Key)
	}
	resp, err := GetSSRFProtectedHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("fetch video returned status %d", resp.StatusCode)
	}
	return resp, nil
}

// fetchRemoteMedia 经 SSRF 防护拉取媒体内容,超限即中止。
func fetchRemoteMedia(ctx context.Context, rawURL string, maxBytes int64) ([]byte, string, error) {
	if err := ValidateSSRFProtectedFetchURL(rawURL); err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := GetSSRFProtectedHTTPClient().Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("fetch media returned status %d", resp.StatusCode)
	}
	if resp.ContentLength > maxBytes {
		return nil, "", fmt.Errorf("media size %d exceeds limit %d", resp.ContentLength, maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > maxBytes {
		return nil, "", fmt.Errorf("media exceeds limit %d bytes", maxBytes)
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" || strings.HasPrefix(contentType, "application/octet-stream") {
		contentType = http.DetectContentType(data)
	}
	return data, contentType, nil
}

// decodeBase64DataURL 解析 data:<mime>;base64,<payload> 为字节与 MIME,超限报错。
func decodeBase64DataURL(dataURL string, maxBytes int64) ([]byte, string, error) {
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 {
		return nil, "", errors.New("invalid data url")
	}
	header := parts[0]
	if !strings.HasPrefix(header, "data:") || !strings.Contains(header, ";base64") {
		return nil, "", errors.New("unsupported data url")
	}
	if int64(len(parts[1]))/4*3 > maxBytes {
		return nil, "", fmt.Errorf("data url exceeds limit %d bytes", maxBytes)
	}
	raw, err := decodeBase64Payload(parts[1])
	if err != nil {
		return nil, "", err
	}
	mimeType := strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")
	if mimeType == "" {
		mimeType = http.DetectContentType(raw)
	}
	return raw, mimeType, nil
}

// decodeBase64Payload 兼容有/无 padding 的 base64。
func decodeBase64Payload(payload string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err == nil {
		return raw, nil
	}
	return base64.RawStdEncoding.DecodeString(payload)
}

// mediaExtension 由 MIME 推导文件扩展名,未知时用 fallback。
func mediaExtension(contentType, fallback string) string {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	switch ct {
	case "image/png":
		return "png"
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/webp":
		return "webp"
	case "image/gif":
		return "gif"
	case "video/mp4":
		return "mp4"
	case "video/webm":
		return "webm"
	case "video/quicktime":
		return "mov"
	}
	return fallback
}
