#!/bin/sh
# 启动或复用本地 agentwatch 服务，成功时最后一行打印看板 URL。
set -eu

port=${AGENTWATCH_PORT:-7788}
url="http://localhost:$port"

alive() { curl -fsS --max-time 1 "$url/api/snapshot" >/dev/null 2>&1; }

# 已有服务就复用，不管它看的是哪个项目：同一端口只能跑一个。
if alive; then
	echo "$url"
	exit 0
fi

project=$PWD
script_dir=$(cd "$(dirname "$0")" && pwd)
# 从源码目录直接运行时上三级就是工具根；装进插件缓存后不是，只能回落到约定位置。
if [ -n "${AGENTWATCH_HOME:-}" ]; then
	home=$AGENTWATCH_HOME
elif [ -f "$script_dir/../../../go.mod" ]; then
	home=$(cd "$script_dir/../../.." && pwd)
else
	home=$HOME/tools/agentwatch
fi
if [ ! -f "$home/go.mod" ]; then
	echo "agentwatch：找不到工具目录（$home 下没有 go.mod），请设置 AGENTWATCH_HOME 指向 agentwatch 源码目录" >&2
	exit 1
fi
if ! command -v go >/dev/null 2>&1; then
	echo "agentwatch：PATH 里没有 go，无法启动服务" >&2
	exit 1
fi

mkdir -p "$home/log"
logfile=$home/log/agentwatch.log
cd "$home"
nohup go run . -project "$project" -port "$port" >"$logfile" 2>&1 &

# 首次 go run 要编译，给足 15 秒。
i=0
while [ $i -lt 30 ]; do
	if alive; then
		echo "$url"
		exit 0
	fi
	sleep 0.5
	i=$((i + 1))
done
echo "agentwatch：15 秒内服务没有就绪，日志见 $logfile" >&2
exit 1
