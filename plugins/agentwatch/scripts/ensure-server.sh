#!/bin/sh
# 启动或复用本地 agentwatch 服务，成功时打印看板 URL：先本机 localhost，再局域网 IP。
set -eu

port=${AGENTWATCH_PORT:-7788}
base="http://localhost:$port"
query=

# 第一个参数是安装时选的界面语言，只认 zh / en；其它值（比如技能里的占位符原样传进来）忽略。
lang=${1:-${AGENTWATCH_LANG:-}}
case "$lang" in
	zh|en) query="?lang=$lang" ;;
esac

# 服务绑定所有网卡（main.go 里是 :port），从另一台机器（Windows + VSCode Remote）打开时
# localhost 指向那台机器自己，所以再给出本机 IP 直连地址。
lan_ip=$(hostname -I 2>/dev/null | awk '{print $1}') || lan_ip=
lan_url=${lan_ip:+"http://$lan_ip:$port$query"}

print_urls() {
	echo "$base$query"
	if [ -n "$lan_url" ]; then
		echo "$lan_url"
	fi
}

alive() { curl -fsS --max-time 1 "$base/api/snapshot" >/dev/null 2>&1; }

# 已有服务就复用，不管它看的是哪个项目：同一端口只能跑一个。
if alive; then
	print_urls
	exit 0
fi

project=$PWD
script_dir=$(cd "$(dirname "$0")" && pwd)
# 从源码目录直接运行时上三级就是工具根；装进插件缓存后不是，用默认位置（首次运行从 GitHub 浅克隆）。
if [ -n "${AGENTWATCH_HOME:-}" ]; then
	home=$AGENTWATCH_HOME
elif [ -f "$script_dir/../../../go.mod" ]; then
	home=$(cd "$script_dir/../../.." && pwd)
else
	home=$HOME/tools/agentwatch
	if [ ! -f "$home/go.mod" ]; then
		if ! command -v git >/dev/null 2>&1; then
			echo "agentwatch：$home 下没有源码，PATH 里也没有 git，无法自动下载；请设置 AGENTWATCH_HOME" >&2
			exit 1
		fi
		mkdir -p "$(dirname "$home")"
		echo "agentwatch：首次运行，正在下载源码到 $home" >&2
		if ! git clone --depth 1 https://github.com/QuanjieDeng/agentwatch.git "$home" >&2; then
			echo "agentwatch：下载失败，请手动克隆 https://github.com/QuanjieDeng/agentwatch 到 $home，或设置 AGENTWATCH_HOME" >&2
			exit 1
		fi
	fi
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
		print_urls
		exit 0
	fi
	sleep 0.5
	i=$((i + 1))
done
echo "agentwatch：15 秒内服务没有就绪，日志见 $logfile" >&2
exit 1
