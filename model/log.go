package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

func applyExplicitLogTextFilter(tx *gorm.DB, column string, value string) (*gorm.DB, error) {
	if value == "" {
		return tx, nil
	}
	if strings.Contains(value, "%") {
		condition, pattern, err := buildLogLikeCondition(column, value)
		if err != nil {
			return nil, err
		}
		return tx.Where(condition, pattern), nil
	}
	return tx.Where(column+" = ?", value), nil
}

func buildLogLikeCondition(column string, value string) (string, string, error) {
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		pattern, err := sanitizeClickHouseLikePattern(value)
		if err != nil {
			return "", "", err
		}
		return column + " LIKE ?", pattern, nil
	}

	pattern, err := sanitizeLikePattern(value)
	if err != nil {
		return "", "", err
	}
	return column + " LIKE ? ESCAPE '!'", pattern, nil
}

func sanitizeClickHouseLikePattern(input string) (string, error) {
	input = strings.ReplaceAll(input, `\`, `\\`)
	input = strings.ReplaceAll(input, `_`, `\_`)

	if err := validateLikePattern(input); err != nil {
		return "", err
	}
	return input, nil
}

type Log struct {
	Id     int `json:"id" gorm:"index:idx_created_at_id,priority:2;index:idx_user_id_id,priority:2"`
	UserId int `json:"user_id" gorm:"index;index:idx_user_id_id,priority:1"`
	// idx_logs_channel_usage (type, channel_id, created_at, quota)：渠道消耗聚合的
	// 覆盖索引（蓝图A 余额告警/剩余天数估算）。type+channel_id 等值定位、
	// created_at 范围扫、quota 免回表；不建则两个聚合在大表上全表扫。
	CreatedAt        int64  `json:"created_at" gorm:"bigint;index:idx_created_at_id,priority:1;index:idx_created_at_type;index:idx_logs_channel_usage,priority:3"`
	Type             int    `json:"type" gorm:"index:idx_created_at_type;index:idx_logs_channel_usage,priority:1"`
	Content          string `json:"content"`
	Username         string `json:"username" gorm:"index;index:index_username_model_name,priority:2;default:''"`
	TokenName        string `json:"token_name" gorm:"index;default:''"`
	ModelName        string `json:"model_name" gorm:"index;index:index_username_model_name,priority:1;default:''"`
	Quota            int    `json:"quota" gorm:"default:0;index:idx_logs_channel_usage,priority:4"`
	PromptTokens     int    `json:"prompt_tokens" gorm:"default:0"`
	CompletionTokens int    `json:"completion_tokens" gorm:"default:0"`
	UseTime          int    `json:"use_time" gorm:"default:0"`
	IsStream         bool   `json:"is_stream"`
	ChannelId        int    `json:"channel" gorm:"index;index:idx_logs_channel_usage,priority:2"`
	ChannelName      string `json:"channel_name" gorm:"->"`
	// ChannelRatio 渠道计费倍率快照：仅供管理员维度的渠道成本统计，不影响用户扣费。
	// 默认在写入时显式赋值（GetChannelRatio 兜底），不用 gorm default 标签。
	// omitempty：用户侧路径经 formatUserLogs 清零后，该字段整个不出现在响应里
	// ——成本倍率是经营机密，普通用户连字段名都不该看到。
	ChannelRatio    float64 `json:"channel_ratio,omitempty"`
	ChannelRatioSet bool    `json:"-"`
	// ChannelQuota 渠道成本快照：原始费用（实付 ÷ 生效分组倍率）× 渠道倍率，
	// QuotaRound 后落库。物化是必须的——分组倍率埋在 other JSON 里，渠道支出
	// SQL 聚合取不到。旧行为 0/NULL 时聚合回退 quota×channel_ratio 旧口径。
	// 同为管理员维度信息：formatUserLogs 对普通用户清零，omitempty 隐藏字段名。
	ChannelQuota int `json:"channel_quota,omitempty" gorm:"default:0"`
	// SupplierId 渠道所属供应商快照（写日志时从渠道 owner 取，0=平台自营）。
	// 供应商结算按此聚合；快照语义使渠道日后转手历史收益仍归原供应商。
	// 同为管理员维度：formatUserLogs 对普通用户清零，omitempty 隐藏字段名。
	SupplierId        int    `json:"supplier_id,omitempty" gorm:"default:0;index"`
	TokenId           int    `json:"token_id" gorm:"default:0;index"`
	Group             string `json:"group" gorm:"index"`
	Ip                string `json:"ip" gorm:"index;default:''"`
	RequestId         string `json:"request_id,omitempty" gorm:"type:varchar(64);index:idx_logs_request_id;default:''"`
	UpstreamRequestId string `json:"upstream_request_id,omitempty" gorm:"type:varchar(128);index:idx_logs_upstream_request_id;default:''"`
	Other             string `json:"other"`
}

// don't use iota, avoid change log type value
const (
	LogTypeUnknown = 0
	LogTypeTopup   = 1
	LogTypeConsume = 2
	LogTypeManage  = 3
	LogTypeSystem  = 4
	LogTypeError   = 5
	LogTypeRefund  = 6
	LogTypeLogin   = 7
)

func ensureLogRequestId(log *Log) {
	if log != nil && log.RequestId == "" {
		log.RequestId = common.NewRequestId()
	}
}

func createLog(log *Log) error {
	ensureLogRequestId(log)
	return LOG_DB.Create(log).Error
}

func clickHouseLogOrder(prefix string) string {
	return prefix + "created_at desc, " + prefix + "request_id desc"
}

func assignDisplayLogIds(logs []*Log, startIdx int) {
	for i := range logs {
		logs[i].Id = startIdx + i + 1
	}
}

func formatUserLogs(logs []*Log, startIdx int) {
	for i := range logs {
		logs[i].ChannelName = ""
		// 渠道计费倍率/成本快照属于管理员维度信息，对普通用户隐藏
		logs[i].ChannelRatio = 0
		logs[i].ChannelQuota = 0
		logs[i].SupplierId = 0
		var otherMap map[string]interface{}
		otherMap, _ = common.StrToMap(logs[i].Other)
		if otherMap != nil {
			// Remove admin-only debug fields.
			delete(otherMap, "admin_info")
			// Remove operation-audit details (operator/route info), admin-only.
			delete(otherMap, "audit_info")
			// delete(otherMap, "reject_reason")
			delete(otherMap, "stream_status")
			// 上游真实模型名属于供应链信息，普通用户不可见（管理员日志视图保留）
			delete(otherMap, "upstream_model_name")
			delete(otherMap, "is_model_mapped")
		}
		logs[i].Other = common.MapToJsonStr(otherMap)
	}
	assignDisplayLogIds(logs, startIdx)
}

func GetLogByTokenId(tokenId int) (logs []*Log, err error) {
	order := "id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("")
	}
	err = LOG_DB.Model(&Log{}).Where("token_id = ?", tokenId).Order(order).Limit(common.MaxRecentItems).Find(&logs).Error
	formatUserLogs(logs, 0)
	return logs, err
}

func RecordLog(userId int, logType int, content string) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	err := createLog(log)
	if err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

// RecordLogWithAdminInfo 记录操作日志，并将管理员相关信息存入 Other.admin_info，
func RecordLogWithAdminInfo(userId int, logType int, content string, adminInfo map[string]interface{}) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	if len(adminInfo) > 0 {
		other := map[string]interface{}{
			"admin_info": adminInfo,
		}
		log.Other = common.MapToJsonStr(other)
	}
	if err := createLog(log); err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

// buildOpField 构建语言无关的操作描述（写入 Other.op）。
// 前端依据 action(稳定操作标识) + params(结构化参数) 在渲染期用 i18n 本地化展示，
// 因此不在数据库中存储自然语言句子。
func buildOpField(action string, params map[string]interface{}) map[string]interface{} {
	op := map[string]interface{}{
		"action": action,
	}
	if len(params) > 0 {
		op["params"] = params
	}
	return op
}

// RecordLoginLog 记录用户登录成功的审计日志（type=LogTypeLogin）。
// username 由调用方传入（登录流程已持有用户对象），避免额外的数据库查询。
// content 为英文兜底文本（用于导出/经典前端）；action+params 供前端本地化渲染。
// extra 可携带 login_method、user_agent 等附加信息（普通用户可见）。
func RecordLoginLog(userId int, username string, content string, ip string, action string, params map[string]interface{}, extra map[string]interface{}) {
	other := map[string]interface{}{}
	for k, v := range extra {
		other[k] = v
	}
	other["op"] = buildOpField(action, params)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeLogin,
		Content:   content,
		Ip:        ip,
		Other:     common.MapToJsonStr(other),
	}
	if err := createLog(log); err != nil {
		common.SysLog("failed to record login log: " + err.Error())
	}
}

// RecordOperationAuditLog 记录管理/高危操作审计日志（type=LogTypeManage）。
// logUserId 为日志归属者，管理审计日志应归属实际操作者；目标资源/用户放入
// action params。username 内部按 logUserId 查询。content 为英文兜底文本（导出/经典前端用）。
// action+params 写入 Other.op，供前端本地化渲染（普通用户可见，不含敏感信息）。
// adminInfo 存放操作者身份（写入 Other.admin_info，普通用户查询时剥离）；
// auditInfo 存放路由/方法/结果等中间件兜底信息（写入 Other.audit_info，普通用户查询时剥离）。
func RecordOperationAuditLog(logUserId int, content string, ip string, action string, params map[string]interface{}, adminInfo map[string]interface{}, auditInfo map[string]interface{}) {
	username, _ := GetUsernameById(logUserId, false)
	other := map[string]interface{}{
		"op": buildOpField(action, params),
	}
	if len(adminInfo) > 0 {
		other["admin_info"] = adminInfo
	}
	if len(auditInfo) > 0 {
		other["audit_info"] = auditInfo
	}
	log := &Log{
		UserId:    logUserId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeManage,
		Content:   content,
		Ip:        ip,
		Other:     common.MapToJsonStr(other),
	}
	if err := createLog(log); err != nil {
		common.SysLog("failed to record operation audit log: " + err.Error())
	}
}

func RecordTopupLog(userId int, content string, callerIp string, paymentMethod string, callbackPaymentMethod string) {
	username, _ := GetUsernameById(userId, false)
	adminInfo := map[string]interface{}{
		"server_ip":               common.GetIp(),
		"node_name":               common.NodeName,
		"caller_ip":               callerIp,
		"payment_method":          paymentMethod,
		"callback_payment_method": callbackPaymentMethod,
		"version":                 common.Version,
	}
	other := map[string]interface{}{
		"admin_info": adminInfo,
	}
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeTopup,
		Content:   content,
		Ip:        callerIp,
		Other:     common.MapToJsonStr(other),
	}
	err := createLog(log)
	if err != nil {
		common.SysLog("failed to record topup log: " + err.Error())
	}
}

func RecordErrorLog(c *gin.Context, userId int, channelId int, modelName string, tokenName string, content string, tokenId int, useTimeSeconds int,
	isStream bool, group string, other map[string]interface{}) {
	logger.LogInfo(c, fmt.Sprintf("record error log: userId=%d, channelId=%d, modelName=%s, tokenName=%s, content=%s", userId, channelId, modelName, tokenName, common.LocalLogPreview(content)))
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	upstreamRequestId := c.GetString(common.UpstreamRequestIdKey)
	otherStr := common.MapToJsonStr(other)
	// 判断是否需要记录 IP
	needRecordIp := false
	if settingMap, err := GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        common.GetTimestamp(),
		Type:             LogTypeError,
		Content:          content,
		PromptTokens:     0,
		CompletionTokens: 0,
		TokenName:        tokenName,
		ModelName:        modelName,
		Quota:            0,
		ChannelId:        channelId,
		TokenId:          tokenId,
		UseTime:          useTimeSeconds,
		IsStream:         isStream,
		Group:            group,
		Ip: func() string {
			if needRecordIp {
				return c.ClientIP()
			}
			return ""
		}(),
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		Other:             otherStr,
	}
	err := createLog(log)
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	}
}

type RecordConsumeLogParams struct {
	ChannelId        int                    `json:"channel_id"`
	PromptTokens     int                    `json:"prompt_tokens"`
	CompletionTokens int                    `json:"completion_tokens"`
	// TotalTokens 用于汇总统计(quota_data.token_used)。Anthropic 语义下
	// PromptTokens 不含缓存 token，需把 cache_read/cache_creation 加回后写入此处，
	// 避免数据看板总 TOKEN 数漏算缓存；OpenAI 语义下与 Prompt+Completion 相同。
	// 缺省 0 时 RecordConsumeLog 回退 Prompt+Completion，保持旧调用点兼容。
	TotalTokens      int                    `json:"total_tokens"`
	ModelName        string                 `json:"model_name"`
	TokenName        string                 `json:"token_name"`
	Quota            int                    `json:"quota"`
	Content          string                 `json:"content"`
	TokenId          int                    `json:"token_id"`
	UseTimeSeconds   int                    `json:"use_time_seconds"`
	IsStream         bool                   `json:"is_stream"`
	Group            string                 `json:"group"`
	Other            map[string]interface{} `json:"other"`
	// CommissionSourceKey links asynchronous task refunds to the exact initial
	// commission owner. Empty keeps the request-id source used by sync relays.
	CommissionSourceKey         string `json:"-"`
	CommissionSourceKeyRequired bool   `json:"-"`
}

// groupRatioFromLogOther 从日志 other 取该笔消费生效的分组倍率（用户专属倍率
// 生效时 group_ratio 已是覆盖后的值）。缺失或非正值按 1 兜底，等价旧口径。
func groupRatioFromLogOther(other map[string]interface{}) float64 {
	if ratio, ok := other["group_ratio"].(float64); ok && ratio > 0 {
		return ratio
	}
	return 1
}

func RecordConsumeLog(c *gin.Context, userId int, params RecordConsumeLogParams) {
	// >>> jzlh-agent 消费分润（异步，不阻塞主链；独立于日志开关）
	// 幂等键取 request id；必须在 goroutine 外读取，handler 返回后 c 可能被回收。
	if params.Quota > 0 {
		sourceKey := params.CommissionSourceKey
		if sourceKey == "" && !params.CommissionSourceKeyRequired {
			if rid := c.GetString(common.RequestIdKey); rid != "" {
				sourceKey = "consume:" + rid
			}
		}
		if sourceKey != "" {
			quota := params.Quota
			record := func() {
				RecordAgentCommission(userId, quota, sourceKey)
			}
			if params.CommissionSourceKey != "" {
				// Persist ownership before billing publishes the task to the poller,
				// so a fast task failure cannot outrun commission creation.
				record()
			} else {
				gopool.Go(record)
			}
		} else {
			// request id 缺失时没有幂等键，无幂等入账可被重放刷佣：跳过分润并留审计日志。
			missingKey := "request id"
			if params.CommissionSourceKeyRequired {
				missingKey = "task source key"
			}
			common.SysLog(fmt.Sprintf(
				"skip agent commission: missing %s (user=%d quota=%d)", missingKey, userId, params.Quota))
		}
	}
	// <<< jzlh-agent
	if !common.LogConsumeEnabled {
		return
	}
	logger.LogInfo(c, fmt.Sprintf("record consume log: userId=%d, params=%s", userId, common.GetJsonString(params)))
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	upstreamRequestId := c.GetString(common.UpstreamRequestIdKey)
	createdAt := common.GetTimestamp()
	otherStr := common.MapToJsonStr(params.Other)
	// 渠道计费倍率 + 渠道成本快照：用于管理员维度的渠道成本统计，与用户扣费无关。
	// 成本基数为原始费用（实付 ÷ 生效分组倍率），见 channelCostQuota。
	channelRatio := 1.0
	supplierId := 0
	if channel, err := CacheGetChannel(params.ChannelId); err == nil && channel != nil {
		channelRatio = channel.GetChannelRatio()
		supplierId = channel.UserId // 供应商结算快照：渠道 owner，0=平台自营
	}
	channelQuota := channelCostQuota(params.Quota, groupRatioFromLogOther(params.Other), channelRatio)
	// 判断是否需要记录 IP
	needRecordIp := false
	if settingMap, err := GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        createdAt,
		Type:             LogTypeConsume,
		Content:          params.Content,
		PromptTokens:     params.PromptTokens,
		CompletionTokens: params.CompletionTokens,
		TokenName:        params.TokenName,
		ModelName:        params.ModelName,
		Quota:            params.Quota,
		ChannelId:        params.ChannelId,
		ChannelRatio:     channelRatio,
		ChannelRatioSet:  true,
		ChannelQuota:     channelQuota,
		SupplierId:       supplierId,
		TokenId:          params.TokenId,
		UseTime:          params.UseTimeSeconds,
		IsStream:         params.IsStream,
		Group:            params.Group,
		Ip: func() string {
			if needRecordIp {
				return c.ClientIP()
			}
			return ""
		}(),
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		Other:             otherStr,
	}
	err := createLog(log)
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	}
	if common.DataExportEnabled {
		// Anthropic 语义下 PromptTokens 不含缓存，优先用调用方算好的 TotalTokens；
		// 未提供(<=0)时回退 Prompt+Completion，保持旧调用点兼容。
		tokenUsed := params.TotalTokens
		if tokenUsed <= 0 {
			tokenUsed = params.PromptTokens + params.CompletionTokens
		}
		// 渠道维度成本随用量数据一并预聚合进 quota_data，与日志快照同源同口径
		LogQuotaData(QuotaDataLogParams{
			UserID:       userId,
			Username:     username,
			ModelName:    params.ModelName,
			Quota:        params.Quota,
			ChannelQuota: channelQuota,
			CreatedAt:    createdAt,
			TokenUsed:    tokenUsed,
			UseGroup:     params.Group,
			TokenID:      params.TokenId,
			ChannelID:    params.ChannelId,
			NodeName:     common.NodeName,
		})
	}
}

type RecordTaskBillingLogParams struct {
	UserId             int
	LogType            int
	Content            string
	ChannelId          int
	ModelName          string
	Quota              int
	TokenId            int
	Group              string
	Other              map[string]interface{}
	NodeName           string // 任务发起节点；为空时回退当前节点
	CommissionEventKey string // 同一任务内可重放且唯一的计费状态迁移标识
}

func RecordTaskBillingLog(params RecordTaskBillingLogParams) {
	// >>> jzlh-agent 任务消费分润同步结算，保证正差额先于后续全额退款落库；
	// 退款(任务失败/差额下调)按原始归属回冲，防"刷失败任务套佣金"。
	// 幂等键取 task_id；差额结算可用状态迁移键区分额度相同的多次合法记账。
	if params.Quota > 0 {
		taskKey := ""
		if tid, ok := params.Other["task_id"].(string); ok && tid != "" {
			eventKey := params.CommissionEventKey
			if eventKey == "" {
				eventKey = fmt.Sprintf("%d", params.LogType)
			}
			taskKey = BuildTaskCommissionSourceKey(
				params.ChannelId, tid, eventKey, params.Quota,
			)
		}
		userId, quota := params.UserId, params.Quota
		if taskKey == "" {
			// task_id 缺失时没有幂等键，无幂等入账/回冲可被重复触发：跳过并留审计日志。
			if params.LogType == LogTypeConsume || params.LogType == LogTypeRefund {
				common.SysLog(fmt.Sprintf(
					"skip agent commission settle: missing task_id (user=%d type=%d quota=%d)",
					userId, params.LogType, quota))
			}
		} else {
			switch params.LogType {
			case LogTypeConsume:
				RecordAgentCommission(userId, quota, taskKey)
			case LogTypeRefund:
				RecordAgentCommissionReversal(userId, quota, taskKey)
			}
		}
	}
	// <<< jzlh-agent
	if params.LogType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(params.UserId, false)
	tokenName := ""
	if params.TokenId > 0 {
		if token, err := GetTokenById(params.TokenId); err == nil {
			tokenName = token.Name
		}
	}
	createdAt := common.GetTimestamp()
	// 渠道计费倍率快照：用于管理员维度的渠道成本统计，与用户扣费无关。
	channelRatio := 1.0
	supplierId := 0
	if channel, err := CacheGetChannel(params.ChannelId); err == nil && channel != nil {
		channelRatio = channel.GetChannelRatio()
		supplierId = channel.UserId // 供应商结算快照：渠道 owner，0=平台自营
	}
	// 渠道成本快照：基数为原始费用（实付 ÷ 生效分组倍率），见 channelCostQuota。
	channelQuota := channelCostQuota(params.Quota, groupRatioFromLogOther(params.Other), channelRatio)
	log := &Log{
		UserId:          params.UserId,
		Username:        username,
		CreatedAt:       createdAt,
		Type:            params.LogType,
		Content:         params.Content,
		TokenName:       tokenName,
		ModelName:       params.ModelName,
		Quota:           params.Quota,
		ChannelId:       params.ChannelId,
		ChannelRatio:    channelRatio,
		ChannelRatioSet: true,
		ChannelQuota:    channelQuota,
		SupplierId:      supplierId,
		TokenId:         params.TokenId,
		Group:           params.Group,
		Other:           common.MapToJsonStr(params.Other),
	}
	err := createLog(log)
	if err != nil {
		common.SysLog("failed to record task billing log: " + err.Error())
	}
	if params.LogType == LogTypeConsume && common.DataExportEnabled {
		nodeName := params.NodeName
		if nodeName == "" {
			nodeName = common.NodeName
		}
		// 渠道维度成本随用量数据一并预聚合进 quota_data，与日志快照同源同口径
		LogQuotaData(QuotaDataLogParams{
			UserID:       params.UserId,
			Username:     username,
			ModelName:    params.ModelName,
			Quota:        params.Quota,
			ChannelQuota: channelQuota,
			CreatedAt:    createdAt,
			UseGroup:     params.Group,
			TokenID:      params.TokenId,
			ChannelID:    params.ChannelId,
			NodeName:     nodeName,
		})
	}
}

func GetAllLogs(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, startIdx int, num int, channel int, group string, requestId string, upstreamRequestId string) (logs []*Log, total int64, err error) {
	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = LOG_DB
	} else {
		tx = LOG_DB.Where("logs.type = ?", logType)
	}

	if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", modelName); err != nil {
		return nil, 0, err
	}
	if tx, err = applyExplicitLogTextFilter(tx, "logs.username", username); err != nil {
		return nil, 0, err
	}
	if tokenName != "" {
		tx = tx.Where("logs.token_name = ?", tokenName)
	}
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
	}
	if upstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", upstreamRequestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if channel != 0 {
		tx = tx.Where("logs.channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", group)
	}
	err = tx.Model(&Log{}).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	order := "logs.created_at desc, logs.id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("logs.")
	}
	err = tx.Order(order).Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		assignDisplayLogIds(logs, startIdx)
	}

	channelIds := types.NewSet[int]()
	for _, log := range logs {
		if log.ChannelId != 0 {
			channelIds.Add(log.ChannelId)
		}
	}

	if channelIds.Len() > 0 {
		var channels []struct {
			Id   int    `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		if common.MemoryCacheEnabled {
			// Cache get channel
			for _, channelId := range channelIds.Items() {
				if cacheChannel, err := CacheGetChannel(channelId); err == nil {
					channels = append(channels, struct {
						Id   int    `gorm:"column:id"`
						Name string `gorm:"column:name"`
					}{
						Id:   channelId,
						Name: cacheChannel.Name,
					})
				}
			}
		} else {
			// Bulk query channels from DB
			if err = DB.Table("channels").Select("id, name").Where("id IN ?", channelIds.Items()).Find(&channels).Error; err != nil {
				return logs, total, err
			}
		}
		channelMap := make(map[int]string, len(channels))
		for _, channel := range channels {
			channelMap[channel.Id] = channel.Name
		}
		for i := range logs {
			logs[i].ChannelName = channelMap[logs[i].ChannelId]
		}
	}

	return logs, total, err
}

