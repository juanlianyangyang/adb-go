/**
# @Datetime : 2026/5/19 21:27
# @Project  : adb-go
# @File     : discovery.go
# @Author   : 眷恋阳阳
*/

package adb

import (
	"context"
	"fmt"

	"github.com/grandcat/zeroconf"
)

// DeviceState 定义设备当前所处的配对/连接状态
type DeviceState int

const (
	// StateReadyToConnect 设备已授权，可以直接连接
	// 对应的 mDNS 服务类型: _adb-tls-connect._tcp
	StateReadyToConnect DeviceState = iota

	// StateWaitingForPair 设备等待配对，需要输入 6 位配对码
	// 对应的 mDNS 服务类型: _adb-tls-pairing._tcp
	StateWaitingForPair
)

// String 返回设备状态的可读字符串表示
func (s DeviceState) String() string {
	switch s {
	case StateReadyToConnect:
		return "连接端口"
	case StateWaitingForPair:
		return "等待配对"
	default:
		return "未知状态"
	}
}

// DeviceEvent 统一的 ADB 设备发现事件封装
type DeviceEvent struct {
	State      DeviceState            // 设备状态
	IP         string                 // 设备 IPv4 地址
	Port       int                    // 服务端口
	InstanceID string                 // mDNS 实例 ID
	HostName   string                 // 设备主机名
	TXTRecords []string               // TXT 记录信息
	RawInfo    *zeroconf.ServiceEntry // 原始服务条目，保留供特殊需求使用
}

// StartDiscovery 启动 ADB mDNS 设备发现服务。
// ctx: 用于控制搜索的生命周期，调用 ctx 的 cancel 函数即可停止搜索；
// onDeviceFound: 发现设备时的回调函数，所有状态的设备都会触发此回调。
func StartDiscovery(ctx context.Context, onDeviceFound func(event DeviceEvent), options ...zeroconf.ClientOption) error {
	resolver, err := zeroconf.NewResolver(options...)
	if err != nil {
		return fmt.Errorf("初始化 mDNS 解析器失败: %w", err)
	}

	// 创建独立的接收通道
	connectEntries := make(chan *zeroconf.ServiceEntry)
	pairingEntries := make(chan *zeroconf.ServiceEntry)

	// 后台协程：处理已授权、可以直接连接的设备
	go func() {
		for entry := range connectEntries {
			if len(entry.AddrIPv4) > 0 {
				LogInfof("[mDNS] 发现已授权设备: %s:%d (%s)", entry.AddrIPv4[0], entry.Port, entry.Instance)
				onDeviceFound(DeviceEvent{
					State:      StateReadyToConnect,
					IP:         entry.AddrIPv4[0].String(),
					Port:       entry.Port,
					RawInfo:    entry,
					InstanceID: entry.Instance,
					HostName:   entry.HostName,
					TXTRecords: entry.Text,
				})
			}
		}
	}()

	// 后台协程：处理正处于配对模式的设备
	go func() {
		for entry := range pairingEntries {
			if len(entry.AddrIPv4) > 0 {
				LogInfof("[mDNS] 发现待配对设备: %s:%d (%s)", entry.AddrIPv4[0], entry.Port, entry.Instance)
				onDeviceFound(DeviceEvent{
					State:   StateWaitingForPair,
					IP:      entry.AddrIPv4[0].String(),
					Port:    entry.Port,
					RawInfo: entry,
				})
			}
		}
	}()

	// 启动非阻塞的 mDNS 搜索

	// 1. 查找配对服务
	err = resolver.Browse(ctx, "_adb-tls-pairing._tcp", "local.", pairingEntries)
	if err != nil {
		return fmt.Errorf("启动配对服务搜索失败: %w", err)
	}

	// 2. 查找连接服务
	err = resolver.Browse(ctx, "_adb-tls-connect._tcp", "local.", connectEntries)
	if err != nil {
		return fmt.Errorf("启动连接服务搜索失败: %w", err)
	}

	LogInfof("[mDNS] ADB 设备双路监听已启动，正在搜索网络中的设备...")
	return nil
}
