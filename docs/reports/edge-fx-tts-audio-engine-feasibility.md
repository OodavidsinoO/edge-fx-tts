# 《edge-fx-tts 音频引擎可行性研究报告》

- **项目**：`github.com/OodavidsinoO/edge-fx-tts`（Go 模块，Go 1.25.0）
- **日期**：2026-09-09
- **性质**：研究与规划（无业务实现代码）
- **方法**：4 个并行 web-search 调研 agent（DSP 选型与编解码 / 效果器参数 / 架构 / 风险可行性）+ 1 个追加调研 agent（电影 AI 人声与播音员声预设）+ 主线一手验证（go-mp3 issue #12、edge-tts 源码、tosone/minimp3 源码、本机工具链实测）；所有外部事实标注 `verified 2026-09-09` 与来源 URL
- **文档读者**：后续进入实现对仓库起指导作用；被 `docs/agents/domain.md` 消费

---

## 0. 执行摘要（结论先行）

| 决策项 | 结论 | 依据 | 状态 |
| --- | --- | --- | --- |
| TTS 接入 | **封装 `lib-x/edgetts`**，经自研 `tts.Synthesizer` 接口隔离 | 活跃库（2026-07-13 推送）、Go 1.25 匹配、`Client.Stream` 返回 MP3 字节流 | ✅ 已决策（用户 A） |
| 解码 | **cgo minimp3**（`tosone/minimp3`，已读源码核实） | Edge 输出 24kHz = MPEG-2 Layer 3；go-mp3 仅 MPEG-1 且仓库已归档（issue #12 原文报错） | ✅ 已决策 |
| 效果引擎 | **纯 Go**：`gopxl/beep`（Streamer 拉流框架+重采样）＋`CWBudde/algo-dsp`（效果算法） | go-sox 2018 归档、mpg123 绑定全死、CGO 牺牲跨平台；本机实测无 ffmpeg/sox/lame | ✅ 已决策 |
| 输出与流式 | **三协程流式管道**，内部恒 24k 单声道 float32，输出 **WAV 16-bit 默认**，ffmpeg 探测到才启用 MP3 | 负载 96 KB/s 极小；minimp3 EOF 语义要求持续 reader；Go 生态无维护中的纯 Go MP3 编码器 | ✅ 已决策 |
| 效果链编排 | YAML `stages[]` 顺序链 + registry 工厂 + **未知效果 fail-fast** + 参数级热重载 + 强制链尾 limiter/总混 | SoX/ffmpeg filtergraph/Web Audio 三先例 | ✅ 已决策 |
| Profile 机制 | **`//go:embed` 编译期打包** + 外部 YAML 覆盖同名 + CLI `--profile <name|none>`；`none` 即直通（等同 TTS 库原功能）；FX 为独立可插拔管线 | 用户要求 | ✅ 已决策 |
| 采样率 | **配置可切换，默认 24k**；`sampleRate: 48000` 时链头自动插重采样 | 用户要求 | ✅ 已决策 |
| V1 范围 | CLI：单句 + 批量文本 + 直通；库 API 仅 `pkg/` 核心生效 | 用户要求 | ✅ 已决策 |
| 开源协议 | MIT（与全部依赖兼容） | 用户要求 | ✅ 已决策 |
| **V1 自研（用户特批）** | **formant shifter + autotune 式逐音节音高量化** 两项缺口 V1 全做 | GLaDOS 式"一听就是 AI"需二者齐上 | ✅ 已决策（用户 C） |
| 新预设 | 电影 AI / 播音员声（预设 D/E，含子变体）编入默认 profile 集 | 追加调研结果 | ✅ 已决策 |

**总裁定**：本项目 **纯 Go 完全可行且是正确选择**——24kHz 单声道语音后处理负载极小（≈96 KB/s），Go 的 GC 停顿毫秒级且与堆大小弱相关（官方指南 + Twitch/Pusher/atdiar 实测证据），C++ 与 Go 的 2–10× 差距对此负载无意义（纯 Go libopus 实测仅 1.7–2.3×）；真正风险是**功能覆盖面**（编码器缺口、两个机器人声缺口）而非性能。唯一不可回避的成本：minimp3 引入 cgo（交叉编译需 C 工具链）。

---

## 1. 音频处理库选型（用户问题 1）

### 1.1 三方案对比（verified 2026-09-09）

