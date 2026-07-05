# pkg/epay — epay 客户端（两代商户协议）

epay 客户端，同时支持两代商户协议，字段与签名逐条对齐 **官方 SDK + 平台实测接口文档**：

- **v1（MD5，经典版）**：`submit.php` 页面支付 / `mapi.php` API 直付 / `api.php` 查单退款，参数 MD5 加盐签名。
- **v2（RSA，新版）**：`api/pay/*`、`api/merchant/*`、`api/transfer/*` REST 端点，商户私钥 `SHA256WithRSA` 签请求、平台公钥验响应与回调，`timestamp` ±300s 防重放。

取代了功能残缺的旧客户端库（只有发起支付 + 回调验签）。

## 配置与协议选择

```go
client, err := epay.NewClient(&epay.Config{
    Version:            epay.VersionV2,        // 空或 "v1" 走 MD5；"v2" 走 RSA
    BaseURL:            "https://<你的易支付接口地址>", // 平台「接口地址」，末尾斜杠可有可无
    PID:                "1001",                // 商户 ID
    // v1：
    Key:                "商户密钥",             // MD5 加盐 key
    // v2：平台公钥 + 商户私钥（裸 base64 DER，或带 PEM 头尾均可）
    PlatformPublicKey:  "MIIBIjANBgkq...",
    MerchantPrivateKey: "MIIEvQIBADANBgkq...",
})
```

> 平台「接口地址」不一定等于商户后台域名——有些平台把 API 单独放在独立主机上（以商户后台「开发文档 / API 信息」里标注的接口地址为准）。**若把后台域名填成接口地址，v2 请求会打到站点页面、被平台回一张 `系统发生错误` 的 HTML**，能力检测会提示「接口返回非 JSON」。

应用层不直接 `NewClient`，而是 `service.GetEpayClient()`：当 RSA 与 MD5 密钥都配置时**优先 RSA、构建失败降级 MD5**；`service.ProbeEpayCapabilities()` 做能力检测时，若 v2 不可用且配了 MD5 会自动改测 MD5 并在结论里说明。

## 能力矩阵

| 能力 | 方法 | v1(MD5) | v2(RSA) | 端点 |
|---|---|:--:|:--:|---|
| 页面跳转支付 | `Purchase` | ✅ | ✅ | `submit.php` / `api/pay/submit` |
| API 直付（扫码） | `CreateOrder` | ✅ | ✅ | `mapi.php` / `api/pay/create` |
| 回调验签 | `VerifyNotify` | ✅ | ✅ | 本地 |
| 查单 | `QueryOrderByOutTradeNo` | ✅ | ✅ | `api.php?act=order` / `api/pay/query` |
| 退款 | `Refund` | ✅ | ✅ | `api.php?act=refund` / `api/pay/refund` |
| 退款查询 | `RefundQuery` | ❌ | ✅ | `api/pay/refundquery` |
| 关闭订单 | `CloseOrder` | ❌ | ✅ | `api/pay/close` |
| 商户信息 | `MerchantInfoQuery` | ❌ | ✅ | `api/merchant/info` |
| 订单列表 | `ListOrders` | ❌ | ✅ | `api/merchant/orders` |
| 代付 | `Transfer` | ❌ | ✅ | `api/transfer/submit` |
| 代付查询 | `TransferQuery` | ❌ | ✅ | `api/transfer/query` |
| 余额查询 | `Balance` | ❌ | ✅ | `api/transfer/balance` |
| 能力探测 | `ProbeCapabilities` | ✅ | ✅ | 查一个不存在的订单号 |

v2 专有能力在 v1 下返回 `errUnsupportedInV1`。

## 签名规则

**待签名串（两代共用）**：取所有**非空**参数，**剔除 `sign` / `sign_type`**，按键名 **ASCII 升序**排序，拼成 `k=v&k=v`（值不做 URL 编码）。

- **v1**：`sign = md5(待签名串 + 商户Key)`，小写十六进制，`sign_type=MD5`。
- **v2**：`sign = base64( SHA256WithRSA(待签名串, 商户私钥) )`，`sign_type=RSA`；响应/回调用平台公钥验签，并校验 `timestamp` 在 ±300s 内。

因为待签名串是**动态拼所有非空参数**的，新增任何请求字段都会自动进签名，无需改签名逻辑。

## 发起支付

页面跳转（返回提交地址 + 已签名表单，交前端自动提交 / 拼 URL 跳转）：

```go
url, params, err := client.Purchase(&epay.PurchaseArgs{
    Type: "alipay", ServiceTradeNo: tradeNo, Name: "充值", Money: "1.00",
    NotifyUrl: notifyURL, ReturnUrl: returnURL,
})
```

API 直付 / 扫码（服务端下单，返回可站内渲染的支付载体）：

