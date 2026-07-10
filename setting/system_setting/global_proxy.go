package system_setting

// GlobalProxyUrl 全局出站代理地址，支持 http/https/socks5/socks5h。
// 为空表示不启用。渠道自身配置的代理优先于全局代理。
var GlobalProxyUrl = ""

// GlobalProxyDirectFallbackEnabled 全局代理请求失败时是否自动改用直连重试。
var GlobalProxyDirectFallbackEnabled = false