| 维度 | 纯 Go（beep + algo-dsp） | CGO（go-sox / mpg123 绑定） | 子进程（ffmpeg / sox） |
| --- | --- | --- | --- |
| 多效果串联 | beep.Streamer 拉流嵌套 + algo-dsp `dsp/effects`（Chorus 多声部 / FeedbackDelay / FDN+Schroeder 混响 / Flanger / Phaser / RingMod / BitCrusher / 压缩器组 / WSOLA 变调 / Bark vocoder）、`dsp/conv` 分区卷积 IR、`dsp/filter/biquad`、`dsp/resample` polyphase FIR | libsox 效果链 1:1 对应 CLI，链表达力与 sox 一致；但绑定仓库已归档 | ffmpeg filter 图极完整（`aecho`/`chorus`/`aphaser`/`afir`/`aresample`/`acrusher`/`acompressor`/`alimiter`），官方文档核实 |
| CGO 复杂度 | 0（CGO_ENABLED=0 可行） | 必须 CGO：go-sox 需 libsox + pkg-config；minimp3 `import "C"` | 0（但依赖系统二进制） |
| 跨平台 | 纯 Go 交叉编译零负担 | 各平台需 C 工具链，Windows 尤痛 | 需随包分发二进制（本机即无） |
| 性能 | 块级零分配热路径 + SIMD（SSE2/AVX2/NEON）；24k 单声道远低于实时算力 | C 库性能强，但 cgo 边界开销 + 绑定死码 | 每 utterance spawn 数十 ms 可接受，每 chunk 不可接受 |
| 维护 | beep: 活跃（589★，gopxl 接管后 2025-07 推送）；algo-dsp: 2026-02 创建、**2026-09-08 刚推送**、连续发版至 v0.7.1、（年轻、3★、单维护者风险） | go-sox pushed 2018-06-20 已 archived；sdobz/oandrew mpg123 绑定 2017–2018 死码 | ffmpeg 文档持续更新；本地无二进制 |

### 1.2 结论（明确）

- **核心引擎 = 纯 Go**：`gopxl/beep`（运输层：Streamer 拉流、`Resample`、`wav.Encode`）＋ `CWBudde/algo-dsp`（算法层：全部所需效果 + 24k→48k polyphase FIR 重采样）。beep 自带 effects 只有 volume/pan/equalizer 等，**必须**配 algo-dsp。
- **CGO 绑定否决**：go-sox 已 archived（2018 起零提交）、mpg123 绑定全死；CGO 交叉编译要求每目标平台 C 工具链，直接抵消纯 Go 分发优势。
- **子进程否决为核心引擎，留作可选后端**：本机实测 `which ffmpeg sox lame` 全空；进程启动开销 + 部署风险转嫁用户。ffmpeg 表现力足以覆盖三个目标听感（官方 filter 文档核实），适合 v2 可选后端；JSON→filter graph 配置驱动映射完全可行。
- 注意 beep：原 `faiface/beep` 已移交 `gopxl`（原版 2024-03 后停滞）——**用 gopxl 不用 faiface**。

### 1.3 来源

https://github.com/gopxl/beep · https://github.com/faiface/beep · https://github.com/CWBudde/algo-dsp · https://github.com/CWBudde/algo-dsp/blob/main/EFFECTS.md · https://github.com/krig/go-sox · https://github.com/sdobz/go-mpg123 · https://github.com/oandrew/go-mpg123 · https://ffmpeg.org/ffmpeg-filters.html

---

## 2. 音频解码与编码链路（用户问题 2）

### 2.1 决定性事实：Edge 输出 = MPEG-2 Layer 3

- 本仓库 `internal/communicate/communicate.go:177` 与 Python 参考实现 edge-tts `communicate.py:438` **均硬编码** `outputFormat: "audio-24khz-48kbitrate-mono-mp3"`（两个仓库源码均已读核实）。
- edge-tts 的 `TTSConfig`（data_classes.py）**无格式字段**（已读核实）：24kHz 是标准库的固定输入。
- 24kHz 只存在于 **MPEG-2 采样率族**（32000/44100/48000 为 MPEG-1）。
- `hajimehoshi/go-mp3` 仅支持 MPEG-1：issue #12 原始报错 `mp3: only MPEG version 1 (want 3; got 2) is supported`；仓库 2023-04-02 已归档 read-only（页面横幅核实）。

**推论**：go-mp3 对本项目**不可用**（直接报错，无冒烟可解）；"换 24kHz 为 go-mp3 可解的 48kHz"纯 Go 路线需要逆向改协议，未验证、不采纳。

### 2.2 解码选型：`tosone/minimp3`（cgo，已读 decode.go 源码核实）

| 项 | 结论 |
| --- | --- |
| 类型 | cgo 绑定 lieff/minimp3（内嵌单头文件实现，MIT），Go 1.15+ |
| 维护 | 活跃：2025-07-09 推送、CI 绿、133★ |
| 24kHz | 逐帧读 `info.hz`，MPEG-1/2/2.5 全采样率族，显式输出 `SampleRate/Channels/Kbps/Layer` |
| 输出 | 16-bit LE PCM 字节流 |
| 流式 | `NewDecoder(io.Reader)` 双 goroutine（读 + 解），内部 ~10KB 缓冲、10ms 轮询 |
| 一次性 | `DecodeFull([]byte)` 全量解码 |
| 零拷贝 | 否（每帧两次全帧 memcpy + 每次 Read copy）——对本负载无影响 |

