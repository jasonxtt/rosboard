#!/bin/sh
# rosboard 通过"优雅退出进程"来请求重启（首次保存设备、修改关键配置等场景），
# 由 systemd 或容器重启策略负责拉起。本脚本在容器内再兜一层循环拉起，
# 即使用户忘记 --restart 也能自愈；进程以非 0 码退出视为故障，立即退出
# 交给 Docker 的重启策略与人工排查，避免无限崩溃循环。

set -u

# 带参数的一律是子命令（version / admin / supervise 等），直接执行不循环
if [ "$#" -gt 0 ]; then
  exec /usr/local/bin/rosboard "$@"
fi

child=
stopping=

forward() {
  stopping=1
  [ -n "$child" ] && kill -TERM "$child" 2>/dev/null
}
trap forward TERM INT

while :; do
  /usr/local/bin/rosboard "$@" &
  pid=$!
  child=$pid
  wait "$pid"
  code=$?
  child=
  if [ "$code" -gt 128 ] && [ "$stopping" = 1 ]; then
    # wait 被 docker stop 的信号打断；等进程真正退出后再退出容器
    wait "$pid" 2>/dev/null
    code=$?
  fi
  if [ "$stopping" = 1 ]; then
    exit 0
  fi
  if [ "$code" -ne 0 ]; then
    exit "$code"
  fi
  sleep 1
done
