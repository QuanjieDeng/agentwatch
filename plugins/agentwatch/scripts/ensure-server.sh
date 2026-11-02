#!/bin/sh
# 启动或复用本地 agentwatch 服务，成功时打印看板 URL：先本机 localhost，再局域网 IP。
set -eu

project=$(pwd -P)
query=
requested=${AGENTWATCH_PORT:-}

# 第一个参数是安装时选的界面语言，只认 zh / en；其它值（比如技能里的占位符原样传进来）忽略。
lang=${1:-${AGENTWATCH_LANG:-}}
case "$lang" in
	zh|en) query="?lang=$lang" ;;
esac

# 服务绑定所有网卡（main.go 里是 :port），从另一台机器（Windows + VSCode Remote）打开时
# localhost 指向那台机器自己，所以再给出本机 IP 直连地址。
lan_ip=$(hostname -I 2>/dev/null | awk '{print $1}') || lan_ip=

# 端口状态：mine=本项目已有服务，free=可以启动，busy=别的项目或别的服务占着。
port_state() {
	body=$(curl -fsS --max-time 1 "http://localhost:$1/api/snapshot" 2>/dev/null) || body=
	if [ -n "$body" ]; then
		watched=$(printf '%s' "$body" | sed -n 's/.*"project":"\([^"]*\)".*/\1/p')
		if [ "$watched" = "$project" ]; then
			echo mine
		else
			echo busy
		fi
		return 0
	fi
	# 不是 agentwatch：能连上就是被别的服务占用；只有连接被拒（curl 7）才算空闲。
	curl -sS --max-time 1 -o /dev/null "http://localhost:$1/" >/dev/null 2>&1 && { echo busy; return 0; }
	if [ $? -eq 7 ]; then echo free; else echo busy; fi
}

# 显式指定端口就只用它；否则从 7788 起找，同一台机器多个用户/项目各占一个。
if [ -n "$requested" ]; then
	candidates=$requested
else
	candidates="7788 7789 7790 7791 7792 7793 7794 7795"
fi

port=
# 先找已经为本项目跑着的服务，避免同一项目起两个。
for p in $candidates; do
	if [ "$(port_state "$p")" = mine ]; then
		port=$p
		break
	fi
done
reuse=${port:+1}
# 没有再取第一个空闲端口。
if [ -z "$port" ]; then
	for p in $candidates; do
		if [ "$(port_state "$p")" = free ]; then
			port=$p
			break
		fi
	done
fi
if [ -z "$port" ]; then
	if [ -n "$requested" ]; then
		echo "agentwatch：端口 $requested 已被别的项目或服务占用，请用 AGENTWATCH_PORT 换一个端口" >&2
	else
		echo "agentwatch：端口 7788-7795 都被占用，请用 AGENTWATCH_PORT 换一个端口" >&2
	fi
	exit 1
fi

base="http://localhost:$port"
lan_url=${lan_ip:+"http://$lan_ip:$port$query"}

print_urls() {
	echo "$base$query"
	if [ -n "$lan_url" ]; then
		echo "$lan_url"
	fi
}

alive() { curl -fsS --max-time 1 "$base/api/snapshot" >/dev/null 2>&1; }

if [ -n "$reuse" ]; then
	print_urls
	exit 0
fi

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
echo "agentwatch：15 秒内服务没有在端口 $port 就绪（可能刚被他人占用），日志见 $logfile" >&2
exit 1