**两个设计约束（必须写进实现）：**
1. **EOF 语义**：喂入的 reader 必须在整个 utterance 内持续**不返回 EOF**（`originalEof` 一旦置位 `Read` 永久 EOF）——Edge WS 分块边界绝不能以 EOF 结尾；这正是三协程管道（下一节）存在的原因之一。
2. **立体声输出**：Edge mono 源 → 解码得 stereo 16-bit，需 downmix（取 L 或 (L+R)/2）。

### 2.3 编码选型

| 输出 | 方案 | 结论 |
| --- | --- | --- |
| WAV | 自研 RIFF 写出（16-bit PCM，约 100 行）或 beep/wav 流式写（支持 1/2/3 字节精度、自动回填头） | go-audio 组织 12 仓库全 archived（wav 391★ 停更），不押注；**WAV 为默认输出** |
| MP3 | **无维护中的纯 Go 编码器**：shine-mp3（README 自称"唯一纯 Go"，活跃至 2026-08-26，但 MPEG-1 only、一次性 `Write` API、License Other/NOASSERTION 商用需人工核实）；LAME 绑定全死（gonutz/lame 404，viert 2019 停更，sjzar fork 0★） | **V1 方案（按决策）**：检测到 ffmpeg 才启用 MP3（`-c:a libmp3lame`），否则清晰报错；WAV 是唯一中间契约 |

### 2.4 流式处理设计（用户问题 2c；决策 3 = 流式管道 + WAV 默认）

```
edgetts WS（length-prefixed chunks, io.Pipe 持续写、不 EOF）
  → minimp3.NewDecoder 流式解码（16-bit LE stereo）
  → 缓冲适配（chunk 积累 ≥16KB；MPEG-2 帧=576 采样，4096 块非整数倍 → 帧边界积累）
  → 效果链（24k mono float32，拉取式）
  → 编码消费者（WAV 写文件；可选 ffmpeg stdin 管道）
```

三协程 + 两个 SPSC 有界 ring（16 × 32KiB），阶段间并行、无锁；内存恒定、天然背压。10s 语音 ≈ 480KB（stereo 16-bit）或 960KB（float32 stereo）——流式对内存是"稳"而非"必需"，但规避 minimp3 EOF 语义问题是**必需**。

### 2.5 来源

https://github.com/hajimehoshi/go-mp3 · https://github.com/hajimehoshi/go-mp3/issues/12 · https://github.com/tosone/minimp3 · https://github.com/tosone/minimp3/blob/main/decode.go · https://github.com/lieff/minimp3/blob/master/README.md · https://github.com/go-audio · https://github.com/braheezy/shine-mp3 · https://github.com/gopxl/beep/blob/main/wav/encode.go · https://github.com/rany2/edge-tts/blob/master/src/edge_tts/communicate.py · https://raw.githubusercontent.com/rany2/edge-tts/master/src/edge_tts/data_classes.py · 本仓库 internal/communicate/communicate.go:177

---

## 3. 效果器参数化（用户问题 3）

### 3.1 算法选型关键结论（24kHz 特调）

- **混响必须用 FDN**：algo-dsp 的 Schroeder `Reverb`（8 comb + 4 allpass）把 comb 长度硬编码为 44.1kHz 样本数且构造器不收采样率（source-verified）；在 24kHz 下等效变 46–68ms → 金属/梳齿声。`FDNReverb` 有显式 `lineDelayScale = sampleRate/44100`、RT60 直接以秒设置、含 pre-delay 与 LFO 调制，为 24k 语音首选。备用：卷积混响（UPOLA，latency≈2.7ms@24k，需自带 IR 资产）。
- **双音感用合唱，不用镶边**：sox 硬校验 chorus 基延迟 ≥20ms（低于即梳状滤波区）；LFO 正弦 0.1–0.35Hz、wet ≤30–40%；flanger 只用于科幻层（低湿比 20–30%、base ≥2ms）。
- **slapback 必须 0 反馈**（BeatKey 明确"1–2% feedback 就毁了"），返回路 HPF 150–250Hz。
- **语音可懂度守则**：总链 HPF 80–100Hz；pre-delay 10–30ms 保字头；延迟/混响返回路 ducking 6–10dB（algo-dsp `Compressor.ProcessSampleSidechain` 现成）；反馈回路低通 ≈2–4kHz。

### 3.2 三套预设（起点参数；各参数可溯源）

**预设 A「合成AI感」**（可懂度优先）：

