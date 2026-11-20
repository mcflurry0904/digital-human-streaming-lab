# Digital Human Streaming Lab

用本地素材模拟实时音频输入和视频输出。独立 Go 音频服务向外部工作流广播 PCM，外部工作流或可选的 Mock 服务通过 RTSP 发布视频，播放器经 MediaMTX WHEP 接收画面，并通过独立的音频 WebSocket 播放模拟音频。

## AutoDL 原生启动（无 Docker）

在 AutoDL 实例的 Linux Shell 中操作。每个服务命令都从仓库根目录运行。首次联调建议启动全部五个进程，确认 Mock 能推流后再接入算法。下面使用回环地址和默认流路径 `live`，无需创建 `.env`。Compose 的变量和容器主机名仅在 Compose 部署中生效。

### 1. 准备依赖和素材目录

检查 PATH 中的 Go（1.23+）、Node.js（22+）、npm、FFmpeg 和 FFprobe。若 Ubuntu/Debian 镜像缺少 FFmpeg，可用具备权限的账号执行 `apt-get update && apt-get install -y ffmpeg`。Mock 需要 FFmpeg 提供 `libx264`。按实例架构从 [MediaMTX v1.12.3 发布页](https://github.com/bluenviron/mediamtx/releases/tag/v1.12.3) 获取 Linux 原生二进制，与项目容器使用的版本一致。下面的下载命令适用于 `uname -m` 为 `x86_64` 的实例。

```sh
go version
node --version
npm --version
ffmpeg -version
ffprobe -version
ffmpeg -hide_banner -encoders | grep libx264

mkdir -p .check/bin storage/audio storage/video
curl -fL -o .check/mediamtx.tar.gz \
  https://github.com/bluenviron/mediamtx/releases/download/v1.12.3/mediamtx_v1.12.3_linux_amd64.tar.gz
tar -xzf .check/mediamtx.tar.gz -C .check/bin mediamtx
.check/bin/mediamtx --version

npm ci
(cd apps/controller && go build -o ../../.check/bin/streaminglab .)
```

### 2. 启动五个进程

按顺序在不同终端或 `tmux` 窗口中启动，每条命令保持前台运行以便查看日志。需要断开 SSH 时，可让进程继续运行在 `tmux` 会话里。

**MediaMTX**（RTSP 8554、WHEP HTTP 8889、媒体 ICE TCP 8189、管理 API 9997）

```sh
MTX_RTSPADDRESS=127.0.0.1:8554 MTX_WEBRTCADDRESS=127.0.0.1:8889 \
MTX_API=yes MTX_APIADDRESS=127.0.0.1:9997 \
MTX_WEBRTCLOCALUDPADDRESS='' MTX_WEBRTCLOCALTCPADDRESS=127.0.0.1:8189 \
MTX_WEBRTCIPSFROMINTERFACES=no MTX_WEBRTCADDITIONALHOSTS=127.0.0.1 \
  .check/bin/mediamtx services/mediamtx/mediamtx.yml
```

**音频 Controller**（8080）

```sh
LAB_KIND=audio HTTP_ADDR=127.0.0.1:8080 MEDIA_DIR="$PWD/storage/audio" \
  .check/bin/streaminglab
```

**视频 Mock**（8082，可选）

```sh
LAB_KIND=video HTTP_ADDR=127.0.0.1:8082 MEDIA_DIR="$PWD/storage/video" \
RTSP_URL=rtsp://127.0.0.1:8554/live \
  .check/bin/streaminglab
```

**Console**（5173）

```sh
DEV_AUDIO_TARGET=http://127.0.0.1:8080 DEV_VIDEO_TARGET=http://127.0.0.1:8082 \
  npm run dev:console -- --host 127.0.0.1 --strictPort
```

如果跳过 Mock，在独立 Shell 中省略 `DEV_VIDEO_TARGET`，并确认根目录的 Vite 环境文件里也没有设置它。使用算法推流时发布到 `rtsp://127.0.0.1:8554/live`。同一路径只能有一个发布者，切换到算法前先停止 Mock 推流。

**Player**（5174）

```sh
DEV_AUDIO_TARGET=http://127.0.0.1:8080 DEV_WHEP_TARGET=http://127.0.0.1:8889 \
VITE_WHEP_URL=/gateway/live/whep \
  npm run dev:player -- --host 127.0.0.1 --strictPort
```

### 3. 验证实例内服务

```sh
curl -fsS http://127.0.0.1:9997/v3/paths/list
curl -fsS http://127.0.0.1:5173/api/audio/healthz
curl -fsS http://127.0.0.1:5174/api/audio/healthz
# 已启动 Mock 时执行
curl -fsS http://127.0.0.1:5173/api/video/healthz
```

音频和 Mock 的健康检查应返回 `{"status":"ok"}`。MediaMTX 的路径列表在推流前可以为空。按下方[验收操作](#验收操作)生成测试素材，使用本节端口完成前 3 项，音频客户端改用 `node tools/audio-client.mjs ws://127.0.0.1:5173/api/audio/ws`。Mock 开始推流后，重新查路径列表，确认 `live` 的 `ready` 为 `true`，再验证浏览器实际出画、出声。

### 4. 从本机浏览器访问

在自己的电脑上开终端，将示例中的 SSH 端口和登录地址替换为 AutoDL 控制台提供的值，保持隧道运行。8189 在本机和实例两端保持同一个端口，因为 MediaMTX 会在 SDP 中通告 `127.0.0.1:8189`。

```sh
ssh -N -o ExitOnForwardFailure=yes -p SSH_PORT \
  -L 127.0.0.1:5173:127.0.0.1:5173 \
  -L 127.0.0.1:5174:127.0.0.1:5174 \
  -L 127.0.0.1:8189:127.0.0.1:8189 \
  root@SSH_HOST
```

在本机打开 Console（http://127.0.0.1:5173）和 Player（http://127.0.0.1:5174）。Vite 转发音频 WebSocket 和 WHEP 信令，WebRTC 画面走 8189 的 ICE TCP 隧道。如果页面打开但没有画面，先查 `live` 是否已就绪，再检查浏览器中 `POST /gateway/live/whep` 是否返回 `201`，以及 8189 是否成功转发。AutoDL 自定义服务提供 HTTP/TCP 映射，仅转发网页端口时仍需单独打通浏览器到 ICE 候选地址的媒体连接。参见 [AutoDL SSH 隧道](https://api.autodl.com/docs/ssh_proxy/)和[开放端口](https://www.autodl.com/docs/port/)。

停止时在各进程终端按 `Ctrl+C`。素材保存在 `storage/audio` 和 `storage/video`，需要跨重建保留时，将 `MEDIA_DIR` 指向实例的持久化目录。重新启动进程后，在 Console 重新选择素材并点 **Start**。

## 使用 Docker 安装和启动

需要 Docker Engine 或 Docker Desktop、Docker Compose v2，以及构建时访问容器镜像、Alpine、Go 和 npm 软件源的网络。使用 FFmpeg 生成测试素材时需要本地 FFmpeg。运行音频测试客户端需要 Node.js 22 或更新版本。

```sh
cp .env.example .env
docker compose up -d --build
```

启用视频 Mock 时使用以下命令。

```sh
docker compose -f compose.yaml -f compose.mock.yaml up -d --build
docker compose -f compose.yaml -f compose.mock.yaml ps
```

| 入口 | 默认地址 |
| --- | --- |
| Console | http://127.0.0.1:8080 |
| 独立 Player | http://127.0.0.1:8081 |
| 音频 WebSocket | `ws://127.0.0.1:8080/api/audio/ws` |
| RTSP 发布 | `rtsp://127.0.0.1:8554/live` |
| WHEP | `http://127.0.0.1:8081/gateway/live/whep` |

两个 Compose 模式共享同名项目和音频数据卷。素材写入 `audio`、`video` Docker 命名卷。容器重建时卷保留。运行状态保存在进程内，服务重启后重新选择素材并 Start。保留素材时使用普通 `down`。带 `-v` 的 `down` 会删除数据卷。

## 验收操作

生成可辨识的测试图案和音调。

```sh
ffmpeg -f lavfi -i "testsrc2=size=640x360:rate=30" -f lavfi -i "sine=frequency=660:sample_rate=48000" -t 8 -c:v libx264 -pix_fmt yuv420p -c:a aac sample.mp4
ffmpeg -i sample.mp4 -vn -c:a pcm_s16le sample.wav
ffmpeg -i sample.mp4 -vn -c:a libmp3lame sample.mp3
```

1. 在 Console 的 **Audio control** 上传 WAV 或 MP3，选择素材并开启 Loop。运行 `node tools/audio-client.mjs`，再点 **Start**。观察两端状态和帧检查。可以同时运行多个客户端。用 **Stop** 停止，用 **Restart** 建立新会话。
2. 在 **Mock video** 上传 MP4，选择素材，开启 Loop 并点 **Start**。打开独立 Player 查看画面，点 **Enable sound** 听 Audio control 选中的音频。音频和画面可分别启动或停止。
3. 在 Mock 页点 **Stop**，Player 应显示 **NO SIGNAL**。再次 **Start**，原页面应自动恢复画面。播放器打开后自动订阅音频，并显示连接状态。首次输出声音需点击 **Enable sound**。若浏览器暂停了音频设备，可点击 **Resume sound**。
4. 运行 `docker compose -f compose.yaml -f compose.mock.yaml stop mock`，确认音频服务和 Player 正常运行。运行同样的 Compose 模式重新启动 Mock，再次确认素材仍在。
5. 用 `docker compose -f compose.yaml -f compose.mock.yaml restart controller mock mediamtx` 测试恢复。确认服务重回健康、素材仍在、音频和视频可以重新 Start。查看 `docker compose -f compose.yaml -f compose.mock.yaml logs --tail=100` 获取异常上下文。

每个操作卡的 **Loop** 设置在下一次 Start 或 Restart 时应用。运行中的素材需先停止才能删除。文件上传默认上限为 512 MiB，MP4 需包含画面，可以没有音轨。Mock 只发布视频轨，播放器声音来自 Audio control 的 PCM。

## 外部工作流接入

- 音频帧布局、连接方式及客户端测试见 [音频 WebSocket 协议](docs/audio-websocket.md)。
- 算法输出的编码、RTSP 推流、外部 WHEP 地址配置和播放验收见 [视频流接入指南](docs/video-stream-integration.md)。

### HTTP API

与控制台同源使用 `/api/audio` 或 `/api/video` 前缀。服务在 Docker 网络内提供去掉前缀的同一接口，例如 `http://controller:8080/status`。

| 请求 | 用途 |
| --- | --- |
| `GET /media` | 素材列表与 FFprobe 元数据 |
| `POST /media?name=sample.wav` | 请求体为文件原始字节，文件名需 URL 编码 |
| `GET /media/{id}` | 下载素材 |
| `DELETE /media/{id}` | 删除素材 |
| `GET /status` | 进程状态、当前素材、循环设置、进度及音频订阅人数 |
| `POST /control/start`、`POST /control/restart` | JSON 请求体 `{"mediaId":"文件 ID","loop":true}` |
| `POST /control/stop` | 停止当前流 |
| `GET /healthz` | 检查对应服务的 HTTP 进程是否可用 |

```sh
curl --fail-with-body --data-binary @sample.wav 'http://127.0.0.1:8080/api/audio/media?name=sample.wav'
curl -fsS 'http://127.0.0.1:8080/api/audio/status'
```

## 部署到 Linux

AutoDL 等无法运行 Docker 的环境使用上面的 [AutoDL 原生启动](#autodl-原生启动无-docker)。

在服务器复制项目与 `.env`，使用上面的 Compose 命令。保持 Console 和 RTSP 绑定在回环地址，通过 SSH 隧道、VPN 或受访问控制的反向代理访问管理与发布入口。服务端播放器可以经 HTTPS 反向代理公开。浏览器媒体面还需要能直接访问 `ICE_UDP_PORT`。

- `ICE_HOSTS` 填浏览器可达的服务器 IP 或主机名，多个值逗号分隔。
- 网关和主机使用同一个 `ICE_UDP_PORT`。防火墙及 NAT 要放行和转发该 UDP 端口，仅代理 WHEP 的 HTTP 请求无法传输媒体。
- `CONSOLE_PORT`、`PLAYER_PORT`、`RTSP_PORT` 控制主机端口。除非网络已有访问控制，保留 `CONSOLE_BIND` 和 `RTSP_BIND` 的回环默认值。
- 本版播放器使用完整 SDP offer，无 TURN 配置界面。只允许经 TURN 出网的客户端需要扩展 ICE 配置。
- Compose 通过 HTTP 探测管理进程及 MediaMTX 本地 API。实际媒体传输可按验收操作另外检查。

## 本地开发和测试

安装 Go 1.23+、FFmpeg、FFprobe 和 Node.js 22+。

```sh
npm ci
npm run build
(cd apps/controller && go test ./...)
```

从仓库根运行 `npm run dev:console` 与 `npm run dev:player`，分别使用 5173 和 5174 端口。Vite 代理通过 `DEV_AUDIO_TARGET`、`DEV_VIDEO_TARGET`、`DEV_WHEP_TARGET` 指向独立运行的服务。原生服务监听地址、卷目录和 RTSP 目标可通过 `HTTP_ADDR`、`MEDIA_DIR` 和 `RTSP_URL` 设置。Mock 复用同一个 Go 二进制，在单独进程中设置 `LAB_KIND=video`。在 Compose 外运行 MediaMTX 时需要自行配置浏览器可达的 WebRTC 地址，并为 Vite 提供 WHEP 地址。

Go 集成测试覆盖上传、格式与路径校验、下载、双客户端 PCM、跨循环序列和 PTS、节奏、Restart、Stop 和删除。验证结果见 [运行与验证记录](docs/verification.md)。
