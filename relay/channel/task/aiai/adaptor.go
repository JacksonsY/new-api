package aiai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// TaskAdaptor 通用 OpenAI 兼容视频生成任务适配器（Bearer 鉴权），适配 new-api 套壳的
// 视频供应商（如 aiai.ac）。上游前缀由 Base URL 自行携带（例如 aiai.ac 的 Base URL 填
// https://aiai.ac/api），适配器只拼相对路径：
//
//	提交：POST {base}/v1/videos/generations       （请求体透传，仅覆写 model + 强制 async）
//	查询：GET  {base}/v1/video/{task_id}/result
//
// 计费：按 (分辨率, 是否带参考视频) 折算倍率 × 计费秒数，管理员在「模型固定价格」为模型
// 配置基准价（{480p, 无参考} 的每秒价）。分辨率倍率表见 constants.go（按模型名）。
type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

// billingParams 从原始请求体读取计费相关字段（TaskSubmitReq 不含 resolution/video）。
// video 在 aiai.ac 既可为字符串 URL 也可为 URL 数组，故用 RawMessage 兼容两种形态。
// RefDuration 是参考视频时长（秒）：aiai.ac 标准请求体不含它，供二开客户端主动传入
// （顶层 ref_duration 或 extra_body.ref_duration），以便对「视频编辑」精确计费；不传则
// 退化为最低计费下限。
type billingParams struct {
	Resolution  string          `json:"resolution"`
	Size        string          `json:"size"`    // 部分模型/客户端用 "宽x高" 传分辨率，计费兜底解析
	Quality     string          `json:"quality"` // kling：std/pro 决定分辨率档
	WithAudio   bool            `json:"with_audio"`
	Video       json.RawMessage `json:"video"`
	Duration    int             `json:"duration"`
	RefDuration int             `json:"ref_duration"`
	ExtraBody   struct {
		RefDuration int `json:"ref_duration"`
	} `json:"extra_body"`
}

// effectiveResolution 返回计费用分辨率：优先请求里的 resolution（seedance 官方参数）；
// 为空时兜底从 size（如 "1920x1080"）推断，防止客户端改用 size 传分辨率、绕过分辨率倍率而少收费。
func (b billingParams) effectiveResolution() string {
	if strings.TrimSpace(b.Resolution) != "" {
		return b.Resolution
	}
	return resolutionFromSize(b.Size)
}

// resolutionFromSize 从 "宽x高"（如 "1280x720" / "720x1280" / "1920×1080" / "1280*720"）
// 按短边映射到分辨率档，就近向上取档（宁可多收不少收）；无法解析时返回 ""（调用方退化到默认档）。
func resolutionFromSize(size string) string {
	s := strings.ToLower(strings.TrimSpace(size))
	sep := ""
	for _, cand := range []string{"x", "*", "×"} {
		if strings.Contains(s, cand) {
			sep = cand
			break
		}
	}
	if sep == "" {
		return ""
	}
	parts := strings.SplitN(s, sep, 2)
	if len(parts) != 2 {
		return ""
	}
	w, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return ""
	}
	short := w
	if h < short {
		short = h
	}
	switch {
	case short <= 480:
		return "480p"
	case short <= 720:
		return "720p"
	case short <= 1080:
		return "1080p"
	default:
		return "4k"
	}
}

// refDuration 返回客户端提供的参考视频时长（秒）；顶层优先于 extra_body，均缺省为 0。
func (b billingParams) refDuration() int {
	if b.RefDuration > 0 {
		return b.RefDuration
	}
	return b.ExtraBody.RefDuration
}

// hasReferenceVideo 判断请求是否携带参考视频（触发「视频编辑」计费）。
// 兼容 video 为字符串或数组，并把 null / 空串 / 空数组视为无参考视频。
func hasReferenceVideo(raw json.RawMessage) bool {
	switch strings.TrimSpace(string(raw)) {
	case "", "null", `""`, "[]", "{}":
		return false
	}
	return true
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	return relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate)
}