| 级 | 参数 | 值 |
| --- | --- | --- |
| 高通 | HPF | 100 Hz |
| 压缩 | thresh/ratio/att/rel | −20 dB / 2:1 / 10 ms / 100 ms |
| 失谐 | WSOLA | +2…+5 cents, mix 30% |
| 窄带 | bandpass | 300–3400 Hz, mix 30–50% |
| bitcrush | bit/mix | 8–10 bit / 10–20% |
| 合唱 | base/depth/LFO/mix | 22 ms / 2 ms / 0.25 Hz / 15–20% |
| 混响 | FDN RT60/damp/pre/wet | 0.8–1.2 s / 0.4 / 10 ms / 12–18% |
| 总 wet/dry | — | 干 100% / 湿 20–30% |

**预设 B「电影双音延迟」**：

| 级 | 参数 | 值 |
| --- | --- | --- |
| 高通 | HPF | 80 Hz |
| 压缩 | ratio/att/rel/GR | 3:1 / 10 ms / 150 ms / 3–6 dB |
| 合唱（加倍） | base/depth/LFO/decay/声部 | 25 ms / 1.5 ms / 0.15 Hz / 0.35 / 2 声部, wet ~35% |
| slapback | delay/feedback/mix + 返回 HPF | 90–110 ms / **0** / 20–30% / 200 Hz |
| 混响 | FDN | RT60 1.5–1.8 s / damp 0.35 / pre 15–20 ms / wet 18–22% |
| ducking | 延迟/混响返回 | GR 6–10 dB, att 5–10 ms, rel 200–400 ms |
| 总 wet/dry | — | 干 100% / 湿 ~35% |

**预设 C「科幻氛围」**：

| 级 | 参数 | 值 |
| --- | --- | --- |
| 高通 | HPF | 80 Hz |
| 压缩 | ratio/rel/GR | 4:1 / 200–400 ms / ≤10 dB |
| 合唱（宽） | base/depth/LFO/mix | 30 ms / 4 ms / 0.3 Hz / 30%（3 声部） |
| 环境延迟 | delay/feedback/mix + 回路低通 | 400–600 ms / 0.35 / 25% / LP 2–4 kHz |
| 混响 | FDN | RT60 2.5–3.5 s / damp 0.2–0.25 / pre 20–40 ms / wet 25–30% |
| shimmer | +12 半音移调 → 混响湿路 | mix 10–15% |
| 慢 LFO 扫滤 | tremolo | 0.1–0.3 Hz / depth 0.2–0.3 |
| Haas 加宽 | delay/level | 15–20 ms / 30–50% |
| ducking + 总 wet | — | 6–10 dB；干 100% / 总湿 ~40% |

### 3.3 来源（节选）

https://man.archlinux.org/man/soxeffect_ng.7.en.txt · https://github.com/chirlu/sox/blob/master/src/chorus.c · https://ffmpeg.org/ffmpeg-filters.html · https://github.com/CWBudde/algo-dsp · https://delay.beatkey.app/slapback-delay · https://www.synchroarts.com/posts/automatic-double-tracking · https://www.soundonsound.com/techniques/creating-shimmer-reverb-effects · https://www.production-expert.com/production-expert-1/maintaining-dialog-clarity · https://wisseloord.org/academy/what-is-the-haas-effect-and-how-to-use-it-for-width

---

## 4. 架构设计规划（用户问题 4）

### 4.1 仓库布局（Go 惯例：`internal/` 防外部导入、`cmd/` 放命令；库+CLI 双面结构参照 go.dev/doc/modules/layout 与 golang-standards/project-layout）

```
edge-fx-tts/
├── go.mod                          # module github.com/OodavidsinoO/edge-fx-tts
├── cmd/
│   └── edgefx/                     # CLI 入口（薄壳装配）：main.go + run.go
├── pkg/                            # 对外库 API（import 契约）
│   ├── tts/                        # tts.Synthesizer 接口 + edgetts 实现（隔离第三方）
│   ├── format/                     # Frame/Framebuf(float32)、Format{SR,Ch}、Decoder/Encoder 接口
│   ├── effects/                    # Node 接口 + Chain + registry.go + 各效果 + upmix
│   ├── pipeline/                   # 三协程流式装配 + SPSC ring + batch（utterance 级并行）
│   └── config/                     # Schema 类型 + Load/Validate/Build + schema.json
├── internal/
│   ├── decode/                     # minimp3 封装（帧边界积累、io.Reader 适配）+ resample
│   └── encode/                     # wav.go 自研 RIFF 16-bit
├── configs/
│   ├── profiles/                   # 默认 profile YAML（编译期 embed：A/B/C/D/E + none）
│   └── examples/                   # 参考样例
├── docs/adr/                       # 工程决策记录（domain 约定）
└── CONTEXT.md                      # 领域词汇表（domain 约定；grill-with-docs 惰性创建）
```

