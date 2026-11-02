---
description: 启动或复用本地 agentwatch 看板，返回浏览器地址
allowed-tools: Bash(sh *)
---

在当前项目目录执行下面的命令，不要先 cd。第一个参数是安装时选定的界面语言：

```sh
sh "${CLAUDE_PLUGIN_ROOT}/scripts/ensure-server.sh" "${user_config.language}"
```

成功时把打印的 URL 都告诉用户：第一行 localhost 用于本机或已转发端口的情况，第二行局域网 IP 用于从另一台机器（如 Windows + VSCode Remote）打开；端口可能不是 7788（被占用时自动顺延），以脚本输出为准；两行都打不开时提示用户在 VSCode 的「端口 / PORTS」面板手动转发。退出码非 0 时，把 stderr 原样转述给用户。
