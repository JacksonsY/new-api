package common

import (
	"crypto/tls"
	//"os"
	//"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

var StartTime = time.Now().Unix() // unit: second
var Version = "v0.0.0"            // this hard coding will be replaced automatically when building, no need to manually change
var SystemName = "New API"
var Footer = ""
var Logo = ""
var TopUpLink = ""

var themeValue atomic.Value // stores string; safe for concurrent read/write

func init() {
	themeValue.Store("classic")
}

func GetTheme() string {
	return themeValue.Load().(string)
}

// SetTheme updates the frontend theme atomically.
// Only "default" and "classic" are accepted; other values are silently ignored.
func SetTheme(t string) {
	if t == "default" || t == "classic" {
		themeValue.Store(t)
	}
}

// ThemeAwarePath rewrites legacy /console/* paths to the default-theme
// equivalents when the active theme is "default".  For "classic" (or any
// other theme) the path is returned unchanged.  The function only touches
// known prefixes so it is safe to call with arbitrary suffixes and query
// strings.
func ThemeAwarePath(suffix string) string {
	if GetTheme() != "default" {
		return suffix
	}
	switch {
	case strings.HasPrefix(suffix, "/console/topup"):
		return strings.Replace(suffix, "/console/topup", "/wallet", 1)
	case strings.HasPrefix(suffix, "/console/log"):
		return strings.Replace(suffix, "/console/log", "/usage-logs", 1)
	case strings.HasPrefix(suffix, "/console/personal"):
		return strings.Replace(suffix, "/console/personal", "/profile", 1)
	}
	return suffix
}

// var ChatLink = ""
// var ChatLink2 = ""
var QuotaPerUnit = 500 * 1000.0 // $0.002 / 1K tokens
// 保留旧变量以兼容历史逻辑，实际展示由 general_setting.quota_display_type 控制
var DisplayInCurrencyEnabled = true
var DisplayTokenStatEnabled = true
var DrawingEnabled = true
var TaskEnabled = true
var DataExportEnabled = true
var DataExportInterval = 5         // unit: minute
var DataExportDefaultTime = "hour" // unit: minute
var DefaultCollapseSidebar = false // default value of collapse sidebar

// Any options with "Secret", "Token" in its key won't be return by GetOptions

var SessionSecret = uuid.New().String()
var CryptoSecret = uuid.New().String()
var SessionCookieSecure = false
var SessionCookieTrustedURLs []string

var OptionMap map[string]string
var OptionMapRWMutex sync.RWMutex

var ItemsPerPage = 10
var MaxRecentItems = 1000

var PasswordLoginEnabled = true
var PasswordRegisterEnabled = true
var EmailVerificationEnabled = false
var GitHubOAuthEnabled = false
var LinuxDOOAuthEnabled = false
var WeChatAuthEnabled = false
var TelegramOAuthEnabled = false
var TurnstileCheckEnabled = false
var RegisterEnabled = true

var EmailDomainRestrictionEnabled = false // 是否启用邮箱域名限制
var EmailAliasRestrictionEnabled = false  // 是否启用邮箱别名限制
var EmailDomainWhitelist = []string{
	"gmail.com",
	"163.com",
	"126.com",
	"qq.com",
	"outlook.com",
	"hotmail.com",
	"icloud.com",
	"yahoo.com",
	"foxmail.com",
}
var EmailLoginAuthServerList = []string{
	"smtp.sendcloud.net",
	"smtp.azurecomm.net",
}

var DebugEnabled bool
var MemoryCacheEnabled bool

var LogConsumeEnabled = true

var TLSInsecureSkipVerify bool
var InsecureTLSConfig = &tls.Config{InsecureSkipVerify: true}

var SMTPServer = ""
var SMTPPort = 587
var SMTPSSLEnabled = false
var SMTPStartTLSEnabled = false
var SMTPInsecureSkipVerify = false
var SMTPForceAuthLogin = false
var SMTPAccount = ""
var SMTPFrom = ""
var SMTPToken = ""

var GitHubClientId = ""
var GitHubClientSecret = ""

// GitHubMinimumAccountAgeSeconds GitHub 账号年龄门禁:OAuth 注册/绑定时,GitHub 账号创建距今
// 不足该秒数则拒绝(防批量新开小号薅注册/邀请赠额)。0 = 关闭;仅对 GitHub provider 生效。
var GitHubMinimumAccountAgeSeconds int64 = 0

var LinuxDOClientId = ""
var LinuxDOClientSecret = ""
var LinuxDOMinimumTrustLevel = 0

var WeChatServerAddress = ""
var WeChatServerToken = ""
var WeChatAccountQRCodeImageURL = ""

var TurnstileSiteKey = ""
var TurnstileSecretKey = ""

var TelegramBotToken = ""
var TelegramBotName = ""

var QuotaForNewUser = 0
var QuotaForInviter = 0
var QuotaForInvitee = 0
var ChannelDisableThreshold = 5.0

// fork 默认开启渠道自动禁用/启用：坏渠道(401/关键字命中)自动下线，
// 配合 monitor_setting 的 passive_recovery 定时测活自动复活。管理员可在
// 系统设置→模型→路由与可靠性 关闭。
var AutomaticDisableChannelEnabled = true
var AutomaticEnableChannelEnabled = true
var QuotaRemindThreshold = 1000
var PreConsumedQuota = 500

// jzlh v2 P2 招商模块开关:只拦"入驻申请"的增量入口,存量代理/供应商照常
// 计费与结算。默认开,向后兼容已上线部署;运营可在系统设置关闭对外招商。
var AgentEnabled = true
var SupplierEnabled = true

// jzlh-agent v2 P2 代理审批默认分润比例(0-1):审批弹窗预填,减少逐单手填。
var AgentDefaultProfitRate float64 = 0

// jzlh-supplier v2 P2 参数配置化(对齐代理模块先例,原为 model 包硬编码常量):
// 供应商收益成熟期(天)与报价率硬上限(审批+改价申请双封顶,防平台毛利归零)。
var SupplierMatureDays = 3
var SupplierMaxRate float64 = 1.0

// jzlh-agent 代理分润成熟期（分钟）。0 = 即时到账；>0 时新分润先进入
// 待成熟状态，超过该时长后才计入可提现余额（兜住退款/撤销窗口，防刷）。
var AgentCommissionMatureMinutes = 0

// jzlh-agent 提现手续费比例(0-1)，申请时快照进提现单的 fee 字段。
var AgentWithdrawFeeRate float64 = 0

// jzlh-agent 最低提现额度(quota)。默认 500000 = $1(按 QuotaPerUnit=500k)。
var AgentWithdrawMinQuota = 500000

// jzlh-agent 单个代理同时存在的待审核提现单上限，防刷单轰炸审核列表。
var AgentWithdrawMaxPending = 3

// jzlh-agent 资格门槛：下级注册满 N 天其消费才开始计佣，抬高批量刷小号成本。0 = 关闭。
var AgentInviteeMinAgeDays = 0

// jzlh-agent 反欺诈 IP 快表(user_ip_records)保留天数，超期由每日清理任务删除。
// 检测窗口最长按 90 天算，默认 180 天留足余量。
var AgentIPRecordRetentionDays = 180

// 蓝图A 渠道余额告警：预计剩余天数低于该阈值时通知 root。0 = 关闭。
var ChannelBalanceAlertThresholdDays = 0

// 蓝图A 渠道余额告警检查间隔（分钟），走 system task 调度（多主去重+运行历史）。
var ChannelBalanceAlertIntervalMinutes = 1440

// fork 默认开启跨渠道重试(上游默认 0=不重试)。重试按优先级逐层降级，
// 3 次可覆盖三层渠道布防；错误是否可重试由 AutomaticRetryStatusCodeRanges 决定。
var RetryTimes = 3

//var RootUserEmail = ""

var IsMasterNode bool

const (
	NodeNameSourceManual   = "manual"
	NodeNameSourceHostname = "hostname"
)

// NodeName 节点名称，优先从 NODE_NAME 环境变量读取，未配置时回退主机名。
// 用于审计日志和后台任务中标识节点身份；多实例部署时建议显式配置稳定 NODE_NAME。
var NodeName = ""

// NodeNameSource records how NodeName was chosen so future instance-management
// reporting can distinguish operator-configured names from automatic fallback.
var NodeNameSource = NodeNameSourceHostname

var NodeNameManuallyConfigured bool

var requestInterval int
var RequestInterval time.Duration

var SyncFrequency int // unit is second

var BatchUpdateEnabled = false
var BatchUpdateInterval int

var RelayTimeout int // unit is second

var RelayIdleConnTimeout int // unit is second
var RelayMaxIdleConns int
var RelayMaxIdleConnsPerHost int

var GeminiSafetySetting string

// https://docs.cohere.com/docs/safety-modes Type; NONE/CONTEXTUAL/STRICT
var CohereSafetySetting string

const (
	RequestIdKey         = "X-Oneapi-Request-Id"
	UpstreamRequestIdKey = "X-Upstream-Request-Id"
)

const (
	RoleGuestUser  = 0
	RoleCommonUser = 1
	RoleAdminUser  = 10
	RoleRootUser   = 100
)

func IsValidateRole(role int) bool {
	return role == RoleGuestUser || role == RoleCommonUser || role == RoleAdminUser || role == RoleRootUser
}

var (
	FileUploadPermission    = RoleGuestUser
	FileDownloadPermission  = RoleGuestUser
	ImageUploadPermission   = RoleGuestUser
	ImageDownloadPermission = RoleGuestUser
)

// All duration's unit is seconds
// Shouldn't larger then RateLimitKeyExpirationDuration
var (
	GlobalApiRateLimitEnable   bool
	GlobalApiRateLimitNum      int
	GlobalApiRateLimitDuration int64

	GlobalWebRateLimitEnable   bool
	GlobalWebRateLimitNum      int
	GlobalWebRateLimitDuration int64

	CriticalRateLimitEnable   bool
	CriticalRateLimitNum            = 20
	CriticalRateLimitDuration int64 = 20 * 60

	UploadRateLimitNum            = 10
	UploadRateLimitDuration int64 = 60

	DownloadRateLimitNum            = 10
	DownloadRateLimitDuration int64 = 60

	// Per-user search rate limit (applies after authentication, keyed by user ID)
	SearchRateLimitEnable         = true
	SearchRateLimitNum            = 10
	SearchRateLimitDuration int64 = 60
)

var RateLimitKeyExpirationDuration = 20 * time.Minute

const (
	UserStatusEnabled  = 1 // don't use 0, 0 is the default value!
	UserStatusDisabled = 2 // also don't use 0
)

const (
	TokenStatusEnabled   = 1 // don't use 0, 0 is the default value!
	TokenStatusDisabled  = 2 // also don't use 0
	TokenStatusExpired   = 3
	TokenStatusExhausted = 4
)

const (
	RedemptionCodeStatusEnabled  = 1 // don't use 0, 0 is the default value!
	RedemptionCodeStatusDisabled = 2 // also don't use 0
	RedemptionCodeStatusUsed     = 3 // also don't use 0
)

const (
	ChannelStatusUnknown          = 0
	ChannelStatusEnabled          = 1 // don't use 0, 0 is the default value!
	ChannelStatusManuallyDisabled = 2 // also don't use 0
	ChannelStatusAutoDisabled     = 3
)

const (
	TopUpStatusPending = "pending"
	TopUpStatusSuccess = "success"
	TopUpStatusFailed  = "failed"
	TopUpStatusExpired = "expired"
)
