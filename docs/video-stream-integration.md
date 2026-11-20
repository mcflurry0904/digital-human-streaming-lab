# 视频流接入指南

算法工作流可以订阅 [音频 WebSocket](audio-websocket.md)，处理 PCM 并输出视频帧。播放器通过 WHEP 接收视频轨，声音由播放器单独订阅音频 WebSocket 获得。发布端可以接入本项目的 MediaMTX，也可以使用已提供 WHEP 的外部媒体服务。

## 发布到内置 MediaMTX

1. 在项目根目录运行 `docker compose up -d --build`，启动播放器、音频服务和 MediaMTX。
2. 将算法的输出编码为 H.264 视频，通过 RTSP over TCP 持续发布。主机上的工作流使用 `rtsp://127.0.0.1:8554/live`，同一 Compose 网络内的容器使用 `rtsp://mediamtx:8554/live`。端口和路径使用项目实际配置。每个路径只允许一个发布者，若 Mock 正在占用该路径，请先停止对应的 Mock 推流，或给工作流分配新路径。
3. 打开 `http://127.0.0.1:8081`。默认播放器通过同源的 `/gateway/live/whep` 读取画面。如需切换内置路径，在根目录 `.env` 中设置 `STREAM_PATH=agents/avatar`，保持 `WHEP_URL` 为空，将发布地址末尾改为 `/agents/avatar`，并运行 `docker compose up -d player` 应用配置。启用 Mock 的部署使用原来的 Compose 文件组合。Mock 也读取 `STREAM_PATH`。

用 MP4 测通发布链路。以下命令会按实时节奏循环文件，发送视频轨。

```sh
ffmpeg -re -stream_loop -1 -i sample.mp4 -map 0:V:0 -an \
  -c:v libx264 -profile:v baseline -tune zerolatency -pix_fmt yuv420p -r 30 -g 30 -bf 0 \
  -f rtsp -rtsp_transport tcp rtsp://127.0.0.1:8554/live
```

若算法输出原始 RGB 帧，可把 FFmpeg 作为长驻子进程，将每帧的字节写入其 stdin。以下示例假设每帧为连续排列的 RGB24，分辨率固定为 640×360，算法按每秒 30 帧送入数据。实际接入时将输入参数与算法输出对齐。一个发布会话复用同一个 FFmpeg 进程。

```sh
ffmpeg -f rawvideo -pixel_format rgb24 -video_size 640x360 -framerate 30 -i pipe:0 \
  -an -c:v libx264 -profile:v baseline -tune zerolatency -pix_fmt yuv420p -g 30 -bf 0 \
  -f rtsp -rtsp_transport tcp rtsp://127.0.0.1:8554/live
```

## 使用已有的 WHEP 服务

在根目录的 `.env` 中设置播放器实际要请求的 WHEP 端点，再执行 `docker compose up -d player`。首次启动或代码更新时添加 `--build`。

```dotenv
WHEP_URL=https://media.example.com/avatar/whep
```

- Compose 中，非空的 `WHEP_URL` 优先于 `STREAM_PATH`，通过运行时的 `/runtime.json` 提供给播放器。可填浏览器可达的 HTTP(S) 完整地址，或已有同源反向代理路由的根路径。应用配置后刷新播放器。使用外部 WHEP 时，播放器容器可以单独启动，声音仍需要音频服务。
- Vite 本地开发可在仓库根目录的 `.env.player.local` 设置 `VITE_WHEP_URL`，再运行 `npm run dev:player`。播放器优先读取有效的 `/runtime.json`，然后使用 `VITE_WHEP_URL`，默认地址为 `/gateway/live/whep`。使用 Vite 的 `/gateway` 代理时，通过 `DEV_WHEP_TARGET` 配置代理上游。
- WHEP 端点需接受含 `application/sdp` 的 POST 并返回 `201`、SDP answer 和用于清理会话的 `Location`。当前播放器获取 ICE 候选后发送 offer，结束会话时向 `Location` 发 DELETE。跨域时，服务需允许播放器页面的 Origin、POST/DELETE/OPTIONS 和 `Content-Type`，并暴露 `Location` 响应头。播放器的 WHEP 请求没有自定义鉴权头。HTTPS 播放器页面应使用 HTTPS 或同源地址。
- RTSP 地址供发布进程使用，WHEP 地址供浏览器协商。浏览器还需要能访问媒体服务提供的 ICE 候选地址。内置 MediaMTX 通过 `ICE_HOSTS` 和 `ICE_UDP_PORT` 配置该访问路径。

## 验收

1. 查看播放器的 `/runtime.json`，确认 `whep` 与预期一致。
2. 先用上面的 MP4 命令验证内置网关，再用算法输出替换文件输入。使用外部 WHEP 时，按对应媒体服务的发布方式发布到匹配的流。打开播放器并确认能看到持续变化的画面。
3. 停止发布，确认播放器显示 **NO SIGNAL**。重新发布同一路流，确认画面自动恢复。若只有信令成功但画面没出来，在浏览器网络面板确认 WHEP POST 返回 `201`，再检查 ICE 可达性和视频编码。
