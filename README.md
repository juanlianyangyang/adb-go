# adb-go

一个纯 Go 实现的 Android ADB（Android Debug Bridge）客户端库，支持无线调试配对、TLS 加密连接、Shell 命令执行、文件传输等核心功能。

**作者**： 眷恋阳阳 [1156803767@qq.com](mailto:1156803767@qq.com)

**AI助手**: Gemini

## 特性

- ✅ **无线调试支持**: 支持 Android 11+ 的无线调试配对协议（SPAKE2+）
- ✅ **TLS 加密**: 支持 ADB STLS 协议，所有通信均经过 TLS 1.3 加密
- ✅ **Shell 交互**: 支持单次命令执行和交互式终端会话
- ✅ **文件传输**: 支持 Push/Pull 文件，带进度回调
- ✅ **端口转发**: 支持正向和反向端口转发
- ✅ **屏幕截图**: 支持 PNG 和 BMP 格式截图
- ✅ **设备控制**: 支持 Reboot、Root、Remount 等操作

## 快速开始

### 1. 加载或生成 RSA 密钥

```go
privKey, err := adb.LoadOrGeneratePrivateKey(false)
if err != nil {
    log.Fatal(err)
}
```

### 2. 配对设备（Android 11+ 无线调试首次连接）

```go
ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
defer cancel()

err := adb.Pair(ctx, "192.168.1.100:38555", "123456", privKey, "MyDevice")
if err != nil {
    log.Fatal(err)
}
```

### 3. 连接设备

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

client, err := adb.Dial(ctx, "192.168.1.100:5555", privKey, "MyDevice")
if err != nil {
    log.Fatal(err)
}
defer client.Close()
```

### 4. 执行 Shell 命令

```go
// 单次命令执行
stream, _ := client.Shell(ctx, "ls", "-l", "/sdcard")
io.Copy(os.Stdout, stream)
stream.Close()

// 交互式 Shell
client.InteractiveShell(ctx, os.Stdin, os.Stdout)
```

### 5. 文件传输

```go
// 推送文件
err := client.PushFile(ctx, "./local.txt", "/data/local/tmp/remote.txt")

// 拉取文件
err := client.PullFile(ctx, "/sdcard/log.txt", "./log.txt")

// 带进度回调
onProgress := func(total, current int64, percent float64) {
    fmt.Printf("进度: %.2f%%\n", percent)
}
err := client.PushFile(ctx, "./large.zip", "/sdcard/large.zip", adb.WithProgress(onProgress))
```

### 6. 屏幕截图

```go
// PNG 格式（使用 screencap 命令）
err := client.Screenshot(ctx, "./screenshot.png")

