package controller

// jzlh-veridrop 真伪检测控制器：渠道验真（管理员）+ 公开检测页 + 红黑榜。
// 检测走原生 pkg/detector 引擎，异步 job（检测含真实请求耗时 15-70s）。

import (
	"context"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/detector"
)

// ---- 异步 job 注册表（内存，短生命周期）----

type detectJob struct {
	ID        string           `json:"id"`
	Status    string           `json:"status"` // running/done/error
	Report    *detector.Report `json:"report,omitempty"`
	Err       string           `json:"error,omitempty"`
	CreatedAt int64            `json:"created_at"`
}

var (
	detectJobs   = make(map[string]*detectJob)
	detectJobsMu sync.Mutex
	// detectSem 限制并发在跑的检测数（每次检测含 15-70s 的多轮上游请求）。超出的
	// job 排队而非无限起 goroutine，对齐 Veridrop 的 inflight 上限。
	detectSem = make(chan struct{}, 6)
)

// pruneDetectJobsLocked 清理 1 小时前的旧 job（调用方须持锁）。
func pruneDetectJobsLocked() {
	cutoff := common.GetTimestamp() - 3600
	for id, job := range detectJobs {
		if job.CreatedAt < cutoff {
			delete(detectJobs, id)
		}
	}
}

func baseURLDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Hostname()
}

// startDetectJob 建 job 并后台跑检测；channelId>0 时把结果写回渠道快照。
func startDetectJob(cfg detector.Config, channelId int, source string) string {
	jobId := common.GetUUID()
	job := &detectJob{ID: jobId, Status: "running", CreatedAt: common.GetTimestamp()}
	detectJobsMu.Lock()
	pruneDetectJobsLocked()
	detectJobs[jobId] = job
	detectJobsMu.Unlock()

	go func() {
		detectSem <- struct{}{} // bounded concurrency; excess jobs queue here
		defer func() { <-detectSem }()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		report, err := detector.Run(ctx, cfg)
		detectJobsMu.Lock()
		if err != nil {
			job.Status = "error"
			job.Err = err.Error()
		} else {
			job.Status = "done"
			job.Report = report
		}
		detectJobsMu.Unlock()
		if err != nil || report == nil {
			return
		}
		persistDetection(report, channelId, cfg.BaseURL, source)
	}()
	return jobId
}

// persistDetection 落 DetectionRecord + 渠道快照，供应商渠道 critical 时自动摘出池。
func persistDetection(report *detector.Report, channelId int, baseURL string, source string) {
	resultsJSON, _ := common.Marshal(report)
	record := &model.DetectionRecord{
		ChannelId:     channelId,
		Domain:        baseURLDomain(baseURL),
		Protocol:      report.Protocol,
		Model:         report.TargetModel,
		Verdict:       report.Verdict,
		Score:         report.TotalScore,
		CriticalCount: report.CriticalCount,
		Results:       string(resultsJSON),
		Source:        source,
		ApiKeyMasked:  report.APIKeyMasked,
	}
	if err := record.Insert(); err != nil {
		common.SysError("failed to persist detection record: " + err.Error())
	}
	if channelId > 0 {
		if err := model.ApplyChannelDetectionSnapshot(channelId, report.Verdict, report.TotalScore, report.CriticalCount, common.GetTimestamp()); err != nil {
			common.SysError("failed to update channel detection snapshot: " + err.Error())
		}
		// 供应商渠道判定 critical（非管理员手动验真）→ 打回待审，摘出路由池，需复审。
		if report.CriticalCount > 0 && source == model.DetectionSourceCron {
			if err := model.AutoSuspendChannelOnCritical(channelId); err != nil {
				common.SysError("failed to auto-suspend channel on critical: " + err.Error())
			}
		}
	}
}

func detectModeOrDefault(mode string) string {
	switch mode {
	case "quick", "standard", "full":
		return mode
	case "deep":
		// 前端「深度」档 = 引擎 full 模式（跑全部检测器 + full-mode 基线比对）。
		// 此前 deep 落到 default 被静默降级成 standard，full 档从 UI 够不到。
		return "full"
	default:
		return "standard"
	}
}

// ---- 管理端：渠道验真 ----

