package relay

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func SeedanceTaskFetch(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	taskID := strings.TrimSpace(c.Param("task_id"))
	if taskID != "" {
		return seedanceFetchTaskByID(c, taskID)
	}
	return seedanceFetchTaskList(c)
}

func seedanceFetchTaskByID(c *gin.Context, taskID string) (respBody []byte, taskResp *dto.TaskError) {
	originTask, exist, err := seedanceGetTaskByID(c.GetInt("id"), taskID)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "get_task_failed", http.StatusInternalServerError)
		return
	}
	if !exist {
		taskResp = service.TaskErrorWrapperLocal(errors.New("task_not_exist"), "task_not_exist", http.StatusBadRequest)
		return
	}

	respBody, err = common.Marshal(seedanceTaskResponse(originTask))
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "marshal_response_failed", http.StatusInternalServerError)
	}
	return
}

func seedanceFetchTaskList(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	pageNum := parseSeedancePositiveInt(c.Query("page_num"), 1, 500)
	pageSize := parseSeedancePositiveInt(c.Query("page_size"), 20, 500)
	offset := (pageNum - 1) * pageSize
	userID := c.GetInt("id")

	statusFilter := strings.TrimSpace(c.Query("filter.status"))
	modelFilter := strings.TrimSpace(c.Query("filter.model"))
	serviceTierFilter := strings.TrimSpace(c.Query("filter.service_tier"))
	taskIDFilter := seedanceTaskIDFilters(c)

	// 快路径：无过滤条件时，直接在数据库层 COUNT + LIMIT/OFFSET 分页，避免把用户近 7 天的
	// 全部任务读进内存。model/service_tier 存于 task.Data JSON，跨库无法可靠下推，故仅在
	// 完全无过滤时启用；带过滤时走下方内存路径以保证过滤+分页语义正确。
	if statusFilter == "" && modelFilter == "" && serviceTierFilter == "" && len(taskIDFilter) == 0 {
		var total int64
		if err := seedanceTaskBaseQuery(userID).Model(&model.Task{}).Count(&total).Error; err != nil {
			taskResp = service.TaskErrorWrapper(err, "get_tasks_failed", http.StatusInternalServerError)
			return
		}
		var tasks []*model.Task
		if err := seedanceTaskBaseQuery(userID).Order("id desc").Limit(pageSize).Offset(offset).Find(&tasks).Error; err != nil {
			taskResp = service.TaskErrorWrapper(err, "get_tasks_failed", http.StatusInternalServerError)
			return
		}
		return seedanceMarshalTaskList(tasks, int(total))
	}

	var tasks []*model.Task
	if err := seedanceTaskBaseQuery(userID).Order("id desc").Find(&tasks).Error; err != nil {
		taskResp = service.TaskErrorWrapper(err, "get_tasks_failed", http.StatusInternalServerError)
		return
	}

	filtered := make([]*model.Task, 0, len(tasks))
	needData := modelFilter != "" || serviceTierFilter != ""
	for _, task := range tasks {
		if len(taskIDFilter) > 0 && !seedanceTaskMatchesID(task, taskIDFilter) {
			continue
		}
		if statusFilter != "" && seedanceTaskStatus(task.Status) != statusFilter {
			continue
		}
		// model/service_tier 存于 task.Data，仅在需要时解析一次并复用，避免每个过滤条件各解析一遍。
		var data map[string]any
		if needData {
			_ = common.Unmarshal(task.Data, &data)
		}
		if modelFilter != "" && !seedanceTaskMatchesModel(task, data, modelFilter) {
			continue
		}
		if serviceTierFilter != "" && !seedanceDataFieldEquals(data, "service_tier", serviceTierFilter) {
			continue
		}
		filtered = append(filtered, task)
	}

	total := len(filtered)
	if offset > total {
		filtered = []*model.Task{}
	} else {
		end := offset + pageSize
		if end > total {
			end = total
		}
		filtered = filtered[offset:end]
	}

	return seedanceMarshalTaskList(filtered, total)
}

// seedanceTaskBaseQuery 构造某用户近 7 天 seedance/火山视频任务的基础查询。每次调用都从
// model.DB 起链返回独立查询，可安全分别用于 Count 与 Find。
func seedanceTaskBaseQuery(userID int) *gorm.DB {
	return model.DB.
		Where("user_id = ?", userID).
		Where("platform in ?", seedanceTaskPlatforms()).
		Where("submit_time >= ?", time.Now().Add(-7*24*time.Hour).Unix())
}