// upstreamModelKey 返回查「上游固有属性表」(分辨率倍率 / 火山式 token 价差) 用的模型名。
// 生产链路里 ModelMappedHelper 会把重定向后的真实上游名写进 ChannelMeta.UpstreamModelName
// （即使本地名带路由前缀如 "A/doubao-seedance-2.0-mini"，这里也是干净的 "doubao-seedance-2.0-mini"），
// 优先用它命中价目表；ChannelMeta 未初始化或为空时（如单元测试）回退到 OriginModelName。
func upstreamModelKey(info *relaycommon.RelayInfo) string {
	if info.ChannelMeta != nil && info.UpstreamModelName != "" {
		return info.UpstreamModelName
	}
	return info.OriginModelName
}

// EstimateBilling 按 (分辨率, 是否带参考视频) 折算相对基准价的倍率，并乘以计费秒数。
//
//	无参考视频：计费秒数 = 生成时长
//	带参考视频：计费秒数 = Max(参考视频时长 + 生成时长, 最低计费时长)
//	          参考视频时长取客户端可选传入的 ref_duration / extra_body.ref_duration；
//	          未提供时（aiai.ac 标准请求体不含）退化为「最低计费时长」下限——此时
//	          参考视频较长会少收，需精确计费请让客户端传 ref_duration。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	bp := a.readBillingParams(c)

	// aiai 全部按秒计费：只有配了「模型固定价格 ModelPrice」的模型才叠加秒/分档 OtherRatio；
	// 未配（misconfig）直接返回 nil、不加成。计费模式检测按 OriginModelName（用户本地名，可能带
	// 路由前缀如 "A/"，seedance 用它区分官方 token 线与 aiai 秒计费线）；分档倍率则按
	// UpstreamModelName（模型重定向后的真实上游名，如 "doubao-seedance-2.0-mini"）查表，
	// 否则带前缀的 Seedance 会查不到价目表、丢失分辨率倍率而少收费。
	if _, perSecond := ratio_setting.GetModelPrice(info.OriginModelName, false); !perSecond {
		return nil
	}

	// 按档定价：不同模型分档维度不同（seedance 分辨率×参考视频、veo 分辨率、kling quality×声音），
	// 计费秒数也不同（seedance 编辑档有下限、veo 均衡线路固定 8s）。按上游名查档位表（兼容本地名前缀）。
	p, ok := videoPricingTable[upstreamModelKey(info)]
	if !ok {
		// 未收录价目表的模型（未来/未知）：纯秒计费（ModelPrice × 时长，不加成）。
		d := bp.Duration
		if d <= 0 {
			d = 5
		}
		return map[string]float64{"seconds": float64(min(d, relaycommon.MaxTaskDurationSeconds))}
	}
	return map[string]float64{
		"tier":    p.tierRatio(bp),
		"seconds": float64(p.billedSeconds(bp)),
	}
}

// aiaiEffectiveDuration 计费与转发统一的有效生成时长（秒）：优先 duration，为 0 再看 OpenAI
// 风格的 seconds 字符串——与 relaycommon.validateTaskDurationBounds 的取值口径一致。返回 0
// 表示请求未指定时长。
func aiaiEffectiveDuration(req relaycommon.TaskSubmitReq) int {
	if req.Duration != 0 {
		return req.Duration
	}
	if s := strings.TrimSpace(req.Seconds); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			return v
		}
	}
	return 0
}

