/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
/**
 * 自定义供应商图标：@lobehub/icons 未收录的品牌在这里注册。
 * lobe-icon-impl 解析时会先按 baseKey（"HappyHorse.Color" → "HappyHorse"）命中
 * 这里；命中则渲染自定义组件、忽略 .Color/.Avatar 等变体后缀。因此渠道页、
 * 供应商/模型页、日志徽章等所有走 getLobeIcon 的入口，只要 key 用 "HappyHorse"
 * 就能复用同一张图。
 */
/* eslint-disable react-refresh/only-export-components */

// HappyHorse 官方 logo（84x84 PNG 内嵌为 data URI）。内嵌而非外链，避免自托管/
// 离线环境依赖外部图床——外链会失效或被墙。
const HAPPYHORSE_LOGO =
  'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAFQAAABUCAYAAAAcaxDBAAAAAXNSR0IArs4c6QAAAARzQklUCAgICHwIZIgAAATnSURBVHic7ZtdjF1TFMd/d+6MmUFbZgiKaEUVUeJrSFPRVEgbRRMeiGgqETz4CKF4QxqCB4R4kQYTDR3x1TQV4isqpCGirVQTQWnRUUw/pnTG3Hs9+E9ycnLOmXvvuefsfcb6Jedlzr3nrP2ftfdae619wTAMwzAMwzAMwzAMwzAM4/9B2fH7r5MNg47tmDS8BtSATcAGYAj4C9gGrAaWAke4NrJI3CNBk65fgNs9mE2FYGEdgtaAKrAKOMS1wT7TBrxcp6Dj1wDQ4dpwX1nWoJg1oALc5NpwHzkO2NmEoDVgD9DjegA+UQbWNSnm+HWb60H4xM0KMmkEfQMouR6ID5wA/JlSzBrwrQUn6AI+rEOszUrskz4zAkxzPSDXLEqY6geAj4DFwN3AWB3CX+h6QFG05fiuqDVvGHgGmAssUPR/tM5d0TkZ2FgouoFXgd1aAx8AjgzcPwrY18A6utLdUPyhDBwds4W8pcHA9KUD+wtDCVjfoKB/AFNcG+4rJwF/NyjoAeA014aHyTMoJbFIaVUjdOof4RW+CLqkye+d2mI7UuODoNNT5JSzW2xLanwQ9IYU28g+oDdhX98BXA28rerWDqVuF0/WWkAZ+CLFnr6qvHYz8LxaJWdLyDbg2Zhd1yhw72QU9RQ15dIWS8LXVqC/jnrAAtcCNEufEvEhedISdTPvzEDM4DWmqT4Sc/8918I0w6HANxGD+R3YlbGgo8CVwPkxG4ehIjb+zqizepTVtUFr5VTg69C9/arRtow8ovw8xz31s4Bjgb3A06F73QpiheL1gEfsBT7N2WMrwLmyZTrwT+j+uiJF+05Nq3HjB+Sts4BHgB9yEHcMODlg08rQ/Spwhyc5+YTMDRkf7ql3AZdmHJy2hzYOPcBboc/sK8rUvy/BU1DVfWOGYlaV7EcxH3gQuN/HmkAUpZAnbFUQAGjnv4r9cIIYu9XUq6QQ9OPAOwtPN/BzIDAsC9xbPsGat0brLMAMefpnyisbCUYXOBp7JswJnPW8JhRJ46rzv+kQbnvE8zpUXVqhZ04UzNbnONZc6FIXM6qS9ESEV/YDx9f57E7gInnu41pOwoKuaPF4vGZ+qEe/BTiowWeU5fkfxHjo8oxs95Kpqk0GPXRmg88oAS8mTPnHMrLdW14JCRCX3iTRl3AaZWMGNntBh6o9h4f+Hj5w+04Tzy6pGh+Xg86K+V4vcF5M8POaErBWA/wVuD5QKOlRKziYdx7WxDsWJkT88DpaBm5UT7+mrXBh9vHoIEIl5DVrlf6UlG8Gt3/HNPGOLrVAogR9V3v0NuB04P3QEjGkem2heCpinRsGnlOz7CF5b38Kb7k1QswdOsk3B3ghotVSBR5u8VhzoV05YVQbYhR4CbhMkb9ZOoEfge/0j1mqSv1AzHIwohy2EBWmKErAFZpicdvE7RLj2ibX0tk6br5KW964GsAgcElWa2feC/KJwJM6WJv07grwvVoW21Te2yPPQuvmNB2HnKn1ccYEnYEK8CZwF/BTi8fllHbgKk3PtD9eqOeqqkl4+WT/eeMUFZy3ZCjmVzqZUrhInpbF+mXyYEqvrSpjWK1OQO74lNSWlOyfqdbJPDXXeif43i7gc+ATNQA3Kfg5wSdB4+hRwt8LHKy/7ddBiZ0uxTMMwzAMwzAMwzAMwzAMwzAMw1f+BWG2RU/RYhesAAAAAElFTkSuQmCC'

function HappyHorse({ size = 20 }: { size?: number }) {
  return (
    <img
      src={HAPPYHORSE_LOGO}
      width={size}
      height={size}
      alt='HappyHorse'
      style={{ display: 'block', borderRadius: 4 }}
    />
  )
}

// key 用 @lobehub 图标同款命名（首字母大写），供 getChannelTypeIcon /
// 后端 vendor icon / model-badge 直接以 "HappyHorse" 或 "HappyHorse.Color" 引用。
export const CUSTOM_PROVIDER_ICONS: Record<
  string,
  React.ComponentType<{ size?: number }>
> = {
  HappyHorse,
}
