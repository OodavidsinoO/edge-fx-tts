# edge-fx-tts

[![Go](https://img.shields.io/github/go-mod/go-version/OodavidsinoO/edge-fx-tts)](go.mod)
[![Release](https://img.shields.io/github/v/release/OodavidsinoO/edge-fx-tts)](https://github.com/OodavidsinoO/edge-fx-tts/releases)
[![CI](https://github.com/OodavidsinoO/edge-fx-tts/actions/workflows/ci.yml/badge.svg)](https://github.com/OodavidsinoO/edge-fx-tts/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/OodavidsinoO/edge-fx-tts)](LICENSE)

[English](README.md) | 简体中文

## 这是什么

edge-fx-tts 是一个构建在 Microsoft Edge TTS 之上的 Go 后处理音效引擎。它先用 Edge
TTS 合成语音（返回 **24 kHz、MPEG-2 Layer 3、单声道** 流），解码后送入可配置的效果链，
最终输出 **16-bit PCM WAV**——或经可选的 ffmpeg 编码为 MP3。

内置 CLI（`cmd/edgefx`）端到端暴露整个引擎：

- `--profile none`（默认）**原样直通**合成出的 MP3 流——即原始 Edge TTS 能力本身，
  不经过任何处理，也不需要 ffmpeg。
- 任何其他 profile 都会解码该流并运行预设效果链：
  upmix 立体声 → EQ/动态 → 时基效果 → limiter → WAV。

## 特性

- **MP3 直通** — `--profile none` 逐字节拷贝 Edge TTS 流；纯文本→MP3 流程只需要 CLI 本身。
- **13 效果链** — `upmix`、`eq`、`compressor`、`deesser`、`chorus`、`delay`、
  `fdnreverb`、`limiter`、`formant`（自研倒谱源-滤波器共振峰搬移）、`pitchcorrector`、
  `flanger`、`gate`、`wsola`（保时长的频谱变调）。
- **11 个内置预设 profile** — filmai ×2（D3–D4 电影 AI 感）、broadcast ×3
  （播音员/电台/电话会议）、aifake / doubledelay / scifiatmo（A/B/C 演示预设）、
  jarvis / edith / ai-modern（现代电影 AI 声），外加一个最小 `placeholder` 占位。
- **流式管线** — 解码 → 效果链 → WAV 三协程流水线，有界 SPSC 环形缓冲 + 天然背压，
  并带尾部尾音，避免混响/延迟衰减被硬切。
- **完整 CLI 表面** — `--profile`、`--type`、`--text`、`--file`、`--output`、
  `--voice`、`--rate`、`--pitch`、`--volume`、`--format`。
- **库 API** — 轻量 `tts.Synthesizer` 接口使引擎与具体合成后端解耦；profile、链构建和
  管线均为独立 `pkg/` 包。

## 安装

安装 CLI：

```bash
go install github.com/OodavidsinoO/edge-fx-tts/cmd/edgefx@latest
```

或作为库使用：

```bash
go get github.com/OodavidsinoO/edge-fx-tts
```

合成需要访问 Edge TTS 网络服务，因此需要联网。

## CLI 用法

```bash
edgefx -h
```

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `--profile` | `none` | `none` = 原样 MP3 直通，或内置预设名 |
| `--type` | `text` | 输入类型：`text` 或 `ssml`。首位置参数 `ssml` 的旧写法仍兼容（显式 `--type` 优先） |
| `--text` | `hello world` | 文本（`--type ssml` 时为 SSML）输入 |
| `--file` | | 从文件读取全部输入（文本或 SSML）；与 `--text` 互斥 |
| `--output` | *（必填）* | 输出音频文件路径 |
| `--voice` | | 声音短名，如 `en-US-GuyNeural`、`zh-CN-XiaoxiaoNeural` |
| `--rate` | | 语速，如 `+10%` |
| `--pitch` | | 音高，如 `+5Hz` |
| `--volume` | | 音量，如 `+10%` |
| `--format` | `auto` | 输出格式：`mp3` 或 `wav`。`auto` 按输出扩展名推断——预设链默认 WAV、直通保持 MP3 |

输出格式规则：

- `--profile none` 始终原样写出原始 MP3 流——`--format` 无关，绝不调用 ffmpeg。
- 预设链默认输出 WAV；只有传 `--format mp3` **或**输出路径为 `.mp3` 时才例外。此时
  管线先渲染到临时 WAV，再用 **ffmpeg** 编码（`ffmpeg -y -i <tmp.wav> -b:a 192k <out.mp3>`）。
- 未安装 ffmpeg 时输出 MP3 会报清晰错误：
  `ffmpeg required for MP3 output; install ffmpeg or use .wav`。

### 示例

纯文本转 MP3——无 profile、无 ffmpeg，流原样直通：

```bash
edgefx --text "Hello, world." --voice en-US-GuyNeural --output hello.mp3
```

SSML 输入（也接受位置参数 `ssml` 的写法）：

```bash
edgefx --type ssml --text '<speak version="1.0" xmlns="http://www.w3.org/2001/10/synthesis" xml:lang="en-US"><voice name="en-US-GuyNeural"><prosody rate="+10%">hello world</prosody></voice></speak>' --output hello.mp3
```

效果预设转 WAV：

```bash
edgefx --profile jarvis --text "Hello from the machine." --output out.wav
```

效果预设转 MP3（需要 ffmpeg）：

```bash
edgefx --profile ai-modern --text "Hello again." --format mp3 --output out.mp3
```

从文件读取输入：

```bash
edgefx --file script.txt --profile broadcast-e2 --output out.wav
```

在预设之上叠加音色与韵律参数：

```bash
edgefx --profile broadcast --voice zh-CN-YunxiNeural --rate +10% --pitch +5Hz --text "你好，世界" --output out.wav
```

## 内置预设 profile

所有预设均以 24 kHz 立体声运行，并以峰值 limiter 收尾。下表中参数为随附默认值。

| Profile | 风格 | 关键链路 |
| --- | --- | --- |
| `aifake` | 合成 AI 感，可懂度优先（报告 §3.2 A） | HPF 100 Hz；压缩 2:1 / −20 dB；带通 300–3400 Hz；合唱 22 ms ×3；FDN RT60 1.0 s wet 0.15 |
| `doubledelay` | 电影双音 + slapback（报告 §3.2 B） | HPF 80 Hz；压缩 3:1 / −18 dB；合唱 25 ms ×2 wet 0.35；slapback 100 ms、零反馈；LPF 7 kHz + 宽带 de-esser（7 kHz / −28 dB）；FDN RT60 1.6 s |
| `scifiatmo` | 科幻氛围（报告 §3.2 C） | HPF 80 Hz；压缩 4:1 / −16 dB；宽合唱 30 ms ×3；环境延迟 250 ms、FB 0.25；LPF 7 kHz + 宽带 de-esser（7 kHz / −28 dB）；FDN RT60 3.0 s damp 0.38 |
| `filmai-d3` | D3 HAL/TARS 冷静服务器嗓 | WSOLA −2.5 st；噪声门 −45 dB 10:1；LPF 7 kHz；压缩 4:1 快起音；FDN RT60 0.25 s wet 0.1；宽带 de-esser（7 kHz / −28 dB）+ LPF 7 kHz |
| `filmai-d4` | D4 微距 OS，近讲微调 | 100 Hz +1.5 dB；3 kHz +2.5 dB；LPF 7.5 kHz；de-esser（6.5 kHz / −28 dB）；轻压缩 1.5:1；FDN RT60 0.2 s wet 0.15 |
| `jarvis` | 现代电影 AI，自然近场（JARVIS 式） | WSOLA −1 st；轻合唱 20 ms ×2 / 10%；HPF 100 Hz；presence 3 kHz +1.5 dB；LPF 7 kHz；FDN RT60 0.2 s wet 0.1；压缩 2.5:1；宽带 de-esser（7 kHz / −28 dB）+ LPF 7 kHz |
| `edith` | 现代电影 AI，更冷/更数字（EDITH 式） | jarvis + formant 上移 1.15；presence 3 kHz +2.0 dB；FDN RT60 0.2 s wet 0.08 |
| `ai-modern` | 现代电影 AI 带轻微机械感 | jarvis + formant 上移 1.2 + 轻音高校正（amount 0.3 / 200 ms / block 4096） |
| `broadcast` | E1 播音员（报告 §6.3） | 广播 EQ 曲线（HP 85 Hz、+1.5 @250 Hz、−1.5 @800 Hz Q4、+2.5 @3 kHz、+1.5 @5.5 kHz、LP 7 kHz、−1.5 @7 kHz Q4）；de-esser（6.2 kHz / −22 dB）；压缩 3:1；FDN RT60 0.25 s |
| `broadcast-e2` | E2 电台/DJ，更密 | 同 EQ 且 250 Hz 为 +3 dB；de-esser（6.2 kHz / −22 dB）；压缩 5:1 快；FDN RT60 0.25 s |
| `broadcast-e3` | E3 电话会议 | 带通 300–3400 Hz；+1 dB @1 kHz；压缩 5:1 极快起音 |

说明：

- 被删除的 `filmai` / `filmai-d2` / `broadcast-e1` 是 `jarvis` / `ai-modern` /
  `broadcast` 的重复别名（链路逐字节相同，`broadcast-e1` 仅差一个注释头）；
  自 v0.5.5 起请使用规范名。
- 广播家族按可行性报告 §6.3 实现；EBU R128 **−23 LUFS** 交付定标**未**写入
  profile——广播交付前需离线测量/增益级（报告 §6.4）。
- `placeholder` 是最小保留占位（upmix + limiter），不是生产风格。

## 库 API

引擎由一组独立 `pkg/` 包组成。示例
（`examples/edgefx/effects.go`，可 `go run ./examples/edgefx` 运行）：

```go
package main

import (
	"context"
	"fmt"
	"os"

	edgetts "github.com/OodavidsinoO/edge-fx-tts"
	"github.com/OodavidsinoO/edge-fx-tts/pkg/config"
	"github.com/OodavidsinoO/edge-fx-tts/pkg/effects"
	"github.com/OodavidsinoO/edge-fx-tts/pkg/pipeline"
	"github.com/OodavidsinoO/edge-fx-tts/pkg/tts"
)

func main() {
	ctx := context.Background()

	// 1. 经 tts.Synthesizer 接口合成（edgetts 实现）。
	synth := tts.NewEdgeTTS(edgetts.New(edgetts.WithVoice("en-US-GuyNeural")))
	stream, err := synth.Stream(ctx, "Hello from edge-fx-tts.")
	if err != nil {
		panic(err)
	}
	defer stream.Close()

	// 2. 加载预设效果链（可另传外部覆盖目录）。
	spec, err := config.LoadProfile("ai-modern", "")
	if err != nil {
		panic(err)
	}

	// 3. 由冻结的 ChainSpec 构建效果节点。
	nodes, err := effects.BuildChain(spec)
	if err != nil {
		panic(err)
	}

	// 4. 运行流式管线：解码 -> 效果链 -> WAV sink。
	out, err := os.Create("output.wav")
	if err != nil {
		panic(err)
	}
	defer out.Close()

	sink, err := pipeline.NewWAVSink(out, spec.SampleRate, spec.Channels)
	if err != nil {
		panic(err)
	}
	p, err := pipeline.New(stream, nodes, spec.SampleRate, spec.Channels, spec.ChunkSamples, sink)
	if err != nil {
		panic(err)
	}
	if err := p.Run(ctx); err != nil {
		panic(err)
	}
	fmt.Println("wrote output.wav")
}
```

`tts.Synthesizer` 接口（`Stream` / `StreamSSML`，均返回流式 MP3 `io.ReadCloser`）使引擎
与后端解耦。`tts.NewEdgeTTS` 包装根包的 `edgetts.Client`，后者还提供常用的一次性助手
（`Save`、`Bytes`、`Stream`、`StreamSSML`、批量和 ZIP 输出、voice 列表/筛选）。
自定义链可写成 YAML/JSON，用 `config.Load` / `config.LoadBytes` 加载，不必用预设名。

## 开发

```bash
go test ./...
go vet ./...
go test -race ./...
```

测试覆盖 CLI、config schema、单效果行为（恒等/直通、左右声道隔离、量化、门限、
热循环零分配）以及解码/管线往返。CI 在 push 到 `main` 及每个 PR 时运行
`go test ./...` + `go vet ./...`。

## 许可证

MIT — 见 [LICENSE](LICENSE)。Copyright (c) 2024 虫子樱桃（上游 `lib-x/edgetts`）、
2026 OodavidsinoO。
