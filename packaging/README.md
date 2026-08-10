# 给朋友安装

## 只使用实时看盘

按朋友 Mac 的 CPU 发送一个文件即可：

- Apple Silicon（M1/M2/M3/M4/M5）：`astock-darwin-arm64`
- Intel Mac：`astock-darwin-amd64`
- 不确定架构：`astock-darwin-universal`

对方运行：

```bash
chmod +x ./astock-darwin-arm64
./astock-darwin-arm64 watch --moyu 600519 000001
```

用方向键或 `j/k` 选择个股，`Enter` 打开完整详情，`Esc` 返回列表；
Page Up/Page Down 或 `b/空格` 跳选/翻页，`g/G` 选择首尾，`q` 退出。
按 `e` 进入两阶段排序（Enter选中、方向键移动、Enter保存），按 `f` 选择、新建或删除自选分组。
详情会加载关联度最高的 6 个行业/概念板块及其资金流、热度排名与领涨股；若近 30 日有龙虎榜，还会展示上榜原因和关键成交数据。
午休、收盘后和周末停止持续拉取，界面仍可操作；持续刷新不会增加主终端的滚动记录。

二进制不需要安装 Go、Node 或 Python。自选股保存在对方自己的
`~/.config/astock/watchlist`。

## 使用策略分析

还必须在对方机器准备：

1. `tradingagents-astock` checkout；
2. Python 3.10+ 虚拟环境并安装该项目依赖；
3. 对方自己的模型 API Key。

然后执行：

```bash
export ASTOCK_TRADINGAGENTS_HOME="$HOME/tradingagents-astock"
export ASTOCK_TRADINGAGENTS_PYTHON="$HOME/tradingagents-astock/.venv/bin/python"
./astock-darwin-arm64 doctor
./astock-darwin-arm64 analyze 600519
```

不要把你的 `.env` 或 API Key 一起打包。