func (a *TaskAdaptor) readBillingParams(c *gin.Context) billingParams {
	var bp billingParams
	_ = common.UnmarshalBodyReusable(c, &bp)
	// 计费时长/尺寸以框架已解析并已 bound 的 TaskSubmitReq 为准：修掉「用 seconds 传时长」
	// 以及 multipart 提交被 JSON-only 的 billingParams 漏掉、从而按 5 秒地板价少收的问题。
	if req, err := relaycommon.GetTaskRequest(c); err == nil {
		if d := aiaiEffectiveDuration(req); d != 0 {
			bp.Duration = d
		}
		if strings.TrimSpace(bp.Size) == "" {
			bp.Size = req.Size
		}
	}
	return bp
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/v1/videos/generations", a.baseURL), nil
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

// BuildRequestBody 透传原始请求体，覆写 model 为上游模型名，并强制 async=true。
// new-api 的视频任务链路本身就是异步（返回 task_id 后台轮询），强制 async 可避免
// 上游同步阻塞导致提交超时。
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, errors.Wrap(err, "get_request_body_failed")
	}
	cachedBody, err := storage.Bytes()
	if err != nil {
		return nil, errors.Wrap(err, "read_body_bytes_failed")
	}

	var bodyMap map[string]interface{}
	if err := common.Unmarshal(cachedBody, &bodyMap); err != nil {
		// body 不是 JSON 对象：无法安全改写 model→UpstreamModelName / 强制 async。
		// 与其把带本地(可能带路由前缀)模型名的原始 body 透传给上游致其误判模型，不如显式失败。
		return nil, errors.Wrap(err, "unmarshal_request_body_failed")
	}
	bodyMap["model"] = info.UpstreamModelName
	bodyMap["async"] = true
	// 通用/Sora 风格请求归一到 aiai 原生字段：仅在原生字段缺失时从通用别名回填，绝不覆盖
	// 客户端已显式给的 aiai 专有参数（aspect_ratio/watermark/extra_body 等照旧透传）。
	//   seconds(OpenAI/通用) → duration(aiai 原生)；size("宽x高", Sora) → resolution(档位)。
	// 这样 aiai 渠道也能吃通用/Sora 风格的 JSON 请求，而不只是 aiai 专有 JSON。
	// （multipart 文件上传因 aiai 上游只收 URL，仍不支持——见上面的 JSON-only 解析。）
	if req, err := relaycommon.GetTaskRequest(c); err == nil {
		if _, ok := bodyMap["duration"]; !ok {
			if d := aiaiEffectiveDuration(req); d > 0 {
				bodyMap["duration"] = d
			}
		}
		if _, ok := bodyMap["resolution"]; !ok {
			size, _ := bodyMap["size"].(string)
			if strings.TrimSpace(size) == "" {
				size = req.Size
			}
			if res := resolutionFromSize(size); res != "" {
				bodyMap["resolution"] = res
			}
		}
	}
	// ref_duration 是我们计费用的自定义字段（非 aiai.ac 文档参数），转发上游前剥掉，
	// 避免严格的上游因未知参数报错。
	delete(bodyMap, "ref_duration")
	if eb, ok := bodyMap["extra_body"].(map[string]interface{}); ok {
		delete(eb, "ref_duration")
	}
	newBody, err := common.Marshal(bodyMap)
	if err != nil {
		return bytes.NewReader(cachedBody), nil
	}
	return bytes.NewReader(newBody), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// submitResponse 兼容多种任务 id 字段位置（提交响应契约待「后台任务」文档确认）。
