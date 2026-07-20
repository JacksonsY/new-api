package s3lite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func baseConfig() Config {
	return Config{
		Endpoint:    "https://accountid.r2.cloudflarestorage.com",
		Bucket:      "my-bucket",
		AccessKeyID: "ak",
		SecretKey:   "sk",
	}
}

func TestRequestURLStyles(t *testing.T) {
	cfg := baseConfig()

	virtual, err := cfg.RequestURL("new-api/videos/task_1.mp4")
	require.NoError(t, err)
	assert.Equal(t, "https://my-bucket.accountid.r2.cloudflarestorage.com/new-api/videos/task_1.mp4", virtual)

	cfg.PathStyle = true
	path, err := cfg.RequestURL("new-api/videos/task_1.mp4")
	require.NoError(t, err)
	assert.Equal(t, "https://accountid.r2.cloudflarestorage.com/my-bucket/new-api/videos/task_1.mp4", path)
}

func TestRequestURLKeepsEndpointBasePathAndEscapesKey(t *testing.T) {
	cfg := baseConfig()
	cfg.Endpoint = "http://minio.internal:9000/prefix/"
	cfg.PathStyle = true

	got, err := cfg.RequestURL("a b/c#d.png")
	require.NoError(t, err)
	assert.Equal(t, "http://minio.internal:9000/prefix/my-bucket/a%20b/c%23d.png", got)
}

func TestPublicURLPrefersPublicDomain(t *testing.T) {
	cfg := baseConfig()
	cfg.PublicDomain = "https://cdn.example.com/"

	assert.Equal(t, "https://cdn.example.com/new-api/images/x.png", cfg.PublicURL("new-api/images/x.png"))

	cfg.PublicDomain = ""
	assert.Equal(t, "https://my-bucket.accountid.r2.cloudflarestorage.com/new-api/images/x.png", cfg.PublicURL("new-api/images/x.png"))
}

func TestResolvedRegion(t *testing.T) {
	cfg := baseConfig()
	assert.Equal(t, "auto", cfg.ResolvedRegion(), "R2 端点默认 region=auto")

	cfg.Region = "ap-guangzhou"
	assert.Equal(t, "ap-guangzhou", cfg.ResolvedRegion(), "显式 region 优先")

	cfg.Region = ""
	cfg.Endpoint = "https://cos.ap-guangzhou.myqcloud.com"
	assert.Equal(t, "us-east-1", cfg.ResolvedRegion(), "非 R2 端点默认 us-east-1")
}

func TestValidateRejectsBadConfig(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"missing secret", func(c *Config) { c.SecretKey = "" }},
		{"missing bucket", func(c *Config) { c.Bucket = "" }},
		{"bucket with slash", func(c *Config) { c.Bucket = "a/b" }},
		{"endpoint without scheme", func(c *Config) { c.Endpoint = "accountid.r2.cloudflarestorage.com" }},
		{"endpoint with query", func(c *Config) { c.Endpoint = "https://minio.internal:9000/?x=1" }},
		{"bad public domain", func(c *Config) { c.PublicDomain = "cdn.example.com" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			tt.mutate(&cfg)
			assert.Error(t, cfg.Validate())
		})
	}

	valid := baseConfig()
	valid.PublicDomain = "https://cdn.example.com"
	require.NoError(t, valid.Validate())
}
