# 任务：开发 Digital Human Streaming Lab

## 一、项目目标

从零开发一个轻量级流媒体实验平台，用于模拟音频流输入、视频流输出，以及浏览器实时播放。

未来会有独立的工作流负责将音频转换为视频，本项目只负责提供公共流媒体基础设施，不实现具体的音视频生成逻辑。

核心要求：

- **模块解耦**：各个模块可以独立运行，通过标准协议通信。
- **统一接口**：外部工作流可以方便地接收音频流、发布视频流。
- **本地验证**：无需 GPU，通过静态媒体文件模拟实时流。
- **容器化部署**：支持本地 Docker 和远程 Linux 服务器部署。
- 启用MUI的skill

请直接完成工程初始化、编码、测试和文档，不要只输出设计方案。

## 二、技术栈

- 前端：React + TypeScript + Vite + MUI
- 后端：Go
- 音频传输：WebSocket + PCM
- 视频处理：FFmpeg / FFprobe
- 流媒体网关：MediaMTX
- 视频播放：WebRTC / WHEP
- 部署：Docker Compose
- 存储：本地文件系统

优先使用成熟组件，避免重复开发现成的流媒体能力。

## 三、系统模块

### 1. Audio Control Console

一个用于管理音频素材和模拟实时音频流的 Web 控制台。

功能：

- 上传、查看、下载、删除 MP3/WAV 文件。
- 查看音频时长、采样率等基本信息。
- 选择音频文件作为当前输入。
- Start、Stop、Restart。
- 支持无限循环播放。
- 显示当前播放状态、进度、连接数。

后端通过 FFmpeg 解码音频，统一转换为：

```text
PCM S16LE
16000 Hz
Mono
20ms / frame
```

通过 WebSocket 按真实时间节奏持续发送音频帧，而不是一次性发送整个文件。

协议需要包含会话信息、序列号和媒体时间戳，允许多个客户端订阅。

注意处理停止、循环边界、客户端断开和慢消费者问题。

### 2. Mock Video Streamer

用于模拟一个持续输出视频流的外部工作流。

功能：

- 上传、查看、下载、删除 MP4 文件。
- 显示视频基本信息。
- 选择文件开始推流。
- Start、Stop、Restart、Loop。
- 显示推流状态和异常信息。

通过 FFmpeg 按真实时间速度读取视频，将包含音轨的视频发布到 MediaMTX。

优先采用 H.264 视频及兼容 WebRTC 的音频编码。

必须正确处理循环播放、时间戳和推流进程生命周期。

Mock Video Streamer 仅用于测试，可以独立启停，不应该成为其他模块运行的必要条件。

### 3. Media Gateway

使用 MediaMTX 作为独立流媒体网关。

职责：

- 接收外部视频流。
- 提供统一发布入口。
- 通过 WebRTC / WHEP 向浏览器分发音视频。
- 管理流连接和基本状态。

不自行实现 WebRTC Server。

发布协议优先采用 RTSP，实际播放使用 WHEP。

相关地址、端口和网络参数必须可配置，不要硬编码 localhost。

### 4. C-End Web Player

开发一个独立的、极简的浏览器播放器。

界面类似电视机：

- 全页面以 16:9 视频画面为主体。
- 无信号时显示 `NO SIGNAL`。
- 有信号时自动连接并播放。
- 同时播放视频和声音。
- 支持全屏、音量调整。
- 支持断线检测和自动重连。

使用 WebRTC 接收音视频，统一交给 HTMLVideoElement 播放。

不要用两个独立播放器分别播放音频和视频。

处理浏览器自动播放限制：如需用户交互才能启用声音，应展示简单的提示。

播放器不需要管理功能、调试面板或复杂控制界面。

## 四、整体架构

```text
Audio Control Console
        |
        v
Go Controller
        |
        | WebSocket / PCM
        v
External Consumer
  (future workflow)


Mock Video Streamer
        |
        | RTSP
        v
    MediaMTX  <------ External Video Publisher
        |
        | WebRTC / WHEP
        v
  C-End Web Player
```

模块间通过明确的网络协议交互。

音频控制台与视频 Mock 不需要直接连接。

播放器也不需要知道视频来源。