三个接口缝保证互换便宜：`tts.Synthesizer`（edgetts ↔ raw-WS）、`format.Decoder`（minimp3 ↔ ffmpeg）、`format.Encoder`（WAV ↔ ffmpeg-MP3 ↔ 未来 lame）——全部以 `io.Reader`/`io.Writer` 字节级为界。

### 4.2 配置驱动效果链（YAML；JSON 同构）

```yaml
version: 1                 # schema 版本不匹配即拒绝
sampleRate: 24000          # 默认 24k；48000 时链头自动插重采样（决策 5）
channels: 2                # 输出声道（mono 源先过 upmix）
chunkSamples: 4096         # 内部分块（≈170ms @24k）
stages:
  - name: decode
    params: { codec: "audio-24khz-48kbitrate-mono-mp3" }   # 隐式首级
  - name: upmix
    enabled: true
    params: { mode: center }
  - name: aifake            # 预设 A
    enabled: true
    params: { detune: 0.03, bandpassHz: [300, 3400], bitDepth: 9, mix: 0.25 }
  - name: doubledelay       # 预设 B
    enabled: false
  - name: scifiatmo         # 预设 C
    enabled: false
  - name: limiter           # 强制终段
    params: { ceiling: 0.95 }
output:
  format: wav               # wav | mp3 | raw
  bitDepth: 16
  mp3: { bitrate: 128 }
```

- **未知 effect**：加载期**硬错误（fail-fast）**并列出注册表已知名；只有 `enabled: false` 才跳过（静默跳过掩盖拼写错误）。
- **顺序校验**：时间型效果（延迟/混响/合唱）不得出现在 upmix 前；limiter 必须末级。
- **热重载**：参数级可行（chunk 边界 `atomic.Pointer[ChainSpec]` 换快照，ffmpeg 运行时 filter 命令为先例）；**结构级（增删/重排 stage）V1 不做流中热换**（时间型状态无法无缝迁移，会爆音），仅在 utterance 之间重载。
- **Profile 机制（决策）**：`//go:embed configs/profiles/*.yaml` 编译期打包；外部 YAML 覆盖同名 profile；CLI `--profile <name|none>`——`none` 走**直通管线**（TTS→解码→WAV，等同于本仓库原功能），FX 可插拔、默认即插即拆。

### 4.3 并发设计

- **链内必须串行**：时间型效果带跨采样状态（延时线/LFO 相位），音频 DSP 硬约束；并行只发生在阶段间。
- 单条流：`producer（WS→解码）→ [ring 16×32KiB] → dsp 单协程拉取（全链串行）→ [ring] → encoder`；SPSC 无锁。
- **批处理**（多文本）：按 utterance 并行（每文本独立 pipeline 实例）；edgetts 每次请求天然独立。
- **sync.Pool 纪律**（官方语义核实）：适用于临时对象复用；池内容每轮 GC 清空（victim 两轮）、取出的对象可能脏——帧块（32KiB）Get/清零/Put 正确用例，节点不得跨 chunk 持有引用；常驻工作缓冲按 GOMAXPROCS 预分配更优（省原子）。
- **共享状态**：LFO/延时线/滤波器状态全在 per-stream Node 实例内，绝不进 config（config 可并发读，Node 不可并发用）。

### 4.4 全进程 vs 混合

| | 全进程 | 混合（决策 3 采纳） |
| --- | --- | --- |
| decode | minimp3（cgo） | 同左 |
| encode | 自研 WAV；MP3 需 cgo lame（维护弱） | WAV/raw 管道给 ffmpeg，检测到才启用 |
| 二进制 | 单文件、无外部依赖（默认） | ffmpeg 存在时增强 |

### 4.5 来源

https://go.dev/doc/modules/layout · https://github.com/golang-standards/project-layout · https://pkg.go.dev/sync · https://go.dev/src/sync/pool.go · https://ffmpeg.org/ffmpeg-filters.html · https://manpages.debian.org/trixie/sox/soxeffect.7.en.html · https://developer.mozilla.org/en-US/docs/Web/API/AudioNode/connect · https://github.com/gopxl/beep/blob/main/interface.go

---

## 5. 风险与可行性评估（用户问题 5）

### 5.1 GC 与实时性（纠正流传错误）

