package model

// jzlh-agent 蓝图F 反欺诈测试：IP 重合检测正确性、clawback 资金守恒、
// 冻结四挂点拦截、封码后 aff 不再绑定、资格门槛。

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func migrateFraudTables(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&Commission{}, &Withdrawal{}, &UserIPRecord{},
		&CommissionFraudAlert{}, &CommissionRiskUser{}, &CommissionRiskEvent{}))
}

func seedFraudAgentPair(t *testing.T, tag string) (*User, *User) {
	t.Helper()
	agent := &User{
		Username:        "fraud_agent_" + tag,
		AffCode:         "fa_" + tag,
		Status:          common.UserStatusEnabled,
		AgentType:       "normal",
		UsageProfitRate: 0.1,
	}
	require.NoError(t, DB.Create(agent).Error)
	invitee := &User{
		Username:  "fraud_down_" + tag,
		AffCode:   "fd_" + tag,
		Status:    common.UserStatusEnabled,
		InviterId: agent.Id,
	}
	require.NoError(t, DB.Create(invitee).Error)
	return agent, invitee
}

func insertIPRecord(t *testing.T, userId int, ip string) {
	t.Helper()
	require.NoError(t, DB.Create(&UserIPRecord{UserId: userId, Ip: ip, Action: "login"}).Error)
}

// TestDetectCommissionFraudByIPOverlap 检测正确性：
// 共享 IP 的代理-下级产生告警且 shared_ip_count 正确；无重合的下级不产生；
// 重复扫描不重复告警；重合消失后 detected 自动清除；dismissed 后重合仍在会复活新告警。
func TestDetectCommissionFraudByIPOverlap(t *testing.T) {
	migrateFraudTables(t)
	agent, invitee := seedFraudAgentPair(t, "detect")
	cleanInvitee := &User{
		Username:  "fraud_down_clean",
		AffCode:   "fd_clean",
		Status:    common.UserStatusEnabled,
		InviterId: agent.Id,
	}
	require.NoError(t, DB.Create(cleanInvitee).Error)

	insertIPRecord(t, agent.Id, "203.0.113.7")
	insertIPRecord(t, agent.Id, "203.0.113.8")
	insertIPRecord(t, invitee.Id, "203.0.113.7") // 与代理重合
	insertIPRecord(t, invitee.Id, "198.51.100.9")
	insertIPRecord(t, cleanInvitee.Id, "192.0.2.55") // 无重合

	newAlerts, err := DetectCommissionFraud(30, false)
	require.NoError(t, err)
	assert.Equal(t, 1, newAlerts)

	var alert CommissionFraudAlert
	require.NoError(t, DB.Where("agent_id = ? AND invitee_id = ?", agent.Id, invitee.Id).First(&alert).Error)
	assert.Equal(t, FraudAlertStatusDetected, alert.Status)
	assert.Equal(t, 1, alert.SharedIpCount)
	assert.Contains(t, alert.SharedIps, "203.0.113.7")

	var cleanCount int64
	require.NoError(t, DB.Model(&CommissionFraudAlert{}).
		Where("invitee_id = ?", cleanInvitee.Id).Count(&cleanCount).Error)
	assert.EqualValues(t, 0, cleanCount, "无重合的下级不应产生告警")

	// 重复扫描：不新增，就地更新
	newAlerts, err = DetectCommissionFraud(30, false)
	require.NoError(t, err)
	assert.Equal(t, 0, newAlerts)

	// 重合消失（记录被清理）→ detected 自动删除
	require.NoError(t, DB.Where("user_id = ?", invitee.Id).Delete(&UserIPRecord{}).Error)
	_, err = DetectCommissionFraud(30, false)
	require.NoError(t, err)
	var gone int64
	require.NoError(t, DB.Model(&CommissionFraudAlert{}).
		Where("agent_id = ? AND invitee_id = ? AND status = ?", agent.Id, invitee.Id, FraudAlertStatusDetected).
		Count(&gone).Error)
	assert.EqualValues(t, 0, gone, "重合消失后 detected 告警应被清除")

	// dismissed 后重合仍在 → 复活新 detected（防误报放行后作弊继续被永久忽略）
	insertIPRecord(t, invitee.Id, "203.0.113.7")
	_, err = DetectCommissionFraud(30, false)
	require.NoError(t, err)
	require.NoError(t, ReviewCommissionFraudAlert(mustLatestDetectedAlertId(t, agent.Id, invitee.Id), FraudActionDismiss, 1, "办公室 NAT"))
	newAlerts, err = DetectCommissionFraud(30, false)
	require.NoError(t, err)
	assert.Equal(t, 1, newAlerts, "dismissed 后重合仍在应新建告警")
}