// seedanceMarshalTaskList 把任务列表与总数序列化为官方 ARK 列表响应。
func seedanceMarshalTaskList(tasks []*model.Task, total int) (respBody []byte, taskResp *dto.TaskError) {
	items := make([]map[string]any, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, seedanceTaskResponse(task))
	}
	respBody, err := common.Marshal(map[string]any{
		"items": items,
		"total": total,
	})
	if err != nil {
		return nil, service.TaskErrorWrapper(err, "marshal_response_failed", http.StatusInternalServerError)
	}
	return respBody, nil
}

func seedanceGetTaskByID(userID int, taskID string) (*model.Task, bool, error) {
	task, exist, err := model.GetByTaskId(userID, taskID)
	if err != nil || exist {
		return task, exist, err
	}

	var tasks []*model.Task
	err = seedanceTaskBaseQuery(userID).Find(&tasks).Error
	if err != nil {
		return nil, false, err
	}
	for _, candidate := range tasks {
		if candidate.GetUpstreamTaskID() == taskID {
			return candidate, true, nil
		}
	}
	return nil, false, nil
}

func seedanceTaskPlatforms() []string {
	return []string{
		strconv.Itoa(constant.ChannelTypeVolcEngine),
		strconv.Itoa(constant.ChannelTypeDoubaoVideo),
	}
}

func seedanceTaskIDFilters(c *gin.Context) []string {
	rawIDs := append(c.QueryArray("filter.task_ids"), c.QueryArray("filter.task_ids[]")...)
	taskIDs := make([]string, 0, len(rawIDs))
	for _, rawID := range rawIDs {
		for _, taskID := range strings.Split(rawID, ",") {
			taskID = strings.TrimSpace(taskID)
			if taskID != "" {
				taskIDs = append(taskIDs, taskID)
			}
		}
	}
	return taskIDs
}

func seedanceTaskMatchesID(task *model.Task, taskIDs []string) bool {
	for _, taskID := range taskIDs {
		if task.TaskID == taskID || task.GetUpstreamTaskID() == taskID {
			return true
		}
	}
	return false
}

func parseSeedancePositiveInt(raw string, fallback, maxValue int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		value = fallback
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func seedanceTaskMatchesModel(task *model.Task, data map[string]any, modelName string) bool {
	if task.Properties.OriginModelName == modelName || task.Properties.UpstreamModelName == modelName {
		return true
	}
	return seedanceDataFieldEquals(data, "model", modelName)
}

// seedanceDataFieldEquals 在已解析的 task.Data map 上比较字段（nil map 视为不匹配）。
func seedanceDataFieldEquals(data map[string]any, field string, value string) bool {
	fieldValue, _ := data[field].(string)
	return fieldValue == value
}

func seedanceTaskResponse(task *model.Task) map[string]any {
	resp := map[string]any{}
	_ = common.Unmarshal(task.Data, &resp)

	resp["id"] = task.GetUpstreamTaskID()
	if modelName := task.Properties.OriginModelName; modelName != "" {
		resp["model"] = modelName
	} else if modelName = task.Properties.UpstreamModelName; modelName != "" {
		resp["model"] = modelName
	}
	resp["status"] = seedanceTaskStatus(task.Status)

	if createdAt := nonzeroSeedanceInt64(task.SubmitTime, task.CreatedAt); createdAt > 0 {
		resp["created_at"] = createdAt
	}
	if task.UpdatedAt > 0 {
		resp["updated_at"] = task.UpdatedAt
	}

	if resultURL := task.GetResultURL(); resultURL != "" && task.Status == model.TaskStatusSuccess {
		content, _ := resp["content"].(map[string]any)
		if content == nil {
			content = map[string]any{}
		}
		if _, ok := content["video_url"]; !ok {
			content["video_url"] = resultURL
		}
		resp["content"] = content
	}

	if task.Status == model.TaskStatusFailure && resp["error"] == nil && task.FailReason != "" {
		resp["error"] = map[string]any{
			"message": task.FailReason,
		}
	}
	return resp
}

func nonzeroSeedanceInt64(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func seedanceTaskStatus(status model.TaskStatus) string {
	switch status {
	case model.TaskStatusSuccess:
		return "succeeded"
	case model.TaskStatusFailure:
		return "failed"
	case model.TaskStatusInProgress:
		return "running"
	default:
		return "queued"
	}
}
