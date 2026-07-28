package dto

// UserStorageSetting 用户个人 S3 兼容存储桶配置（R2/OSS/COS/AWS S3/MinIO 等）。
// 配置后,API 与站内请求产出的图片、视频会转存到该桶长期留存。
type UserStorageSetting struct {
	Endpoint     string `json:"endpoint"`                // S3 API 地址,如 https://<accountid>.r2.cloudflarestorage.com
	Bucket       string `json:"bucket"`                  // 桶名
	Region       string `json:"region,omitempty"`        // 区域,选填(R2 默认 auto)
	AccessKeyID  string `json:"access_key_id"`           // AccessKey ID
	SecretKey    string `json:"secret_key"`              // SecretKey
	PublicDomain string `json:"public_domain,omitempty"` // 桶绑定的公开访问域名,选填
	PathStyle    bool   `json:"path_style,omitempty"`    // path-style 寻址(MinIO 等);保存验证时自动探测
}

type UserSetting struct {
	NotifyType                       string  `json:"notify_type,omitempty"`                          // QuotaWarningType 额度预警类型
	QuotaWarningThreshold            float64 `json:"quota_warning_threshold,omitempty"`              // QuotaWarningThreshold 额度预警阈值
	WebhookUrl                       string  `json:"webhook_url,omitempty"`                          // WebhookUrl webhook地址
	WebhookSecret                    string  `json:"webhook_secret,omitempty"`                       // WebhookSecret webhook密钥
	NotificationEmail                string  `json:"notification_email,omitempty"`                   // NotificationEmail 通知邮箱地址
	BarkUrl                          string  `json:"bark_url,omitempty"`                             // BarkUrl Bark推送URL
	GotifyUrl                        string  `json:"gotify_url,omitempty"`                           // GotifyUrl Gotify服务器地址
	GotifyToken                      string  `json:"gotify_token,omitempty"`                         // GotifyToken Gotify应用令牌
	GotifyPriority                   int     `json:"gotify_priority"`                                // GotifyPriority Gotify消息优先级
	UpstreamModelUpdateNotifyEnabled bool    `json:"upstream_model_update_notify_enabled,omitempty"` // 是否接收上游模型更新定时检测通知（仅管理员）
	AcceptUnsetRatioModel            bool    `json:"accept_unset_model_ratio_model,omitempty"`       // AcceptUnsetRatioModel 是否接受未设置价格的模型
	RecordIpLog                      bool    `json:"record_ip_log,omitempty"`                        // 是否记录请求和错误日志IP
	SidebarModules                   string  `json:"sidebar_modules,omitempty"`                      // SidebarModules 左侧边栏模块配置
	BillingPreference                string  `json:"billing_preference,omitempty"`                   // BillingPreference 扣费策略（订阅/钱包）
	Language                         string  `json:"language,omitempty"`                             // Language 用户语言偏好 (zh, en)

	Storage *UserStorageSetting `json:"storage,omitempty"` // Storage 个人存储桶配置

	// >>> jzlh-sub 子账号权限/元信息（仅子账号 parent_id>0 时有意义）
	SubAccount *SubAccountSetting `json:"sub_account,omitempty"`
	// <<< jzlh-sub
}

// >>> jzlh-sub
// SubAccountSetting 子账号的功能权限白名单 + 管理元信息（存 users.setting JSON，零 DDL）。
// 权限用固定功能开关白名单（字符串 key → bool），不做独立权限表/权限 id。
type SubAccountSetting struct {
	Note               string          `json:"note,omitempty"`                 // 备注
	RolePreset         string          `json:"role_preset,omitempty"`          // "user"（普通）| "admin"（管理员）
	Permissions        map[string]bool `json:"permissions,omitempty"`          // 功能开关白名单
	InitialPasswordEnc string          `json:"initial_password_enc,omitempty"` // 初始密码密文（AES-GCM，供主号查看/复制）
}

// 子账号功能权限 key（对齐 fork 侧栏模块，非照搬 302）。
const (
	SubPermPlayground     = "playground"      // Playground/对话
	SubPermApiKeys        = "api_keys"        // 建管 API Keys
	SubPermUsageLogs      = "usage_logs"      // 用量/任务日志
	SubPermWallet         = "wallet"          // 充值（仅管理员子号可授予；入主号钱包）
	SubPermTeamManagement = "team_management" // 团队管理（仅管理员子号可授予）
)

// 子账号预设。
const (
	SubRolePresetUser  = "user"
	SubRolePresetAdmin = "admin"
)
// <<< jzlh-sub

var (
	NotifyTypeEmail   = "email"   // Email 邮件
	NotifyTypeWebhook = "webhook" // Webhook
	NotifyTypeBark    = "bark"    // Bark 推送
	NotifyTypeGotify  = "gotify"  // Gotify 推送
)
