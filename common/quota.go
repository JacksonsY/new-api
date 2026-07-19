package common

func GetTrustQuota() int {
	// 安全加固：返回 0 关闭「信任额度旁路」。
	// 原值 10*QuotaPerUnit 让余额略高于 10 单位或 unlimited 的令牌在入口零预留、
	// 仅事后结算，叠加并发突发可在任一请求结算前集体超支（运营方买单）。
	// shouldTrust() 在 trustQuota<=0 时直接返回 false，所有请求改走正常预扣预留。
	// 与本 fork「预扣与结算都必须安全」的计费红线一致。
	return 0
}