// BMP 格式（直接读取帧缓冲）
err := client.SaveFramebufferBMP(ctx, "./screenshot.bmp")
```

## API 参考

### 包级别函数

#### 密钥管理

| 函数 | 说明 |
|------|------|
| `LoadOrGeneratePrivateKey(autoGen bool) (*rsa.PrivateKey, error)` | 多级密钥加载器，优先从缓存、嵌入密钥、本地文件加载，最后生成新密钥 |

#### 设备发现

| 函数 | 说明 |
|------|------|
| `StartDiscovery(ctx context.Context, onDeviceFound func(DeviceEvent)) error` | 启动 mDNS 设备发现服务，监听网络中的 ADB 设备 |

#### 配对

| 函数 | 说明 |
|------|------|
| `Pair(ctx context.Context, address string, pairingCode string, privKey *rsa.PrivateKey, deviceName string) error` | 发起设备配对，用于 Android 11+ 无线调试首次连接 |

#### 认证工具

| 函数 | 说明 |
|------|------|
| `GenerateTLSCertificate(priv *rsa.PrivateKey, deviceName string) (tls.Certificate, error)` | 使用 RSA 私钥生成自签名 x509 证书 |
| `Sign(priv *rsa.PrivateKey, token []byte) ([]byte, error)` | 使用 RSA 私钥对认证令牌进行签名 |
| `EncodePublicKey(pub *rsa.PublicKey) ([]byte, error)` | 将 RSA 公钥编码为 ADB 自定义格式 |
| `EncodePublicKeyWithName(pub *rsa.PublicKey, name string) ([]byte, error)` | 将公钥编码为 ADB Base64 格式并附加设备名称 |
| `DecodePublicKey(data []byte) (*rsa.PublicKey, error)` | 解码 ADB 格式的公钥数据 |

---

### Client 类

Client 是对底层 Connection 的高级封装，提供面向开发者的便捷 API。

| 方法 | 说明 |
|------|------|
| `Dial(ctx, address, privKey, deviceName) (*Client, error)` | 拨号连接到 ADB 守护进程并完成握手认证 |
| `Close() error` | 关闭底层连接并释放资源 |
| `Disconnected() <-chan struct{}` | 获取连接断开信号通道 |
| `Connection() *Connection` | 返回底层 Connection 实例 |

#### Shell 操作

| 方法 | 说明 |
|------|------|
| `Shell(ctx context.Context, args ...string) (*Stream, error)` | 在设备上执行 shell 命令 |
| `InteractiveShell(ctx, in io.Reader, out io.Writer) error` | 启动全双工交互式终端会话 |

#### 文件传输

| 方法 | 说明 |
|------|------|
| `Push(ctx, remotePath, reader, mode, mtime, opts) error` | 推送流数据到设备 |
| `Pull(ctx, remotePath, writer, opts) error` | 从设备拉取数据到流 |
| `PushFile(ctx, localPath, remotePath, opts) error` | 推送本地文件到设备 |
| `PushBytes(ctx, remotePath, content, mode, opts) error` | 推送内存数据到设备 |
| `PullFile(ctx, remotePath, localPath, opts) error` | 从设备拉取文件到本地 |
| `PullBytes(ctx, remotePath, opts) ([]byte, error)` | 从设备拉取数据到内存 |

#### 端口转发

| 方法 | 说明 |
|------|------|
| `Forward(ctx, localPort, remotePort) error` | 建立正向端口转发（PC -> 设备） |
| `Reverse(ctx, cmd) (*Stream, error)` | 执行反向端口转发命令 |
| `ReverseForward(ctx, remotePort, localPort) error` | 建立反向端口转发（设备 -> PC） |
| `TCPConnect(ctx, port) (*Stream, error)` | 打开到设备内部端口的 TCP 连接 |

#### 屏幕截图

| 方法 | 说明 |
|------|------|
| `Screenshot(ctx, localPath) error` | 截取屏幕并保存为 PNG 格式 |
| `SaveFramebufferBMP(ctx, localPath) error` | 截取屏幕并保存为 BMP 格式 |
| `Framebuffer(ctx) (*Stream, error)` | 获取设备屏幕的帧缓冲流 |

#### 设备控制

| 方法 | 说明 |
|------|------|
| `Reboot(ctx, target) error` | 触发设备重启（支持 bootloader/recovery/fastboot） |
| `Root(ctx) (disconnect bool, msg string)` | 尝试获取 root 权限 |
| `Unroot(ctx) (disconnect bool, msg string)` | 恢复非 root 权限 |
| `Remount(ctx, args) (string, error)` | 重新挂载系统分区为读写模式 |

#### 其他服务

| 方法 | 说明 |
|------|------|
| `FileSync(ctx) (*Stream, error)` | 请求同步服务，用于文件 Push/Pull |
| `RawOpen(ctx, destination) (*Stream, error)` | 直接以目标字符串打开一条流 |

---

### Connection 类

管理与 ADB 守护进程之间的网络连接，负责握手认证、TLS 加密升级和多路流管理。

| 方法 | 说明 |
|------|------|
| `NewConnection(conn, privKey, deviceName, apiLevel) *Connection` | 创建新的 ADB 连接包装器 |
| `Connect(ctx) error` | 启动 ADB 握手流程和后台消息读取循环 |
| `Open(ctx, destination) (*Stream, error)` | 向 ADB 守护进程发起服务请求 |
| `Close() error` | 关闭底层物理连接并清理所有流资源 |
| `Disconnected() <-chan struct{}` | 获取断开信号通道 |

---

### Stream 类

代表与 ADB 守护进程之间的一条多路复用逻辑流。

| 方法 | 说明 |
|------|------|
| `Read(p []byte) (n int, err error)` | 从流中读取数据（实现 io.Reader） |
| `Write(p []byte) (n int, err error)` | 向流中写入数据（实现 io.Writer） |
| `Close() error` | 关闭流，向对端发送 CLSE 包 |
| `MaxPayload() int` | 返回可以写入的最大载荷大小 |

---

### SyncClient 类

实现 ADB SYNC 协议，用于文件 Push/Pull 操作。

| 方法 | 说明 |
|------|------|
| `NewSyncClient(stream) *SyncClient` | 创建 Sync 客户端 |
| `Close() error` | 关闭客户端并发送 QUIT 请求 |
| `Stat(remotePath) (mode, size, mtime uint32, err error)` | 获取远程文件状态 |
| `Send(remotePath, mode, mtime, reader, opts) error` | 推送数据到设备 |
| `Recv(remotePath, writer, opts) error` | 从设备拉取数据 |

#### SyncOption（功能选项）

| 函数 | 说明 |
|------|------|
| `WithProgress(p ProgressFunc) SyncOption` | 设置进度回调函数 |
| `WithTotalSize(size int64) SyncOption` | 设置文件总大小 |

---

### Service 枚举

定义 ADB 协议支持的各种本地服务类型。

| 服务 | 前缀 | 说明 |
|------|------|------|
| `ServiceShell` | `shell:` | Shell 命令执行服务 |
| `ServiceSync` | `sync:` | 文件同步服务 |
| `ServiceTcpConnect` | `tcp:` | TCP 端口连接服务 |
| `ServiceFramebuffer` | `framebuffer:` | 帧缓冲截图服务 |
| `ServiceRemount` | `remount:` | 重新挂载分区服务 |
| `ServiceReverse` | `reverse:` | 反向端口转发服务 |
| `ServiceBackup` | `backup:` | 备份服务 |
| `ServiceRestore` | `restore:` | 还原服务 |

| 方法 | 说明 |
|------|------|
| `Name() (string, error)` | 返回服务类型对应的基础前缀字符串 |
| `GetDestination(args ...string) (string, error)` | 根据参数生成目标字符串 |

---

### DeviceEvent 结构体

统一的 ADB 设备发现事件封装。

| 字段 | 类型 | 说明 |
|------|------|------|
| `State` | `DeviceState` | 设备状态（已授权/待配对） |
| `IP` | `string` | 设备 IPv4 地址 |
| `Port` | `int` | 服务端口 |
| `InstanceID` | `string` | mDNS 实例 ID |
| `HostName` | `string` | 设备主机名 |
| `TXTRecords` | `[]string` | TXT 记录信息 |
| `RawInfo` | `*zeroconf.ServiceEntry` | 原始服务条目 |

---

### DeviceState 枚举

设备配对/连接状态。

| 状态 | 值 | 说明 |
|------|------|------|
| `StateReadyToConnect` | 0 | 设备已授权，可以直接连接 |
| `StateWaitingForPair` | 1 | 设备等待配对，需要输入 6 位配对码 |

---

### 日志接口

| 函数 | 说明 |
|------|------|
| `SetLogger(l Logger)` | 设置全局日志实现 |
| `GetLogger() Logger` | 获取当前全局日志实现 |
| `SetLogLevel(level int)` | 设置日志级别（Debug/Info/Warn/Error） |
| `NewDefaultLogger(writer, prefix, flag) Logger` | 创建默认日志实例 |

#### 便捷日志函数

| 函数 | 说明 |
|------|------|
| `LogDebugf(format, args)` | 输出调试级别日志 |
| `LogInfof(format, args)` | 输出信息级别日志 |
| `LogWarnf(format, args)` | 输出警告级别日志 |
| `LogErrorf(format, args)` | 输出错误级别日志 |

---

## 协议版本

| 版本 | 载荷大小 | 说明 |
|------|----------|------|
| V1 | 4KB | 兼容旧设备的默认版本 |
| V2 | 256KB | Android 8.0+ |
| V3 | 1MB | Android 9.0+ |

## 命令行工具

项目包含以下示例工具：

- **adb-example**: 交互式 ADB 测试沙盒，支持配对、连接、Shell、文件传输等功能
- **adb-shell**: 简单的 Shell 命令执行工具
- **rsa_generate_key**: RSA 密钥生成工具

## 依赖

```go
require (
    filippo.io/edwards25519 v1.2.0    // SPAKE2+ 椭圆曲线运算
    github.com/grandcat/zeroconf v1.0.0 // mDNS 设备发现
    golang.org/x/crypto v0.51.0         // 加密相关
)
```

## 许可证

本项目采用 **GNU AGPLv3** 协议开源。
