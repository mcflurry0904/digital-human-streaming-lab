# 运行与验证记录

在 Windows 主机、Docker Desktop Linux Engine 和 Chromium 无头浏览器上执行。Go 1.24.4 用于原生集成测试，容器内运行 FFmpeg 6.1.2 和 MediaMTX 1.12.3。

| 范围 | 实测结果 |
| --- | --- |
| 前端 | `npm run build` 通过，包含完整 TypeScript 类型检查与 Console、Player 两套 Vite 生产构建。浏览器访问两个容器的首页均成功。 |
| 音频 Controller | `go vet ./...` 与 `go test -count=3 ./...` 通过。覆盖 WAV 上传、校验和下载、两客户端实时订阅、跨循环的会话序列和 PTS、Restart、Stop、删除。浏览器上传后通过实际 Compose WebSocket 收到 30 帧，平均间隔约 20.1 ms。另通过实际 API 上传、下载并校验 MP3 字节一致，循环解码后收到 30 个 PCM 帧，平均间隔 20.05 ms。 |
| Mock 与 Media Gateway | 浏览器上传 MP4、选中后 Start。MediaMTX 日志确认通过 RTSP 接收 H.264 和 Opus，再通过 WebRTC 分发两个轨道。另用 FFmpeg 以 RTSP/TCP 接收约 5 秒，跨两个循环边界收到 150 个 H.264 包和 248 个 Opus 包，两轨 PTS 严格递增。 |
| 独立 Player | Chromium 显示实时画面。开启声音后，从远端音轨检测到测试音调，音量和全屏控制有效。停止源后显示 `NO SIGNAL`，重新推流后原页面自动恢复且保留声音设置。验证断流后的 WHEP DELETE 请求经代理到达网关，网关已回收该会话。模拟测试另覆盖无源重试和会话清理。 |
| Compose | 基础与 Mock 两种配置均通过 `docker compose config`。Mock 模式五个容器构建并达到 healthy。重启 Controller、Mock 和 MediaMTX 后，原素材可下载，服务恢复健康。停止 Mock 后 Console 音频 API 和 Player 均可访问。切换到基础模式后，由主机 FFmpeg 直接通过 RTSP 发布，Chromium 收到视频和音轨。重新启用 Mock 时原视频卷仍可用。 |

可独立复跑以下检查。

```sh
npm ci
npm run build
node tools/check-whep.mjs
(cd apps/controller && go test ./...)
```

实际部署操作见 [README](../README.md#验收操作)。本机浏览器验证使用回环地址和 UDP 8189。独立远程 Linux 主机上的防火墙和 NAT 尚未验证。播放器依赖支持 WebRTC 和 `requestVideoFrameCallback` 的浏览器，当前实测为 Chromium。当前 Windows Go 工具链缺少 C 编译器，未运行 `go test -race`。

## 独立音画播放

更新后在 Docker Compose 和 Chromium 上复测。Mock 发布及 WHEP 分发均只有 H.264 视频轨。播放器在 NO SIGNAL 时仍可通过 WebSocket 播放上传的 WAV。停止音频后画面继续推进，再次启动音频可恢复声音。无音轨的 MP4 也通过上传校验。上表的双轨结果记录的是修改前的验证。

订阅状态复测：打开播放器时 Controller 计数从 0 变为 1，开启声音后持续订阅 32 秒并调度 930 个非静音 PCM 帧，关闭页面后计数回到 0。
