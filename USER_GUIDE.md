# 直播系统使用文档

> 服务器地址：`jfznbx.cn` | 公网 IP：`47.100.227.26`
> 
> 最后更新：2026-06-09

---

## 目录

1. [系统架构](#1-系统架构)
2. [快速开始（5 分钟跑通）](#2-快速开始5-分钟跑通)
3. [推流端 — Raspberry Pi / FFmpeg](#3-推流端--raspberry-pi--ffmpeg)
4. [拉流端 — 观众播放](#4-拉流端--观众播放)
5. [管理 API 参考](#5-管理-api-参考)
6. [播放 URL 鉴权说明](#6-播放-url-鉴权说明)
7. [常见问题排查](#7-常见问题排查)

---

## 1. 系统架构

```
Raspberry Pi (推流)              Internet               服务器 (jfznbx.cn)
┌─────────────┐                                       ┌──────────────────────┐
│  FFmpeg      │ ──── RTMP :1935 ──────────────→      │ SRS 5.0              │
│  /dev/video0 │                                       │ ├ RTMP ingest :1935  │
└─────────────┘                                       │ ├ HTTP API   :1985   │
                                                       │ └ HLS/FLV    :8082   │
观众 (播放)                            HTTPS :443       │                       │
┌─────────────┐                                       │ Nginx → SRS :8082    │
│  浏览器      │ ←── https://jfznbx.cn/live/ ──────── │                       │
│  VLC/ffplay │                                       │ Go API Server :9090  │
└─────────────┘                                       │ PostgreSQL + Redis   │
                                                       └──────────────────────┘
```

| 端口 | 协议 | 用途 | 公网可达 |
|------|------|------|----------|
| 1935 | RTMP | 推流接入 | ✅ |
| 443 | HTTPS | HLS/FLV 播放（经 Nginx 代理） | ✅ |
| 9090 | HTTP | 管理 API（内部） | ❌ |

---

## 2. 快速开始（5 分钟跑通）

### 2.1 安装依赖

**推流端（Raspberry Pi）：**
```bash
sudo apt install -y ffmpeg
```

**拉流端（任意设备）：**
- 浏览器直接播放（推荐 Chrome/Edge）
- 或安装 `ffplay` / `VLC`

### 2.2 获取推流地址

推流地址由**管理后台**生成。向服务器 API 发起请求：

```bash
# 替换为实际的服务器地址（内网用 IP，公网用域名）
SERVER="jfznbx.cn:9090"

# 1. 管理员登录
ADMIN_TOKEN=$(curl -s -X POST "http://$SERVER/api/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}' \
  | jq -r '.data.token')

# 2. 创建直播流
STREAM=$(curl -s -X POST "http://$SERVER/api/streams" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{
    "protocol": "rtmp",
    "resolution": "1920x1080",
    "bitrate": "2000k"
  }')

# 3. 提取推流和播放地址
echo "$STREAM" | jq '{
  push_url: .data.push_url,
  hls_url:  .data.hls_url,
  flv_url:  .data.flv_url
}'
```

**返回示例：**
```json
{
  "push_url": "rtmp://jfznbx.cn:1935/live/550e8400-e29b-41d4-a716-446655440000?token=abc123...&expire=1781084864",
  "hls_url":  "https://jfznbx.cn/live/550e8400-e29b-41d4-a716-446655440000.m3u8?sign=def456...&expire=1781084864",
  "flv_url":  "https://jfznbx.cn/live/550e8400-e29b-41d4-a716-446655440000.flv?sign=def456...&expire=1781084864"
}
```

### 2.3 开始推流

```bash
ffmpeg -re -f lavfi -i testsrc=size=1920x1080:rate=30 \
  -f lavfi -i sine=frequency=1000 \
  -c:v libx264 -preset ultrafast -b:v 2000k \
  -c:a aac -b:a 128k \
  -f flv "rtmp://jfznbx.cn:1935/live/550e8400-...?token=abc123...&expire=1781084864"
```

### 2.4 播放测试

```bash
# 用 ffplay 播放 FLV（延迟更低）
ffplay "https://jfznbx.cn/live/550e8400-....flv?sign=def456...&expire=1781084864"

# 或用 VLC 播放 HLS
vlc "https://jfznbx.cn/live/550e8400-....m3u8?sign=def456...&expire=1781084864"
```

---

## 3. 推流端 — Raspberry Pi / FFmpeg

### 3.1 推流地址格式

```
rtmp://jfznbx.cn:1935/live/<stream_key>?token=<push_token>&expire=<unix_timestamp>
```

参数说明：
| 参数 | 来源 | 说明 |
|------|------|------|
| `stream_key` | API 创建流时返回 | 流的唯一标识（UUID v4） |
| `push_token` | API 创建流时返回 | HMAC-SHA256 推流凭证 |
| `expire` | API 创建流时返回 | Token 过期时间（Unix 秒），默认 24h |

> ⚠️ **必须携带 token 和 expire 参数**，否则服务器会拒绝推流（403）。

### 3.2 Raspberry Pi 推流命令

```bash
#!/bin/bash
# rpi_push.sh — Raspberry Pi 推流脚本

PUSH_URL="rtmp://jfznbx.cn:1935/live/STREAM_KEY?token=TOKEN&expire=EXPIRE"

# USB 摄像头
ffmpeg -re \
  -f v4l2 -input_format mjpeg -video_size 1280x720 -framerate 30 -i /dev/video0 \
  -f alsa -ac 1 -ar 44100 -i hw:1,0 \
  -c:v libx264 -preset veryfast -tune zerolatency \
  -b:v 2000k -maxrate 2500k -bufsize 4000k \
  -g 60 -keyint_min 30 \
  -c:a aac -b:a 128k -ar 44100 \
  -f flv "$PUSH_URL"
```

**Raspberry Pi 硬件加速版（推荐）：**
```bash
# 使用 Raspberry Pi 的 H.264 硬件编码器
ffmpeg -re \
  -f v4l2 -input_format mjpeg -video_size 1280x720 -framerate 30 -i /dev/video0 \
  -f alsa -ac 1 -ar 44100 -i hw:1,0 \
  -c:v h264_v4l2m2m -b:v 2000k \
  -c:a aac -b:a 128k \
  -f flv "$PUSH_URL"
```

### 3.3 常用分辨率和码率

| 分辨率 | 码率 | 适用场景 |
|--------|------|----------|
| 640x360 | 500k | 低带宽 / 移动网络 |
| 854x480 | 800k | 标清 |
| 1280x720 | 1500k–2500k | 高清（推荐） |
| 1920x1080 | 3000k–5000k | 全高清 |

### 3.4 Token 过期处理

Token 有效期 24 小时。过期后需要刷新：

```bash
# 刷新推流 token
curl -s -X POST "http://jfznbx.cn:9090/api/streams/<stream_id>/refresh-token" \
  -H "Authorization: Bearer $ADMIN_TOKEN" | jq '.data.push_url'
```

### 3.5 推流状态检查

```bash
# 查看流是否正在推流
curl -s "http://jfznbx.cn:9090/api/streams/<stream_id>" \
  -H "Authorization: Bearer $ADMIN_TOKEN" | jq '{status: .data.status, started_at: .data.started_at}'
```

状态说明：
| 状态 | 含义 |
|------|------|
| `created` | 已创建，等待推流 |
| `publishing` | 正在推流中 |
| `ended` | 推流已结束 |
| `error` | 异常 |

---

## 4. 拉流端 — 观众播放

### 4.1 播放地址格式

```
# FLV（低延迟 1-3 秒，推荐）
https://jfznbx.cn/live/<stream_key>.flv?sign=<signature>&expire=<unix_timestamp>

# HLS（兼容性好 3-6 秒延迟）
https://jfznbx.cn/live/<stream_key>.m3u8?sign=<signature>&expire=<unix_timestamp>
```

### 4.2 网页播放（推荐）

#### FLV 播放（flv.js — 延迟更低）

```html
<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"><title>直播播放</title></head>
<body>
  <video id="video" controls autoplay muted style="width:100%;max-width:800px"></video>

  <script src="https://cdn.jsdelivr.net/npm/flv.js@1.6.2/dist/flv.min.js"></script>
  <script>
    if (flvjs.isSupported()) {
      var video = document.getElementById('video');
      var flvPlayer = flvjs.createPlayer({
        type: 'flv',
        url: 'https://jfznbx.cn/live/<stream_key>.flv?sign=<signature>&expire=<expire>',
        isLive: true
      });
      flvPlayer.attachMediaElement(video);
      flvPlayer.load();
      // 点击播放（部分浏览器需要用户交互）
      video.addEventListener('click', function() { flvPlayer.play(); });
    }
  </script>
</body>
</html>
```

#### HLS 播放（hls.js — 兼容性更好）

```html
<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"><title>HLS 直播</title></head>
<body>
  <video id="video" controls autoplay muted style="width:100%;max-width:800px"></video>

  <script src="https://cdn.jsdelivr.net/npm/hls.js@1.5.17/dist/hls.min.js"></script>
  <script>
    var video = document.getElementById('video');
    if (Hls.isSupported()) {
      var hls = new Hls({ liveSyncDurationCount: 3 });
      hls.loadSource('https://jfznbx.cn/live/<stream_key>.m3u8?sign=<signature>&expire=<expire>');
      hls.attachMedia(video);
    } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
      // Safari 原生支持 HLS
      video.src = 'https://jfznbx.cn/live/<stream_key>.m3u8?sign=<signature>&expire=<expire>';
    }
  </script>
</body>
</html>
```

### 4.3 桌面播放器

```bash
# ffplay (FFmpeg 自带)
ffplay "https://jfznbx.cn/live/<stream_key>.flv?sign=<signature>&expire=<expire>"

# VLC
vlc "https://jfznbx.cn/live/<stream_key>.m3u8?sign=<signature>&expire=<expire>"

# MPV
mpv "https://jfznbx.cn/live/<stream_key>.flv?sign=<signature>&expire=<expire>"
```

### 4.4 移动端播放

**iOS Safari — 直接使用 HLS：**
```html
<!-- iOS Safari 原生支持 HLS，直接设置 src 即可 -->
<video src="https://jfznbx.cn/live/<stream_key>.m3u8?sign=..." controls autoplay></video>
```

**Android — 使用 flv.js 或 hls.js（同上网页方案）。**

### 4.5 获取播放地址（无需管理员权限）

观众不需要登录，可通过公开 API 获取播放地址：

```bash
# GET /api/playback/:stream_key （无需认证）
curl -s "http://jfznbx.cn:9090/api/playback/<stream_key>" | jq '{
  status: .data.status,
  hls_url: .data.hls_url,
  flv_url: .data.flv_url
}'
```

返回示例：
```json
{
  "status": "publishing",
  "hls_url": "https://jfznbx.cn/live/xxx.m3u8?sign=...&expire=...",
  "flv_url": "https://jfznbx.cn/live/xxx.flv?sign=...&expire=..."
}
```

---

## 5. 管理 API 参考

### 5.1 管理员认证

```bash
POST /api/auth/login
Content-Type: application/json

{
  "username": "admin",
  "password": "admin123"
}
```

返回 JWT token，有效期 8 小时。后续管理接口需在 Header 中携带：
```
Authorization: Bearer <jwt_token>
```

### 5.2 完整接口列表

| 方法 | 路径 | 认证 | 说明 |
|------|------|------|------|
| GET | `/api/health` | 无 | 健康检查 |
| POST | `/api/auth/login` | 无 | 管理员登录 |
| POST | `/api/streams` | JWT | 创建直播流 |
| GET | `/api/streams` | JWT | 列出所有流（支持 `?status=` 过滤） |
| GET | `/api/streams/:id` | JWT | 获取流详情 |
| POST | `/api/streams/:id/start` | JWT | 创建推流任务 |
| POST | `/api/streams/:id/stop` | JWT | 停止推流 |
| DELETE | `/api/streams/:id` | JWT | 删除流 |
| POST | `/api/streams/:id/refresh-token` | JWT | 刷新推流 token |
| GET | `/api/playback/:stream_key` | **无** | 获取播放地址（公开） |

### 5.3 创建流（完整参数）

```bash
curl -X POST "http://jfznbx.cn:9090/api/streams" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{
    "protocol": "rtmp",
    "resolution": "1920x1080",
    "bitrate": "2000k",
    "channel_id": "可选的频道 UUID"
  }'
```

### 5.4 停止 / 删除流

```bash
# 停止推流（通知 SRS 踢掉推流端）
curl -X POST "http://jfznbx.cn:9090/api/streams/<stream_id>/stop" \
  -H "Authorization: Bearer $ADMIN_TOKEN"

# 删除流记录
curl -X DELETE "http://jfznbx.cn:9090/api/streams/<stream_id>" \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

---

## 6. 播放 URL 鉴权说明

### 6.1 鉴权机制

播放 URL 中包含 **HMAC-SHA256 签名**，有效期 24 小时：

```
https://jfznbx.cn/live/<stream_key>.flv?sign=<hmac_signature>&expire=<unix_timestamp>
```

- `sign` — HMAC-SHA256(`stream_key` + `expire`, `play_secret`)
- `expire` — Unix 时间戳（秒），过期后 URL 失效

### 6.2 自行生成播放签名

如果需要在非 Web 环境生成播放 URL：

```go
// Go 示例
import "crypto/hmac"
import "crypto/sha256"
import "encoding/hex"
import "fmt"

func GeneratePlaySign(streamKey string, expire int64, secret string) string {
    message := fmt.Sprintf("%s%d", streamKey, expire)
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write([]byte(message))
    return hex.EncodeToString(mac.Sum(nil))
}
```

```python
# Python 示例
import hmac
import hashlib

def generate_play_sign(stream_key: str, expire: int, secret: str) -> str:
    message = f"{stream_key}{expire}"
    return hmac.new(secret.encode(), message.encode(), hashlib.sha256).hexdigest()
```

---

## 7. 常见问题排查

### 7.1 推流失败

**现象：FFmpeg 报 `Input/output error`**

可能原因：
1. **Token 错误或过期** — 检查 push_url 中的 token 是否正确，过期则刷新
2. **1935 端口不通** — 测试：`telnet jfznbx.cn 1935`
3. **流已结束** — 状态为 `ended` 的流无法再次推流，需创建新流

**诊断命令：**
```bash
# 测试端口连通性
timeout 3 bash -c "echo > /dev/tcp/jfznbx.cn/1935" && echo "端口可达" || echo "端口不通"

# 不带 token 测试（如果流允许无 token 推流）
ffmpeg -re -f lavfi -i testsrc=size=320x240:rate=10 \
  -c:v libx264 -preset ultrafast -f flv \
  "rtmp://jfznbx.cn:1935/live/test_key"
```

### 7.2 播放黑屏 / 无画面

1. **签名过期** — 重新获取播放 URL
2. **流未在推** — 确认流状态为 `publishing`
3. **HLS 需要等待 3-6 秒** — HLS 有首片延迟，用 FLV 替代可降低延迟
4. **浏览器限制** — HTTPS 页面不能加载 HTTP 资源，确保 URL 以 `https://` 开头

### 7.3 延迟太大

- FLV 延迟约 1-3 秒，HLS 约 3-6 秒
- 在 FFmpeg 推流端添加 `-tune zerolatency` 减少编码延迟
- Raspberry Pi 端建议使用硬件编码 `h264_v4l2m2m`

### 7.4 查看服务器端日志

```bash
# SSH 到服务器后执行：

# SRS 流媒体服务器日志
tail -f /usr/local/srs/objs/srs.log

# Go 管理服务日志
journalctl -u go-live -f

# 查看当前在线的推流客户端
curl -s http://127.0.0.1:1985/api/v1/clients/ | jq '.data.clients[] | {name, type, publish, alive}'
```

### 7.5 获取帮助

服务器信息速查：
```
管理 API：  http://jfznbx.cn:9090  （需内网或 SSH 隧道）
推流入口：  rtmp://jfznbx.cn:1935/live
播放入口：  https://jfznbx.cn/live/
管理员账号：admin / admin123
```

---

> 📝 **版本**：v1.0 | **服务器**：阿里云 ECS 上海 (i-uf65sudisstvkw1y3wip) | **SRS**：5.0.224
