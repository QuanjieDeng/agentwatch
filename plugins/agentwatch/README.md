# agentwatch 插件

在 Claude Code 或 Codex 里一句话打开 agentwatch 看板：当前项目的 Claude Code 与 Codex 会话各占一间 3D 办公室，agent 的思考、工具调用与卡住状态实时可见。

## 安装

```sh
# Claude Code：安装范围 user（默认）/ project / local，界面语言 zh / en
claude plugin marketplace add QuanjieDeng/agentwatch
claude plugin install agentwatch@agentwatch --scope project --config language=zh

# Codex
codex plugin marketplace add QuanjieDeng/agentwatch
codex plugin add agentwatch@agentwatch
```

在 Claude Code 的 `/plugin` 里交互式安装时，会直接询问安装范围和界面语言。语言之后可用 `claude plugin configure agentwatch` 修改，重新运行 `/agentwatch` 生效。

首次运行时本机还没有源码，脚本会自动浅克隆仓库到 `~/tools/agentwatch`。

## 包含什么

- skill `agentwatch`：两端共用，agent 运行启动脚本并把看板 URL 交给你。
- Claude Code 命令 `/agentwatch`：调用同一个脚本。
- `scripts/ensure-server.sh`：幂等启动脚本。端口上已有服务就直接复用；否则以调用时所在目录为项目，在后台 `go run` 起服务，日志写到工具目录的 `log/agentwatch.log`。

插件不带 hook，看板只在你要时启动。

## 依赖

- agentwatch 源码目录：首次运行自动克隆到 `~/tools/agentwatch`，其他位置用 `AGENTWATCH_HOME` 指定。
- PATH 里有 `go`、`git` 与 `curl`。

## 环境变量

- `AGENTWATCH_PORT`：端口，默认 7788。
- `AGENTWATCH_HOME`：agentwatch 源码目录。
- `AGENTWATCH_LANG`：界面语言 `zh` / `en`，命令行第一个参数或插件选项优先。

## 数据

服务只在本机读 `~/.claude` 与 `~/.codex/sessions` 下的会话日志，没有外发与遥测。它绑定所有网卡（`:端口`），所以启动脚本会同时给出 `localhost` 和本机局域网 IP 两个地址：后者用于从另一台机器（如 Windows 连远程 Linux）直接打开。看板没有鉴权，只在可信网络内使用。