- **Go 从未"1.21+ 分代化"**：官方称并发三色 **mark-sweep 非移动**收集器（ismmkeynote）；1.25 亮点是实验性 **Green Tea GC**（按页标记扫描，期望 GC CPU −10–40%，`GOEXPERIMENT=greenteagc` 启用，1.26 转正），**仍非分代**。
- 停顿实测：Twitch 150 万 goroutine/数百 MB 堆 ~1ms（Go 1.7 起）；Pusher 200MB 堆最坏 7ms（多为已修复 bug，STW ~1ms）；golang/go#14812 在 **6.5GB 堆** STW 仍 ~1ms。停顿受 GOMAXPROCS 而非堆大小支配（gc-guide）。
- 实时音频实证：atdiar/go_RT_audio_benchmark（Go 1.26.3/PortAudio/48kHz 64–128 帧/实时优先级 30 分钟）零 xrun，回调 p99 0.064ms vs C 0.012ms；结论"受控无分配回调体内 Go 未被排除在实时音频外"。
- **RealtimeAudio wiki 已不存在**（404，2023 wiki 迁移 golang/go#61940 时消失）——"官方反对 Go 做实时音频"流传观点不成立。
- 反面经验（beep#85 Linux 卡顿、oto#229 空闲 CPU 5%）属调度/时序抖动而非 GC。

### 5.2 分配策略（本项目）

- 大对象对 GC 几乎免费：纯 float 缓冲无指针，mark 只扫位图不追指针（ismmkeynote）；"对象又多又碎"才是压力源。
- 本负载数量级：24k mono float32 = **96 KB/s**，10s ≈ 1MB，与桌面内存带宽差 ~5 个数量级；float32 vs float64 带宽 2×，选 float32 正确。
- 工程规则：效果节点持常驻预分配输出缓冲；跨节点临时缓冲 sync.Pool（指针元素、归还置零）；禁每样本 make/append；`go build -gcflags=-m` 查逃逸；`go test -bench` 断言 allocs/op。
- GC 调节：默认 GOGC=100；容器内多片段并行时 `GOMEMLIMIT=0.9×限额`（软限制，过低会 thrash）；CLI 不内置 memory limit（官方反对）。

### 5.3 Go vs Python vs C++（定位）

- **Python 无竞争力**：edge-tts 仅客户端传输（README 核实，`--rate/--volume/--pitch` 是发服务端的 SSML prosody），无 DSP 能力；numpy 向量化快但每次调用微秒级固定开销，实时需复杂调优；GIL/无单二进制交付。
- **C++ vs Go**：Benchmarks Game 最佳条目 1.6–14×；DSP 域实测纯 Go libopus vs C = 1.68×（解码）/ 2.25×（编码），且分配只占差距 ~15%、大头是向量化缺失——对本负载（96 KB/s）该差距无意义。
- **CGO 开销**：实测单线程每次 cgo 调用 ~40ns（纯 Go 基线 0.94ns）；缓解 = **整帧批处理**（一次传整段 PCM，非逐样本）。仅当某效果/编码器纯 Go 缺失且实现贵时才值得（典型：编码器兜底）。

### 5.4 风险矩阵（24k 单声道短语音、后处理、非实时关键）

| 方案/风险 | 可能性 | 影响 | 缓解 |
| --- | --- | --- | --- |
| 纯 Go 效果缺口（ducking/真卷积等） | 中–高 | 中 | 选可实现集 + 明确不支持清单；ducking 实测已有（sidechain） |
| **编码器缺口（MP3）** | 中 | **高** | WAV 中间契约 + ffmpeg 兜底（决策 3） |
| GC 抖动（无分配控制） | 低 | 低–中 | 预分配 + sync.Pool + bench 断言 |
| CGO libsox（若上）构建/交叉编译 | 高 | 高 | 已否决为主引擎；保留纯 Go 回退链 |
| ffmpeg 子进程打包/配置漂移 | 中 | 中 | 版本锁定 + golden 测试（仅编码兜底） |
| edgetts 逆向服务风控（token/端点变动） | 中 | 高 | Synthesizer 接口隔离；限定速层提示 |
| Edge 24k 码率底噪 × bitcrush | 确定 | 低 | bitcrush 前置 + 解码后 11–12kHz 低切 + de-esser 兜底 |

**最终裁定**：**纯 Go 对本负载"真·够用"**——96 KB/s 负载下 GC/CGO/C++ 差距全淹噪声，瓶颈只可能是功能缺口而非性能；唯一不可回避成本是 minimp3 的 cgo（构建约束，非性能）。CGO(libsox) 仅作未来"某效果成硬需求"时整帧批处理上的升级路线。

### 5.5 来源（节选）

https://go.dev/doc/gc-guide · https://go.dev/blog/greenteagc · https://go.dev/blog/ismmkeynote · https://golang.design/under-the-hood/en/part4memory/ch13gc/generational/ · https://github.com/golang/go/issues/14812 · https://blog.twitch.tv/en/2016/07/05/gos-march-to-low-latency-gc-a6fa96f06eb7/ · https://github.com/atdiar/go_RT_audio_benchmark · https://github.com/faiface/beep/issues/85 · https://github.com/tphakala/go-opus/issues/5 · https://aureliar.net/posts/benchmarking-go-ffi/ · https://www.jefftk.com/p/effect-of-numpy

