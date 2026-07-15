package relay

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSeedanceTaskIDFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/seedance/api/v3/contents/generations/tasks?filter.task_ids=cgt-a,cgt-b&filter.task_ids=task_c&filter.task_ids[]=cgt-d",
		nil,
	)

	require.Equal(t, []string{"cgt-a", "cgt-b", "task_c", "cgt-d"}, seedanceTaskIDFilters(ctx))
}

func TestSeedanceTaskResponseUsesUpstreamShape(t *testing.T) {
	task := &model.Task{
		TaskID:     "task_public",
		Status:     model.TaskStatusSuccess,
		SubmitTime: 1710000000,
		UpdatedAt:  1710000100,
		Properties: model.Properties{
			OriginModelName: "doubao-seedance-1-5-pro",
		},
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "cgt-upstream",
			ResultURL:      "https://example.com/video.mp4",
		},
		Data: json.RawMessage(`{"id":"cgt-upstream","status":"running","content":{},"service_tier":"default"}`),
	}

	resp := seedanceTaskResponse(task)
	assert.Equal(t, "cgt-upstream", resp["id"])
	assert.Equal(t, "doubao-seedance-1-5-pro", resp["model"])
	assert.Equal(t, "succeeded", resp["status"])
	assert.Equal(t, int64(1710000000), resp["created_at"])
	assert.Equal(t, int64(1710000100), resp["updated_at"])

	content, ok := resp["content"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "https://example.com/video.mp4", content["video_url"])
}

// TestSeedanceFetchTaskListPagination 同时护住无过滤的 DB 快路径与带过滤的内存慢路径：
// 两条路径都必须只返回本人、近 7 天、seedance/火山平台的任务，按 id 倒序，且分页与 total 正确。
func TestSeedanceFetchTaskListPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	model.DB = db
	t.Cleanup(func() { model.DB = originalDB })

	const userID = 42
	doubao := constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeDoubaoVideo))
	now := time.Now().Unix()

	// 5 条有效任务，按 t1..t5 顺序插入 → 自增 id 递增 → id desc 即 t5..t1。
	valid := []struct {
		taskID string
		status model.TaskStatus
	}{
		{"t1", model.TaskStatusSuccess},
		{"t2", model.TaskStatusInProgress},
		{"t3", model.TaskStatusSuccess},
		{"t4", model.TaskStatusInProgress},
		{"t5", model.TaskStatusSuccess},
	}
	for _, v := range valid {
		require.NoError(t, db.Create(&model.Task{
			TaskID:     v.taskID,
			Platform:   doubao,
			UserId:     userID,
			Status:     v.status,
			SubmitTime: now - 3600,
			Data:       json.RawMessage(`{}`),
		}).Error)
	}
	// 必须被排除的三条：他人、超 7 天窗口、非 seedance 平台。
	for _, excluded := range []*model.Task{
		{TaskID: "other-user", Platform: doubao, UserId: 99, Status: model.TaskStatusSuccess, SubmitTime: now - 3600, Data: json.RawMessage(`{}`)},
		{TaskID: "stale", Platform: doubao, UserId: userID, Status: model.TaskStatusSuccess, SubmitTime: now - 8*24*3600, Data: json.RawMessage(`{}`)},
		{TaskID: "wrong-platform", Platform: constant.TaskPlatform("1"), UserId: userID, Status: model.TaskStatusSuccess, SubmitTime: now - 3600, Data: json.RawMessage(`{}`)},
	} {
		require.NoError(t, db.Create(excluded).Error)
	}

	fetch := func(query string) (items []string, total int) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/tasks?"+query, nil)
		ctx.Set("id", userID)
		body, taskErr := seedanceFetchTaskList(ctx)
		require.Nil(t, taskErr)
		var got struct {
			Items []map[string]any `json:"items"`
			Total int              `json:"total"`
		}
		require.NoError(t, common.Unmarshal(body, &got))
		items = make([]string, 0, len(got.Items))
		for _, it := range got.Items {
			id, _ := it["id"].(string)
			items = append(items, id)
		}
		return items, got.Total
	}

	// 快路径（无过滤）：DB 级 COUNT + LIMIT/OFFSET。
	items, total := fetch("page_num=1&page_size=2")
	assert.Equal(t, 5, total, "只应统计本人近 7 天的 seedance 任务")
	assert.Equal(t, []string{"t5", "t4"}, items, "应按 id 倒序")
	items, _ = fetch("page_num=2&page_size=2")
	assert.Equal(t, []string{"t3", "t2"}, items)
	items, _ = fetch("page_num=3&page_size=2")
	assert.Equal(t, []string{"t1"}, items)
	items, total = fetch("page_num=4&page_size=2")
	assert.Empty(t, items, "越界页无数据")
	assert.Equal(t, 5, total, "越界页 total 不变")

	// 慢路径（状态过滤）：succeeded=SUCCESS 的 t5/t3/t1，过滤后再分页。
	items, total = fetch("filter.status=succeeded&page_num=1&page_size=2")
	assert.Equal(t, 3, total)
	assert.Equal(t, []string{"t5", "t3"}, items)
	items, _ = fetch("filter.status=succeeded&page_num=2&page_size=2")
	assert.Equal(t, []string{"t1"}, items)
}

// TestVideoFetchGenerationsPathReturnsOpenAIVideo 护住 fork 修复：查询 /v1/video/generations/{id} 必须
// 返回裸 OpenAIVideo（与提交侧一致、地址在 metadata.url），而不是通用 {code:"success",data} 信封——
// 否则 OpenAI 视频客户端（如 infinite-canvas）解析失败报“请求失败”。该前缀判定位于上游 relay_task.go，
// 合并上游时易被冲回，故用测试钉死。
func TestVideoFetchGenerationsPathReturnsOpenAIVideo(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}))
	model.DB = db
	t.Cleanup(func() { model.DB = originalDB })

	const userID = 7
	require.NoError(t, db.Create(&model.Task{
		TaskID:   "task_openai_fmt",
		Platform: constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeAiai)),
		UserId:   userID,
		Status:   model.TaskStatusSuccess,
		Progress: "100%",
		Data:     json.RawMessage(`{"task_status":"succeed","video_result":[{"url":"https://cdn.example.com/v.mp4"}]}`),
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/video/generations/task_openai_fmt", nil)
	ctx.Params = gin.Params{{Key: "task_id", Value: "task_openai_fmt"}}
	ctx.Set("id", userID)

	respBody, taskErr := videoFetchByIDRespBodyBuilder(ctx)
	require.Nil(t, taskErr)

	var got map[string]any
	require.NoError(t, common.Unmarshal(respBody, &got))
	// 裸 OpenAIVideo：object=video、completed、地址在 metadata.url；且不是通用信封。
	assert.Equal(t, "video", got["object"])
	assert.Equal(t, "completed", got["status"])
	_, isEnvelope := got["code"]
	assert.False(t, isEnvelope, "不应是 {code:success,data} 通用信封")
	metadata, ok := got["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "https://cdn.example.com/v.mp4", metadata["url"])
}