type submitResponse struct {
	ID           string `json:"id"`
	TaskID       string `json:"task_id"`
	ErrorMessage string `json:"error_message"`
	Data         struct {
		ID     string `json:"id"`
		TaskID string `json:"task_id"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
}

func (r submitResponse) errMessage() string {
	if r.Error != nil {
		return firstNonEmpty(r.ErrorMessage, r.Error.Message)
	}
	return r.ErrorMessage
}

func (r submitResponse) taskID() string {
	for _, v := range []string{r.ID, r.TaskID, r.Data.ID, r.Data.TaskID} {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	var sResp submitResponse
	if err := common.Unmarshal(responseBody, &sResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}
	if msg := sResp.errMessage(); strings.TrimSpace(msg) != "" {
		taskErr = service.TaskErrorWrapper(errors.New(msg), "upstream_error", http.StatusBadRequest)
		return
	}
	upstreamID := sResp.taskID()
	if upstreamID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty, body: %s", responseBody), "invalid_response", http.StatusInternalServerError)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	c.JSON(http.StatusOK, ov)

	return upstreamID, responseBody, nil
}

// FetchTask 查询任务状态：GET {base}/v1/video/{task_id}/result（单数 video + /result 后缀）。
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || taskID == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	uri := fmt.Sprintf("%s/v1/video/%s/result", baseUrl, taskID)

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

// taskResult 兼容 aiai 视频查询响应的字段命名（真机实测契约，跨火山/快手/HappyHorse/xAI 统一）：
//
//	处理中：{"task_status":"pending"/"running"}
//	成功：  {"task_status":"succeed", "video_result":[{"url"}]}   （另兼容文档的 status / 顶层 video_url）
//	失败：  {"task_status":"failed", "error":{"message"}}         （另兼容文档的 error_message）
type taskResult struct {
	TaskID      string `json:"task_id"`
	Status      string `json:"status"` // 文档字段；真机用 task_status
	TaskStatus  string `json:"task_status"`
	VideoURL    string `json:"video_url"` // 文档字段；真机用 video_result[].url
	URL         string `json:"url"`
	VideoResult []struct {
		URL string `json:"url"`
	} `json:"video_result"`
	ErrorMessage string `json:"error_message"` // 文档字段；真机用 error.message
	Error        *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (r taskResult) status() string {
	return strings.ToLower(strings.TrimSpace(firstNonEmpty(r.Status, r.TaskStatus)))
}

func (r taskResult) videoURL() string {
	if u := firstNonEmpty(r.VideoURL, r.URL); u != "" {
		return u
	}
	for _, v := range r.VideoResult {
		if strings.TrimSpace(v.URL) != "" {
			return v.URL
		}
	}
	return ""
}

func (r taskResult) errMsg() string {
	if r.Error != nil {
		return firstNonEmpty(r.ErrorMessage, r.Error.Message)
	}
	return r.ErrorMessage
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var res taskResult
	if err := common.Unmarshal(respBody, &res); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	info := &relaycommon.TaskInfo{Code: 0}
	switch res.status() {
	case "queued", "in_queue", "pending", "submitted", "created", "not_start":
		info.Status = model.TaskStatusQueued
		info.Progress = "10%"
	case "processing", "running", "in_progress", "generating", "doing":
		info.Status = model.TaskStatusInProgress
		info.Progress = "50%"
	case "success", "succeeded", "succeed", "completed", "done", "finished":
		info.Status = model.TaskStatusSuccess
		info.Progress = "100%"
		info.Url = res.videoURL()
	case "failed", "failure", "error", "fail",
		"cancelled", "canceled", "timeout", "expired", "rejected":
		// 文档只给了 succeed 的 task_status，失败取值未列明；覆盖常见终态失败词，
		// 避免未识别的终态落到 default 分支导致任务一直轮询不结束。
		info.Status = model.TaskStatusFailure
		info.Progress = "100%"
		info.Reason = res.errMsg()
	default:
		info.Status = model.TaskStatusInProgress
		info.Progress = "30%"
	}
	return info, nil
}

func (a *TaskAdaptor) GetModelList() []string { return ModelList }

func (a *TaskAdaptor) GetChannelName() string { return ChannelName }

// ConvertToOpenAIVideo 把本地任务记录转成 OpenAI 视频对象返回给客户端。
func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var res taskResult
	if err := common.Unmarshal(originTask.Data, &res); err != nil {
		return nil, errors.Wrap(err, "unmarshal aiai task data failed")
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = originTask.TaskID
	ov.TaskID = originTask.TaskID
	ov.Status = originTask.Status.ToVideoStatus()
	ov.SetProgressStr(originTask.Progress)
	ov.SetMetadata("url", res.videoURL())
	ov.CreatedAt = originTask.CreatedAt
	ov.CompletedAt = originTask.UpdatedAt
	ov.Model = originTask.Properties.OriginModelName

	if msg := res.errMsg(); strings.TrimSpace(msg) != "" {
		ov.Error = &dto.OpenAIVideoError{Message: msg}
	}
	return common.Marshal(ov)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