---

## 6. 追加调研：电影 AI 人声质感 + 播音员声预设（用户扩展要求）

### 6.1 制作一线事实（verified 2026-09-09，均一手访谈）

| 标杆 | 手法 | 对本项目 |
| --- | --- | --- |
| HAL 9000 | **纯表演+剪辑**：去呼吸=非人感核心；录音指示"even softer, in the depths" | 流式无剪辑期 → 需噪声门或接受"有呼吸的 AI" |
| GLaDOS | Valve 官方管线：pitch constrained + modulation suppressed + **formant moved up**（Melodyne 还原：吸附最近半音→压平→formant 上调可一整个八度） | **真缺口：autotune 量化 + formant shifter 两项** |
| Her（Samantha） | ADR 重配、breathy/nasal、音高起伏=拟人（不处理路线） | D4 微距 OS：几乎不处理 |
| Ex Machina（Ava） | 非人感在动作层（陀螺仪/水晶碗/接触麦），人声几乎不处理 | 同上克制路线 |
| 《攻壳机动队》 | 1995：扬声器悬吊 25L 素烧陶罐口下录反弹；Innocence：带盖 PVC 桶；2017 真人版 Kuze：Krotos Dehumaniser 2 + 抠字口吃 | **赛博空间人声 = 窄带 + 短促腔体回声**，全部 algo-dsp 现成 |
| 通用机器人对白（Boom Box Post） | 降 1 半音 → flanger depth 满/rate≈0.41Hz → doubler 4 声部 <50 cents → 短 plate/IR | 现成范式 |

科学佐证：ACM THRI 2023（doi:10.1145/3632124）——F0 与共振峰参数可靠地让听者判断"机器人大小"，支持 formant 单独搬移是正当手段。

### 6.2 预设 D「电影 AI 感 / 攻壳氛围」（D-默认 主链，全部 ✅/🔧 可落地）

| 阶段 | 参数 | algo-dsp 映射 |
| --- | --- | --- |
| 1 微降调 | WSOLA 0.944（−1 st） | ✅ WSOLA 0.25–4.0 |
| 2 相位化 | flanger depth 6–10 ms / LFO ≈0.4 Hz 慢满深 | ✅ Flanger(0.1–10ms) |
| 3 失谐层叠 | ±12–25 cents × 3 声部（<50 cents 范式） | ✅ Chorus |
| 4 窄带 | HP 300 + LP 3400 Hz（12–24dB/oct）+ 可选 800/1.5k/2.8k Hz bell Q10–12 +2–4 dB | ✅ biquad 级联 |
| 5 腔体回声 | FDN RT60 0.3–0.6 s / pre 30–80 ms / damp 高（≈陶罐）；或金属桶 IR | ✅ FDN / ✅ 卷积（需 IR） |
| 6 压平 | ratio 3–4:1 / att 10–20 ms / rel 80–150 ms / GR 3–6 dB | ✅ Compressor(+sidechain) |
| 7 齿音控制 | de-esser 阈值中档（窄带下 s/z 更突出） | ✅ de-esser |

子变体：**D1 攻壳广播腔**（= 主链全开，基座）；**D2 GLaDOS 量化合成**（❌ 硬缺口：autotune 吸附最近半音 + formant 上移 → 见 6.3 决策）；**D3 HAL/TARS 冷静服务器嗓**（WSOLA −2~−3 st + 强压缩 4:1/att 5ms + FDN 0.4–0.8s；去呼吸 = ❌ 缺噪声门/剪辑期）；**D4 微距 OS**（近讲 80–120Hz +1~2dB、3k +2~3dB、轻柔压缩、FDN 0.15–0.3s，其余不处理）。

### 6.3 预设 E「播音员 / 电台广播声」（全部现成；唯一缺口是交付定标）

| 阶段 | 参数 |
| --- | --- |
| 高通 | HP 70–100 Hz |
| 温暖/近讲 | 200–300 Hz +1~2 dB |
| 鼻音衰减 | 800 Hz Q 高 −1~2 dB |
| presence | 3 kHz +2~3 dB；5–6 kHz +1~2 dB |
| 齿音 | 6–8 kHz 窄带衰减 + de-esser |
| 压缩 | 3–4:1 / att 10–20 ms / rel 80–150 ms / GR 3–6 dB |
| 短混响 | FDN RT60 0.2–0.3 s 低电平 damp 高 |
| 定标 | **−23.0 LUFS（±1.0 LU）/ 真峰值 ≤ −1 dBTP**（EBU R128 v5 官方 PDF 核实）——❌ LUFS 表缺失 → 离线测量补增益或链尾定标 |