func mustLatestDetectedAlertId(t *testing.T, agentId, inviteeId int) int {
	t.Helper()
	var alert CommissionFraudAlert
	require.NoError(t, DB.Where("agent_id = ? AND invitee_id = ? AND status = ?",
		agentId, inviteeId, FraudAlertStatusDetected).Order("id desc").First(&alert).Error)
	return alert.Id
}

// TestDetectCommissionFraudRegisterIp register_ip 参与交集：下级注册 IP 命中代理 IP 集即告警。
func TestDetectCommissionFraudRegisterIp(t *testing.T) {
	migrateFraudTables(t)
	agent, invitee := seedFraudAgentPair(t, "regip")
	insertIPRecord(t, agent.Id, "203.0.113.99")
	require.NoError(t, DB.Model(&User{}).Where("id = ?", invitee.Id).
		Update("register_ip", "203.0.113.99").Error)

	newAlerts, err := DetectCommissionFraud(30, false)
	require.NoError(t, err)
	assert.Equal(t, 1, newAlerts)
}

// TestReviewFraudAlertClawback 追回的资金守恒：
// pending 流水翻 confiscated 永不结转；matured 净额走负记录冲销余额与累计；
// 下级解绑、aff_count 递减；重复处置被并发闸门拒绝。
func TestReviewFraudAlertClawback(t *testing.T) {
	migrateFraudTables(t)
	agent, invitee := seedFraudAgentPair(t, "claw")
	require.NoError(t, DB.Model(&User{}).Where("id = ?", agent.Id).Updates(map[string]interface{}{
		"aff_count":                1,
		"commission_quota":         150, // matured 200 - 回冲 50
		"commission_history_quota": 250, // 100(pending) + 200(matured) - 50(回冲)
	}).Error)
	require.NoError(t, DB.Create(&Commission{
		AgentId: agent.Id, FromUserId: invitee.Id, Quota: 100, Status: CommissionStatusPending, Rate: 0.1,
	}).Error)
	require.NoError(t, DB.Create(&Commission{
		AgentId: agent.Id, FromUserId: invitee.Id, Quota: 200, Status: CommissionStatusMatured, Rate: 0.1,
	}).Error)
	require.NoError(t, DB.Create(&Commission{
		AgentId: agent.Id, FromUserId: invitee.Id, Quota: -50, Status: CommissionStatusMatured, Rate: 0.1,
	}).Error)

	alert := &CommissionFraudAlert{
		AgentId: agent.Id, InviteeId: invitee.Id,
		SharedIps: `["203.0.113.1"]`, SharedIpCount: 1,
		Status: FraudAlertStatusDetected, DetectedAt: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(alert).Error)

	require.NoError(t, ReviewCommissionFraudAlert(alert.Id, FraudActionClawback, 42, "自邀实锤"))

	var reloadedAgent User
	require.NoError(t, DB.First(&reloadedAgent, agent.Id).Error)
	assert.Equal(t, 0, reloadedAgent.CommissionQuota, "matured 净额 150 应被冲销")
	assert.Equal(t, 0, reloadedAgent.CommissionHistoryQuota, "累计应扣 pending 100 + matured 净 150")
	assert.Equal(t, 0, reloadedAgent.AffCount)

	var reloadedInvitee User
	require.NoError(t, DB.First(&reloadedInvitee, invitee.Id).Error)
	assert.Equal(t, 0, reloadedInvitee.InviterId, "clawback 应同时解绑")

	var confiscated int64
	require.NoError(t, DB.Model(&Commission{}).
		Where("agent_id = ? AND from_user_id = ? AND status = ?", agent.Id, invitee.Id, CommissionStatusConfiscated).
		Count(&confiscated).Error)
	assert.EqualValues(t, 1, confiscated, "pending 流水应翻为 confiscated")

	var clawRow Commission
	require.NoError(t, DB.Where("source_key = ?", fmt.Sprintf("clawback:alert:%d", alert.Id)).First(&clawRow).Error)
	assert.Equal(t, -150, clawRow.Quota, "负记录应冲销 matured 净额")

	var reloadedAlert CommissionFraudAlert
	require.NoError(t, DB.First(&reloadedAlert, alert.Id).Error)
	assert.Equal(t, FraudAlertStatusResolved, reloadedAlert.Status)
	assert.Equal(t, FraudActionClawback, reloadedAlert.ResolvedAction)
	assert.Equal(t, 250, reloadedAlert.ClawbackQuota)

	// 事件留痕
	var events int64
	require.NoError(t, DB.Model(&CommissionRiskEvent{}).
		Where("user_id = ? AND action = ?", agent.Id, FraudActionClawback).Count(&events).Error)
	assert.EqualValues(t, 1, events)

	// 并发闸门：已 resolved 的告警再处置直接拒绝
	err := ReviewCommissionFraudAlert(alert.Id, FraudActionClawback, 42, "")
	assert.ErrorIs(t, err, ErrFraudAlertAlreadyResolved)
}