const logSearchCountLimit = 10000

func GetUserLogs(userId int, logType int, startTimestamp int64, endTimestamp int64, modelName string, tokenName string, startIdx int, num int, group string, requestId string, upstreamRequestId string) (logs []*Log, total int64, err error) {
	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = LOG_DB.Where("logs.user_id = ?", userId)
	} else {
		tx = LOG_DB.Where("logs.user_id = ? and logs.type = ?", userId, logType)
	}
	if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", modelName); err != nil {
		return nil, 0, err
	}
	if tokenName != "" {
		tx = tx.Where("logs.token_name = ?", tokenName)
	}
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
	}
	if upstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", upstreamRequestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", group)
	}
	err = tx.Model(&Log{}).Limit(logSearchCountLimit).Count(&total).Error
	if err != nil {
		common.SysError("failed to count user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}
	order := "logs.id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("logs.")
	}
	err = tx.Order(order).Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		common.SysError("failed to search user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}

	formatUserLogs(logs, startIdx)
	return logs, total, err
}

type Stat struct {
	Quota int `json:"quota"`
	Rpm   int `json:"rpm"`
	Tpm   int `json:"tpm"`
}

func SumUsedQuota(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, channel int, group string) (stat Stat, err error) {
	tx := LOG_DB.Table("logs").Select("COALESCE(sum(quota), 0) quota")

	// 为rpm和tpm创建单独的查询
	rpmTpmQuery := LOG_DB.Table("logs").Select("count(*) rpm, COALESCE(sum(prompt_tokens), 0) + COALESCE(sum(completion_tokens), 0) tpm")

	if tx, err = applyExplicitLogTextFilter(tx, "username", username); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyExplicitLogTextFilter(rpmTpmQuery, "username", username); err != nil {
		return stat, err
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
		rpmTpmQuery = rpmTpmQuery.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if tx, err = applyExplicitLogTextFilter(tx, "model_name", modelName); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyExplicitLogTextFilter(rpmTpmQuery, "model_name", modelName); err != nil {
		return stat, err
	}
	if channel != 0 {
		tx = tx.Where("channel_id = ?", channel)
		rpmTpmQuery = rpmTpmQuery.Where("channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where(logGroupCol+" = ?", group)
		rpmTpmQuery = rpmTpmQuery.Where(logGroupCol+" = ?", group)
	}

	tx = tx.Where("type = ?", LogTypeConsume)
	rpmTpmQuery = rpmTpmQuery.Where("type = ?", LogTypeConsume)

	// 只统计最近60秒的rpm和tpm
	rpmTpmQuery = rpmTpmQuery.Where("created_at >= ?", time.Now().Add(-60*time.Second).Unix())

	// 执行查询
	if err := tx.Scan(&stat).Error; err != nil {
		common.SysError("failed to query log stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}
	if err := rpmTpmQuery.Scan(&stat).Error; err != nil {
		common.SysError("failed to query rpm/tpm stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}

	return stat, nil
}

func SumUsedToken(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string) (token int) {
	tx := LOG_DB.Table("logs").Select("COALESCE(sum(prompt_tokens), 0) + COALESCE(sum(completion_tokens), 0)")
	if username != "" {
		tx = tx.Where("username = ?", username)
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if modelName != "" {
		tx = tx.Where("model_name = ?", modelName)
	}
	tx.Where("type = ?", LogTypeConsume).Scan(&token)
	return token
}

func CountOldLog(ctx context.Context, targetTimestamp int64) (int64, error) {
	var total int64
	if err := LOG_DB.WithContext(ctx).Model(&Log{}).Where("created_at < ?", targetTimestamp).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func DeleteOldLogBatch(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	if limit <= 0 {
		limit = 100
	}
	if nil != ctx.Err() {
		return 0, ctx.Err()
	}

	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		// ClickHouse DELETE is a heavy mutation that rewrites data parts, so
		// per-batch mutations would be pathologically slow. Remove all matching
		// rows in a single synchronous mutation regardless of limit; the reported
		// count lets the caller's progress loop complete in one pass.
		total, err := CountOldLog(ctx, targetTimestamp)
		if err != nil {
			return 0, err
		}
		if total == 0 {
			return 0, nil
		}
		if err := LOG_DB.WithContext(ctx).Exec(
			"ALTER TABLE logs DELETE WHERE created_at < ? SETTINGS mutations_sync = 1",
			targetTimestamp,
		).Error; err != nil {
			return 0, err
		}
		return total, nil
	}

	result := LOG_DB.WithContext(ctx).Where("created_at < ?", targetTimestamp).Limit(limit).Delete(&Log{})
	if nil != result.Error {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func DeleteOldLog(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	if limit <= 0 {
		limit = 100
	}

	var total int64 = 0

	for {
		if nil != ctx.Err() {
			return total, ctx.Err()
		}

		rowsAffected, err := DeleteOldLogBatch(ctx, targetTimestamp, limit)
		if nil != err {
			return total, err
		}

		total += rowsAffected

		if rowsAffected < int64(limit) {
			break
		}
	}

	return total, nil
}

// ---- 蓝图A 渠道消耗聚合（余额告警 / 剩余天数估算，参考 feitianbubu）----

type ChannelRecentUsage struct {
	Quota      int64 `json:"quota"`
	ActiveDays int   `json:"active_days"`
}

// 剩余天数估算的共享窗口（渠道列表与余额告警同一口径）。
const (
	ChannelRecentUsageActiveDays   = 7
	ChannelRecentUsageLookbackDays = 90 // 聚合最多回看的天数，防无界扫描
)

func channelQuotaSnapshotSQL() string {
	ratioExpr := fmt.Sprintf(
		"CASE WHEN channel_ratio_set AND channel_ratio >= 0 AND channel_ratio <= %.0f THEN channel_ratio WHEN channel_ratio > 0 AND channel_ratio <= %.0f THEN channel_ratio ELSE 1 END",
		MaxChannelRatio,
		MaxChannelRatio,
	)
	productExpr := "quota * (" + ratioExpr + ")"
	// ROUND on approximate numeric types differs at .5 across supported engines
	// (notably PostgreSQL may use ties-to-even). FLOOR-based branches implement
	// the half-away-from-zero rule used by common.QuotaRound everywhere.
	roundedExpr := "CASE WHEN " + productExpr + " >= 0 THEN FLOOR(" + productExpr + " + 0.5) ELSE -FLOOR(-(" + productExpr + ") + 0.5) END"
	legacyExpr := fmt.Sprintf(
		"CASE WHEN %s >= %d THEN %d WHEN %s <= %d THEN %d ELSE %s END",
		roundedExpr, common.MaxQuota, common.MaxQuota,
		roundedExpr, common.MinQuota, common.MinQuota,
		roundedExpr,
	)
	// 新行写入时已把 原始费用（实付÷生效分组倍率）×渠道倍率 物化进 channel_quota
	// （分组倍率埋在 other JSON 里，SQL 取不到，只能物化）。加列前的旧行该列为
	// 0/NULL，回退上面 quota×channel_ratio 的旧口径表达式；真 0 成本行（quota=0
	// 或显式 0 倍率）回退结果同为 0，两分支无歧义。
	return "CASE WHEN channel_quota <> 0 THEN channel_quota ELSE " + legacyExpr + " END"
}

// GetChannelsRecentUsage 返回每个渠道"最近 maxActiveDays 个有消费的 UTC 日"的
// 消耗额度总和（回看不早于 since）。按活跃日而非自然日取平均，低频渠道不会被
// 大量零消费日稀释成"永不耗尽"。
func GetChannelsRecentUsage(channelIds []int, since int64, maxActiveDays int) (map[int]ChannelRecentUsage, error) {
	usageMap := make(map[int]ChannelRecentUsage)
	if len(channelIds) == 0 || maxActiveDays <= 0 {
		return usageMap, nil
	}
	var rows []struct {
		ChannelId int
		Day       int64
		Quota     int64
	}
	// 整数取模做日桶（created_at - created_at % 86400）：MySQL/PostgreSQL/SQLite
	// 行为一致（"/" 在 MySQL 上是小数除法，不可用）。
	// Each consume log snapshots the ratio used when it was charged. Sum the
	// per-row rounded values so later configuration changes do not rewrite
	// historical channel cost. Invalid legacy values fall back to 1.0; zero is a
	// valid explicit zero-cost ratio.
	err := LOG_DB.Table("logs").
		Select("channel_id, created_at - created_at % 86400 as day, sum("+channelQuotaSnapshotSQL()+") as quota").
		Where("type = ?", LogTypeConsume).
		Where("created_at >= ?", since).
		Where("channel_id IN ?", channelIds).
		Group("channel_id, day").
		Order("day desc").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	// 行按日期倒序返回，每个渠道的前 maxActiveDays 行恰好是其最近的活跃日
	for _, row := range rows {
		usage := usageMap[row.ChannelId]
		if usage.ActiveDays >= maxActiveDays {
			continue
		}
		usage.ActiveDays++
		usage.Quota += row.Quota
		usageMap[row.ChannelId] = usage
	}
	return usageMap, nil
}

// GetChannelsQuotaSince 返回每个渠道自 since 起（滑动窗口，不按日分桶）的消耗
// 额度总和。窗口内无消费的渠道不在 map 里。
func GetChannelsQuotaSince(channelIds []int, since int64) (map[int]int64, error) {
	result := make(map[int]int64)
	if len(channelIds) == 0 {
		return result, nil
	}
	var rows []struct {
		ChannelId int
		Quota     int64
	}
	err := LOG_DB.Table("logs").
		Select("channel_id, sum("+channelQuotaSnapshotSQL()+") as quota").
		Where("type = ?", LogTypeConsume).
		Where("created_at >= ?", since).
		Where("channel_id IN ?", channelIds).
		Group("channel_id").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.ChannelId] = row.Quota
	}
	return result, nil
}