后续只需替换或关闭 Mock Video Streamer，即可接收外部工作流发布的视频流。

## 五、工程结构

建议使用 Monorepo：

```text
digital-human-streaming-lab/
├── apps/
│   ├── console/           # 媒体管理控制台
│   ├── player/            # C-End 播放器
│   └── controller/        # Go API + 音频流
├── services/
│   └── mediamtx/          # MediaMTX 配置
├── storage/
│   ├── audio/
│   └── video/
├── docs/
├── compose.yaml
├── compose.mock.yaml
├── .env.example
└── README.md
```

结构允许合理调整，但不要过度拆分。

Console 可以统一管理音频和 Mock 视频，Player 必须是独立应用。

## 六、Docker 部署

支持两种模式。

**基础模式：**

```bash
docker compose up -d --build
```

启动 Console、Controller、MediaMTX、Player。

**Mock 测试模式：**

```bash
docker compose -f compose.yaml -f compose.mock.yaml up -d --build
```

额外启用视频 Mock 功能。

要求：

- 所有服务使用 Docker 网络通信。
- 媒体素材通过 Volume 持久化。
- 环境变量统一管理地址和端口。
- 提供健康检查及日志。
- 能够在 Linux 环境直接运行。
- 注意 WebRTC 的 ICE、NAT 和 UDP 端口配置。
- 不要将上传的媒体文件提交到 Git。

## 七、开发与验证顺序

按下面顺序实施，每完成一阶段进行实际测试。

**Phase 1：视频播放闭环**

完成 MediaMTX、FFmpeg Mock 推流和 C-End Player。

允许使用 FFmpeg 生成自带音轨的测试视频。

验收：浏览器能通过 WebRTC 看到连续的视频并播放声音，断流后进入 NO SIGNAL，恢复推流后自动重连。

**Phase 2：音频控制台**

完成音频文件管理、PCM 实时分帧、WebSocket 推送、Start/Stop/Loop 等功能。

编写一个简单的 WebSocket 测试客户端。

验收：客户端能持续收到符合格式的 PCM 数据，且媒体时间戳和发送节奏正确。

**Phase 3：视频管理**

完成 MP4 上传、管理、选择、推流控制和 FFmpeg 进程管理。

验收：在 Console 选择 MP4 并点击 Start，独立 Player 能收到画面和声音。

**Phase 4：部署与文档**

完成 Docker Compose、配置、README、协议说明和基础测试。

验证容器重启、文件持久化和服务恢复。

## 八、工程规范

1. 优先保证功能真实可运行，不做无意义的架构抽象。
2. 所有控制台按钮必须调用真实 API。
3. 不要使用本地文件播放冒充视频流播放。
4. 不要用 iframe 代替独立的 WebRTC 播放器。
5. 文件管理需要基本的格式校验、大小限制和路径安全处理。
6. 正确处理 WebSocket 断开、FFmpeg 异常、WebRTC 重连及资源释放。
7. 固定关键依赖和 Docker 镜像版本。
8. 第一版无需数据库、认证系统、任务队列或复杂监控。
9. 在可信的本地环境开发；远程部署不要直接暴露无认证的管理接口。
10. 未经过实际验证的功能，不得声称测试通过。

## 九、最终交付

完成后提供：

- 完整可运行的工程代码。
- Docker Compose 配置。
- README：安装、启动、操作、部署步骤。
- 音频 WebSocket 协议说明。
- 视频发布地址和调用示例。
- 各模块的运行状态与测试结果。
- 已知问题及尚未验证的项目。

最终验收场景：

1. 启动 Docker Compose。
2. 打开 Console，上传 MP3/WAV 和 MP4。
3. 选择一个音频文件，启动模拟音频流，测试客户端可以持续接收 PCM。
4. 选择一个 MP4，启动视频 Mock 推流。
5. 打开独立 Player，能够播放实时视频和声音。
6. 停止推流后显示 NO SIGNAL，再次推流能够恢复。
7. 关闭 Mock 后，其余服务仍然正常运行。

**现在直接开始编码。优先完成最小端到端闭环，再逐步补齐控制界面、文件管理和部署能力。不要停留在方案设计阶段。**