// TestReviewFraudAlertUnbindOnly 仅解绑不追回：余额不动、流水不动、关系清零。
func TestReviewFraudAlertUnbindOnly(t *testing.T) {
	migrateFraudTables(t)
	agent, invitee := seedFraudAgentPair(t, "unbind")
	require.NoError(t, DB.Model(&User{}).Where("id = ?", agent.Id).Updates(map[string]interface{}{
		"commission_quota": 300, "commission_history_quota": 300,
	}).Error)
	require.NoError(t, DB.Create(&Commission{
		AgentId: agent.Id, FromUserId: invitee.Id, Quota: 300, Status: CommissionStatusMatured, Rate: 0.1,
	}).Error)
	alert := &CommissionFraudAlert{
		AgentId: agent.Id, InviteeId: invitee.Id, Status: FraudAlertStatusDetected,
		DetectedAt: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(alert).Error)

	require.NoError(t, ReviewCommissionFraudAlert(alert.Id, FraudActionUnbind, 42, ""))

	var reloadedAgent User
	require.NoError(t, DB.First(&reloadedAgent, agent.Id).Error)
	assert.Equal(t, 300, reloadedAgent.CommissionQuota, "unbind 不动余额")
	var reloadedInvitee User
	require.NoError(t, DB.First(&reloadedInvitee, invitee.Id).Error)
	assert.Equal(t, 0, reloadedInvitee.InviterId)
}

