//go:build linux

package main

// automemlimit 在容器内读取 cgroup 内存上限并自动设置 GOMEMLIMIT，
// 让 GC 感知容器内存限制、避免被 OOM-kill；非容器环境无副作用。
// 可用环境变量 AUTOMEMLIMIT 调整比例（默认 0.9），AUTOMEMLIMIT=off 关闭。
import _ "github.com/KimMachineGun/automemlimit"