子风格：**E1 播音员**（全频，压缩 3–4:1）；**E2 电台/DJ**（更密压缩 4–6:1 att 5–10ms GR 5–8dB、200–300Hz +3dB）；**E3 电话会议**（HP300+LP3400 带通 + bells、压缩 5:1 快攻）。

### 6.4 缺口清单（V1 自研决策依据）

| 缺口 | 说明 | 工作量（报告估算） | V1 处置（用户已定） |
| --- | --- | --- | --- |
| **formant shifter** | 源-滤波器解耦的共振峰搬移；algo-dsp 只有整体变调 | ~300 行（LPC 或倒谱域） | **V1 自研** |
| **autotune 式逐音节音高量化** | 基频估计 + 逐音节贴半音 + 压平；Valve 管线核心 | 显著大（基频估计 + 贴调器） | **V1 自研**（与 formant 齐上；D2 才完整） |
| vocoder 载波源 | algo-dsp 无噪声/振荡器发生器 | 小 | 预生成 WAV 嵌入或运行时生成 |
| 噪声门 | HAL 式去呼吸 | 小 | 可选 v2 |
| LUFS/峰值表 | −23 LUFS 交付定标 | 中 | 离线测量补增益 |
| 双段压缩 | 广播 1500–2500 Hz 分频 | 小（双路+分频组合） | v2 |
| phase rotator | 限幅前波形对称化 | 小 | v2 低优先 |

> **实现策略提示（供实现阶段细化，不推翻决策）**：autotune 量化（基频估计+逐音节贴调）复杂度显著高于 formant shifter；实现时可分层推进——先固定比值 + 强压缩近似（D3 级），再逐音节贴调、后量化收口，避免 V1 范围失控。报告仅作风险标注，V1 范围以决策为准。

### 6.5 来源（节选）

https://povmagazine.com/im-sure-youll-agree-theres-some-truth-in-what-i-say/ · https://web.archive.org/web/20231127204412/https://developer.valvesoftware.com/wiki/Creating_a_Portal_AI_Voice · https://theghostintheshell.jp/en/feature/interview09_2 · https://www.asoundeffect.com/ghost-in-the-shell-sound/ · https://www.asoundeffect.com/westworld-sound/ · https://www.boomboxpost.com/blog/2017/9/13/inside-sound-design-robot-dialogue-processing · https://www.soundtoys.com/wp-content/uploads/Little-AlterBoy-Manual.pdf · https://www.antarestech.com/blog/auto-tune-effect-using-auto-tune · https://benztown.com/working-with-vo-equalization/ · https://tech.ebu.ch/files/live/sites/tech/files/shared/r/r128v5_0.pdf · https://doi.org/10.1145/3632124 · https://venuspatrol.com/2008/11/portal-team-postmortem/ · https://www.redsharknews.com/creating-the-real-out-of-the-unreal-for-exmachina-sound-production · https://www.vulture.com/2014/11/bill-irwin-on-voicing-tars-in-interstellar.html

---

## 7. 附：调研用测试命令（仅调研验证用途，不构成业务实现）

```bash
# Edge TTS 输出格式核查（本仓库）
grep -n "outputFormat" internal/communicate/communicate.go        # → audio-24khz-48kbitrate-mono-mp3

# minimp3 接入验证（开发期冒烟，非业务代码）
go get github.com/tosone/minimp3
# 解码 test.mp3 打印 SampleRate/Channels（应 24000/2）

# ffmpeg 可选 MP3 编码（检测到才启用）
ffmpeg -i in.wav -c:a libmp3lame -b:a 128k out.mp3

# 可选：go-mp3 反证 24kHz 不支持（预期报 only MPEG version 1）
go run ./cmd/check24k   # 临时一次性脚本，验证后删除

# 分配审计
go build -gcflags=-m ./pkg/effects/...    # 逃逸分析
go test -bench=. -benchmem ./pkg/effects/ # allocs/op 断言
```

---

## 8. 遗留开放项（实现阶段再决策）

1. **CLI 细节**：`-text/-ssml/-file/-batch`、`--profile`、`--config`、`--output/-o`、`--voice/-v/--rate/--pitch/--volume`（对齐 edgetts demo flags）的最终旗标集（实现阶段定，可先默认对齐）。
2. **直通管线行为**：`--profile none` 输出默认 WAV（本仓库原功能是输出 MP3）——直通是否保留 MP3 原样透传（不经解码直接存文件）待定。
3. **profile 默认集**：预设 A/B/C/D（含 D1–D4 子变体）/E（含 E1–E3）的默认启用与命名（报告参数表即为起点）。
4. **FFmpeg 探测策略**：仅编码时探测 vs 启动即探测。
5. **utterance 级并行度**：默认值（建议 GOMAXPROCS）与 CLI 覆盖。

（以上不影响 V1 范围与已定决策，属实现规格细节。）