// TestFreezeBlocksCommissionExits 冻结四挂点：
// 转额度/提现入口拦截、pending 不结转、待审核提现单自动拒绝并退回、解除后恢复。
func TestFreezeBlocksCommissionExits(t *testing.T) {
	migrateFraudTables(t)
	agent, _ := seedFraudAgentPair(t, "freeze")
	require.NoError(t, DB.Model(&User{}).Where("id = ?", agent.Id).Updates(map[string]interface{}{
		"commission_quota": 1000000, "commission_history_quota": 1000000,
	}).Error)

	// 预置一张待审核提现单（模拟已预扣）
	w := &Withdrawal{
		UserId: agent.Id, Amount: 600000, Method: "alipay",
		PayeeName: "测试", PayeeAccount: "13800000000", Status: WithdrawalPending,
	}
	require.NoError(t, DB.Create(w).Error)

	rejected, err := ApplyCommissionRiskControls(agent.Id, 42, true, false, "IP 重合调查中")
	require.NoError(t, err)
	assert.Equal(t, 1, rejected, "冻结应自动拒绝待审核提现单")

	var reloadedW Withdrawal
	require.NoError(t, DB.First(&reloadedW, w.Id).Error)
	assert.Equal(t, WithdrawalRejected, reloadedW.Status)
	var reloadedAgent User
	require.NoError(t, DB.First(&reloadedAgent, agent.Id).Error)
	assert.Equal(t, 1600000, reloadedAgent.CommissionQuota, "拒单退回预扣余额")

	// 出口拦截
	assert.ErrorIs(t, ConvertCommissionToQuota(agent.Id, 500000), ErrCommissionAssetsFrozen)
	_, err = CreateWithdrawal(agent.Id, 600000, "alipay", "测试", "13800000000", "")
	assert.ErrorIs(t, err, ErrCommissionAssetsFrozen)

	// pending 不结转
	origMature := common.AgentCommissionMatureMinutes
	common.AgentCommissionMatureMinutes = 1
	t.Cleanup(func() { common.AgentCommissionMatureMinutes = origMature })
	pendingRow := &Commission{
		AgentId: agent.Id, FromUserId: 99999, Quota: 777, Status: CommissionStatusPending, Rate: 0.1,
	}
	require.NoError(t, DB.Create(pendingRow).Error)
	require.NoError(t, DB.Model(pendingRow).Update("created_at", common.GetTimestamp()-3600).Error)
	MatureAgentCommissions(agent.Id)
	var frozenPending Commission
	require.NoError(t, DB.First(&frozenPending, pendingRow.Id).Error)
	assert.Equal(t, CommissionStatusPending, frozenPending.Status, "冻结期间 pending 不结转")

	// 解除管制后恢复：结转生效、出口放开
	require.NoError(t, RemoveCommissionRiskControls(agent.Id, 42, "误报"))
	MatureAgentCommissions(agent.Id)
	var maturedRow Commission
	require.NoError(t, DB.First(&maturedRow, pendingRow.Id).Error)
	assert.Equal(t, CommissionStatusMatured, maturedRow.Status)
	assert.NoError(t, ConvertCommissionToQuota(agent.Id, 500000))
}

// TestBlockedInviteCode 封码后 aff_code 解析失效，新注册不再绑定。
func TestBlockedInviteCode(t *testing.T) {
	migrateFraudTables(t)
	agent, _ := seedFraudAgentPair(t, "block")

	id, err := GetUserIdByAffCode(agent.AffCode)
	require.NoError(t, err)
	assert.Equal(t, agent.Id, id)

	_, err = ApplyCommissionRiskControls(agent.Id, 42, false, true, "刷号")
	require.NoError(t, err)

	id, err = GetUserIdByAffCode(agent.AffCode)
	assert.Error(t, err, "封码后解析应失败(注册路径忽略错误→inviterId=0)")
	assert.Equal(t, 0, id)

	require.NoError(t, RemoveCommissionRiskControls(agent.Id, 42, ""))
	id, err = GetUserIdByAffCode(agent.AffCode)
	require.NoError(t, err)
	assert.Equal(t, agent.Id, id)
}

// TestInviteeMinAgeGate 资格门槛：注册不满 N 天的下级消费不计佣，满了才计。
func TestInviteeMinAgeGate(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	migrateFraudTables(t)
	_, invitee := seedFraudAgentPair(t, "minage")

	origDays := common.AgentInviteeMinAgeDays
	common.AgentInviteeMinAgeDays = 7
	t.Cleanup(func() { common.AgentInviteeMinAgeDays = origDays })

	before := countCommissions(t)
	RecordAgentCommission(invitee.Id, 1000, "")
	assert.Equal(t, before, countCommissions(t), "新注册下级不满 7 天不应计佣")

	// 注册时间回拨 8 天 → 计佣恢复
	require.NoError(t, DB.Model(&User{}).Where("id = ?", invitee.Id).
		Update("created_at", common.GetTimestamp()-8*86400).Error)
	invalidateUserCache(invitee.Id)
	RecordAgentCommission(invitee.Id, 1000, "")
	assert.Equal(t, before+1, countCommissions(t), "满 7 天后应正常计佣")
}
