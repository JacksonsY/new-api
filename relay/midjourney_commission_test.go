package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMidjourneyCommissionSourceKeyMatchesTaskRefundIdentity(t *testing.T) {
	const (
		channelID = 17
		taskID    = "mj:provider_job%1"
		quota     = 1250
	)

	assert.Equal(t,
		model.BuildTaskCommissionSourceKey(channelID, taskID, "initial", quota),
		midjourneyCommissionSourceKey(channelID, taskID, quota),
	)
	assert.Empty(t, midjourneyCommissionSourceKey(channelID, "", quota))
}

func TestRejectedMidjourneySubmissionsAreNotRefundable(t *testing.T) {
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalRedisEnabled := common.RedisEnabled
	originalBatchUpdateEnabled := common.BatchUpdateEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Midjourney{}))
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	service.InitHttpClient()
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.RedisEnabled = originalRedisEnabled
		common.BatchUpdateEnabled = originalBatchUpdateEnabled
	})

	user := &model.User{
		Username: "mj-rejected-user",
		Status:   common.UserStatusEnabled,
		Quota:    10_000_000,
	}
	require.NoError(t, db.Create(user).Error)

	tests := []struct {
		name       string
		path       string
		body       string
		statusCode int
		response   string
		relay      func(*gin.Context, *relaycommon.RelayInfo)
	}{
		{
			name:       "queue full midjourney task",
			path:       "/mj/submit/imagine",
			body:       `{"prompt":"cat"}`,
			statusCode: http.StatusOK,
			response:   `{"code":23,"description":"queue full","result":"rejected-mj"}`,
			relay: func(ctx *gin.Context, info *relaycommon.RelayInfo) {
				info.RelayMode = relayconstant.RelayModeMidjourneyImagine
				info.OriginModelName = "mj_imagine"
				_ = RelayMidjourneySubmit(ctx, info)
			},
		},
		{
			name:       "failed swap face task",
			path:       "/mj/insight-face/swap",
			body:       `{"sourceBase64":"source","targetBase64":"target"}`,
			statusCode: http.StatusBadGateway,
			response:   `{"code":4,"description":"upstream failure","result":"rejected-swap"}`,
			relay: func(ctx *gin.Context, info *relaycommon.RelayInfo) {
				info.OriginModelName = "swap_face"
				_ = RelaySwapFace(ctx, info)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, db.Exec("DELETE FROM midjourneys").Error)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			t.Cleanup(upstream.Close)

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			ctx.Set("base_url", upstream.URL)
			ctx.Set(string(constant.ContextKeyChannelId), 0)
			info := &relaycommon.RelayInfo{
				UserId:         user.Id,
				UsingGroup:     "default",
				UserGroup:      "default",
				StartTime:      time.Now(),
				IsPlayground:   true,
				TokenUnlimited: true,
			}

			tt.relay(ctx, info)

			var task model.Midjourney
			require.NoError(t, db.Order("id DESC").First(&task).Error)
			assert.Zero(t, task.Quota, "a rejected task must not become refundable")
			assert.Equal(t, model.MidjourneyBillingReady, task.BillingStatus)
		})
	}
}