// DetectChannel 对指定渠道跑真伪检测（用渠道自己的 base_url+key+首个模型）。
func DetectChannel(c *gin.Context) {
	channelId, _ := strconv.Atoi(c.Param("id"))
	if channelId <= 0 {
		common.ApiErrorMsg(c, "invalid channel id")
		return
	}
	channel, err := model.GetChannelById(channelId, true)
	if err != nil || channel == nil {
		common.ApiError(c, err)
		return
	}
	baseURL := ""
	if channel.BaseURL != nil {
		baseURL = *channel.BaseURL
	}
	testModel := ""
	if channel.TestModel != nil {
		testModel = *channel.TestModel
	}
	if testModel == "" {
		testModel = firstModel(channel.Models)
	}
	cfg := detector.Config{
		BaseURL: baseURL,
		APIKey:  channel.Key,
		Model:   testModel,
		Mode:    detectModeOrDefault(c.Query("mode")),
	}
	jobId := startDetectJob(cfg, channelId, model.DetectionSourceAdmin)
	common.ApiSuccess(c, gin.H{"job_id": jobId, "status_url": "/api/detector/status/" + jobId})
}

func firstModel(models string) string {
	for i := 0; i < len(models); i++ {
		if models[i] == ',' {
			return models[:i]
		}
	}
	return models
}

// ChannelLatestDetection 返回某渠道最近一次检测记录。
func ChannelLatestDetection(c *gin.Context) {
	channelId, _ := strconv.Atoi(c.Param("id"))
	record, err := model.GetLatestDetectionByChannel(channelId)
	if err != nil {
		common.ApiSuccess(c, nil) // 无记录不算错误
		return
	}
	common.ApiSuccess(c, record)
}

// DetectorRecords 检测历史（管理端；?channel_id= 过滤）。
func DetectorRecords(c *gin.Context) {
	channelId, _ := strconv.Atoi(c.Query("channel_id"))
	page, pageSize := parseAgentPagination(c)
	records, total, err := model.ListDetectionRecords(channelId, (page-1)*pageSize, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": records, "total": total, "page": page, "page_size": pageSize})
}

// ---- 公开检测页 ----

type publicDetectRequest struct {
	BaseURL                   string `json:"base_url"`
	APIKey                    string `json:"api_key"`
	Model                     string `json:"model"`
	Protocol                  string `json:"protocol"`
	Mode                      string `json:"mode"`
	IncludeLongContext        bool   `json:"include_long_context"`
	IncludeLongContextExtreme bool   `json:"include_long_context_extreme"`
}

// DetectPublic 公开检测任意中转站（限速 + 引擎内 SSRF 守卫）。
func DetectPublic(c *gin.Context) {
	var req publicDetectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.BaseURL == "" || req.APIKey == "" || req.Model == "" {
		common.ApiErrorMsg(c, "base_url / api_key / model required")
		return
	}
	cfg := detector.Config{
		BaseURL:                   req.BaseURL,
		APIKey:                    req.APIKey,
		Model:                     req.Model,
		Protocol:                  req.Protocol,
		Mode:                      detectModeOrDefault(req.Mode),
		IncludeLongContext:        req.IncludeLongContext,
		IncludeLongContextExtreme: req.IncludeLongContextExtreme,
	}
	jobId := startDetectJob(cfg, 0, model.DetectionSourcePublic)
	common.ApiSuccess(c, gin.H{"job_id": jobId, "status_url": "/api/detector/status/" + jobId})
}

// DetectStatus 轮询 job 结果。
func DetectStatus(c *gin.Context) {
	jobId := c.Param("jobId")
	detectJobsMu.Lock()
	job, ok := detectJobs[jobId]
	detectJobsMu.Unlock()
	if !ok {
		common.ApiErrorMsg(c, "job not found")
		return
	}
	common.ApiSuccess(c, job)
}

// AdminRecheckSupplierChannels 批量复检所有已通过的供应商渠道（运营可 cron 触发本端点）。
// 每渠道跑 quick 模式；critical 结果经 persistDetection(source=cron) 自动摘出路由池。
func AdminRecheckSupplierChannels(c *gin.Context) {
	channels, err := model.GetApprovedSupplierChannels()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	started := 0
	for _, ch := range channels {
		withKey, e := model.GetChannelById(ch.Id, true)
		if e != nil || withKey == nil {
			continue
		}
		baseURL := ""
		if withKey.BaseURL != nil {
			baseURL = *withKey.BaseURL
		}
		targetModel := firstModel(withKey.Models)
		if withKey.TestModel != nil && *withKey.TestModel != "" {
			targetModel = *withKey.TestModel
		}
		startDetectJob(detector.Config{
			BaseURL: baseURL, APIKey: withKey.Key, Model: targetModel, Mode: "quick",
		}, ch.Id, model.DetectionSourceCron)
		started++
	}
	common.ApiSuccess(c, gin.H{"started": started})
}

// DetectorLeaderboard 渠道红黑榜（公开，按域名贝叶斯加权聚合，仅公开检测提交）。
func DetectorLeaderboard(c *gin.Context) {
	page, pageSize := parseAgentPagination(c)
	entries, err := model.GetDetectionLeaderboard((page-1)*pageSize, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": entries, "page": page, "page_size": pageSize})
}