```go
res, err := client.CreateOrder(&epay.PurchaseArgs{
    Type: "wxpay", ServiceTradeNo: tradeNo, Name: "充值", Money: "1.00",
    ClientIP: clientIP, NotifyUrl: notifyURL,
    // Method 留空默认 "web"（v2 专用）
})
// res.QRCode / res.PayURL 已按 pay_type 归一，可直接渲染
```

**v2 `create` 的两处关键差异（平台实测，旧 SDK 没覆盖）**：

1. **必带 `method`（接口类型）**，本库默认 `web`。取值：`web`（按 device 自动返回 二维码/跳转URL/小程序参数）、`jump`（仅跳转 URL）、`jsapi`（需 `sub_openid`+`sub_appid`）、`app`、`scan`（付款码，需 `AuthCode`）、`applet`。
2. **响应是 `pay_type` + `pay_info`**（不是 `qrcode`/`payurl`）。本库据 `pay_type` 归一：

| `pay_type` | `pay_info` 内容 | 映射到 |
|---|---|---|
| `qrcode` | 二维码原始内容（协议 URI 串） | `res.QRCode` |
| `jump` | 跳转 URL | `res.PayURL` |
| `jsapi` / `scan` / `wxplugin` / `wxapp` | 一段 JSON 参数串 | 仅 `res.PayType` / `res.PayInfo`，交端侧 SDK |

v1 `mapi` 仍是老格式（`code==1` + `payurl`/`qrcode`/`urlscheme`），本库自动区分。

## 回调验签

```go
result, err := client.VerifyNotify(params) // params = GET query / POST form 全字段
if err == nil && result.VerifyStatus && result.TradeStatus == epay.StatusTradeSuccess {
    // 入账；随后向平台输出字符串 "success"
}
```

`VerifyStatus` = 验签通过 **且 `pid` 与本商户一致**。pid 绑定是关键防线：v2 平台公钥全平台共享，不校验 pid 则他人商户的合法回调可冒充本商户骗取入账。

## 查单 / 退款 / 对账

```go
info, err := client.QueryOrderByOutTradeNo(outTradeNo) // v2 用 out_trade_no（平台文档确认支持）
// info.Found / info.Paid(status==1) / info.Money / info.PID

r, err := client.Refund(&epay.RefundArgs{OutTradeNo: outTradeNo, Money: "1.00"})
// TradeNo / OutTradeNo 二选一；OutRefundNo 选填（v2 留空由平台生成，v1 不用）

rq, err := client.RefundQuery(outRefundNo, "") // v2：rq.Success = status==1
```

订单状态（v2 查单）：`0` 未支付 / `1` 已支付 / `2` 已退款 / `3` 冻结 / `4` 预授权。

## 商户信息 / 代付（v2）

```go
mi, _ := client.MerchantInfoQuery()      // mi.Money 余额、结算信息、当日/昨日订单统计
bal, _ := client.Balance()               // bal.AvailableMoney 可用余额、bal.TransferRate 费率
list, _ := client.ListOrders(0, 50, -1)  // offset 从 0，limit≤50，status<0 不过滤

tr, _ := client.Transfer(&epay.TransferArgs{Type: "alipay", Account: "u@x.com", Money: "1.00"})
// tr.Status：0 处理中 / 1 成功；处理中用 TransferQuery(outBizNo, bizNo) 复查
tq, _ := client.TransferQuery(tr.OutBizNo, "")
// tq.Status：0 处理中 / 1 成功 / 2 失败（失败时 tq.ErrMsg）
```

## 能力探测（无副作用）

`ProbeCapabilities` 查一个高概率不存在的订单号做真实往返：拿到结构化 JSON（v1）/ 响应验签通过（v2）即证明**平台可达 + 凭证有效**，据此推断各接口能力。拿到 HTML/非 JSON 会判为**「可达但接口不匹配」**（而非「不可达」），提示核对平台地址 / 是否开启 v2 接口。

## 端点对照

| | v1（MD5） | v2（RSA） |
|---|---|---|
| 页面跳转 | `submit.php` | `api/pay/submit` |
| API 直付 | `mapi.php` | `api/pay/create` |
| 查单 | `api.php?act=order` | `api/pay/query` |
| 退款 | `api.php?act=refund` | `api/pay/refund` |
| 退款查询 | — | `api/pay/refundquery` |
| 关单 | — | `api/pay/close` |
| 商户信息 | — | `api/merchant/info` |
| 订单列表 | — | `api/merchant/orders` |
| 代付 / 查询 / 余额 | — | `api/transfer/{submit,query,balance}` |

请求一律 `application/x-www-form-urlencoded`，响应 JSON、UTF-8。

## 安全要点

- v1 查单/退款把 `key` 明文放 query，网络错误经 `sanitizeHTTPError` 剥掉 query 再记录，**不泄漏商户密钥**。
- 响应体限读 1 MiB，禁止自动重定向。
- 回调/响应数字用 `json.Number` 保留原文，避免 `float64` 往返改变验签串导致 v2 验签假阴性。
- 入账前务必校验回调金额与本地订单一致（见 `service.EpayCallbackMoneyMatches`）。
