---
description: 启动或复用本地 agentwatch 看板，返回浏览器地址
allowed-tools: Bash(sh *)
---

在当前项目目录执行下面的命令，不要先 cd。第一个参数是安装时选定的界面语言：

```sh
sh "${CLAUDE_PLUGIN_ROOT}/scripts/ensure-server.sh" "${user_config.language}"
```

成功时把最后一行的 URL 告诉用户。退出码非 0 时，把 stderr 原样转述给用户。
