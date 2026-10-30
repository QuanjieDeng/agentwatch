---
name: agentwatch
description: 打开 agentwatch 实时 3D 看板，查看当前项目的 Claude Code 与 Codex 会话和 agent 状态。用户说「打开 agentwatch」「看板」「看看 agent 在干嘛」「监控台」时使用。
---

# agentwatch

在当前项目目录下运行插件自带的启动脚本，把它打印的 URL 交给用户。

1. 脚本位于本 skill 所在插件的根目录下的 `scripts/ensure-server.sh`，插件根目录就是本 SKILL.md 的上两级目录。找不到时用兜底路径 `$HOME/tools/agentwatch/plugins/agentwatch/scripts/ensure-server.sh`。
2. 在用户的项目目录里执行 `sh <脚本路径>`，不要先 cd 到别处：脚本用调用时的当前目录决定看哪个项目。
3. 脚本成功时最后一行是看板 URL，原样告诉用户，让用户在浏览器打开。已有服务在跑时脚本会直接复用，这是正常结果。
4. 脚本退出码非 0 时，把它的 stderr 原样转述给用户，不要自行重试或改端口。

要换端口，执行前设置环境变量 `AGENTWATCH_PORT`；工具不在默认位置时设置 `AGENTWATCH_HOME`。
