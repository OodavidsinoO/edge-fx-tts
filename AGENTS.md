# Repository Guidelines

## Project Overview

`github.com/lib-x/edgetts` — a Go library for Microsoft Edge TTS (text-to-speech). A thin public API over a WebSocket protocol that mirrors what Edge's browser uses: DRM-style `Sec-MS-GEC` tokens, SSML wrapping, and audio chunk streaming from `wss://speech.platform.bing.com`. Go 1.25.0, MIT licensed, 3 direct deps (`google/uuid`, `gorilla/websocket`, `golang.org/x/net`).

Two API generations coexist:

- **New `Client` API** (recommended) — `edgetts.New(opts...)` + functional options; symmetric Text/SSML entry points (`Bytes`/`BytesSSML`, `Save`/`SaveSSML`, `Stream`/`StreamSSML`, `WriteTo`/`WriteSSMLTo`), request objects (`Text(...)`/`SSML(...)`), batch (`Batch`, `SaveBatch`, `WriteZIP`), voices (`Voices`, `FindVoice`, `FilterVoices`).
- **Legacy `Speech` API** (deprecated compat wrapper) — `speech.go`; kept for migration only. Don't extend it.

## Architecture & Data Flow

```
root edgetts (client.go, option.go, types.go, voiceManager.go, speech.go)
  └─ newCommunicate: mergeOptions (client opts → per-call opts, later wins) → toInternalOption
     └─ internal/communicate: NewCommunicate → CheckAndApplyDefaultOption (defaults) → validate.WithCommunicateOption (regex) → WriteStreamToContext
        ├─ internal/communicateOption — CommunicateOption struct + defaulting
        ├─ internal/validate — regex validation of voice/pitch/rate/volume
        ├─ internal/businessConsts — endpoint/trusted-client-token/Chrome-version/DefaultVoice constants
        └─ internal/ttsTask — legacy worker-pool tasks; DEAD CODE (nothing imports it)
```

Request lifecycle: `Client.WriteRequestTo` → empty-input check (`ErrEmptyInput`) → fresh merged option → `NewCommunicate` (defaults + validation) → `WriteStreamToContext` → `stream()`: one goroutine, one unbuffered `chan map[string]interface{}`; **one new WebSocket connection per text chunk**; each sends `speech.config` then an `ssml` message (`X-RequestId`: uuid without dashes, `X-Timestamp`: GMT), then `connStreamExchange` reads messages: binary audio payloads (2-byte big-endian length header) are written sequentially to the user's `io.Writer`; text `turn.start`/`turn.end`/`WordBoundary` drive state. Public `Stream`/`StreamSSML` adapt the channel to `io.Pipe` + `io.ReadCloser`.

Key internals:

- `internal/communicate/drm.go` — `generateWssEndpoint()` appends `Sec-MS-GEC` (SHA-256 of bucketed-ticks + `TrustedClientToken`) and `Sec-MS-GEC-Version`; Chrome version 130.0.2849.68.
- `internal/communicate/utils.go` — `splitTextByByteLength` (bufio.Scanner with custom split func, never splits mid-token; `getMaxMessageSize` = 1<<16 minus SSML header overhead + 50), escape (`>`/`<`), `removeIncompatibleCharacters`.
- `internal/communicate/handlers.go` — message dispatch, audio chunk parsing, `WordBoundary` offset math (`wordBoundaryOffset = 8_750_000`, cross-chunk `shiftTime`).
- `internal/communicate/ssml.go` — `makeSsml` + `Speak`/`Voice`/`Prosody` XML types.
- `internal/communicate/ws_proxy.go` — `applyWebSocketProxyIfSet`: HTTP proxy, SOCKS5 (`x/net/proxy`), `InsecureSkipVerify`.
- `voiceManager.go` — `VoiceManager` with `sync.Once`-built, `Clone()`d request header; `ListVoices` GETs the voice-list endpoint.

## Key Directories

| Path | Purpose |
| --- | --- |
| `client.go`, `option.go`, `types.go`, `voiceManager.go`, `errors.go` | Public API surface (module root = importable by consumers) |
| `speech.go` | Deprecated `Speech` compat wrapper |
| `cmd/demo/` | Runnable demo CLI (`go run ./cmd/demo`) |
| `internal/communicate/` | WebSocket engine: protocol, DRM, SSML, chunking, proxying |
| `internal/communicateOption/` | `CommunicateOption` + `CheckAndApplyDefaultOption` |
| `internal/validate/` | Regex validation, returns bare sentinels |
| `internal/businessConsts/` | Endpoint/token/version/`DefaultVoice` constants (note typo: `vlidate.go`) |
| `internal/ttsTask/` | Dead legacy task model — do not use |

## Development Commands

```bash
go test ./...   # unit tests — fast, network-free, sub-second
go vet ./...    # only static check enforced (CI + release)
go run ./cmd/demo -text "hello world" -voice en-US-GuyNeural -output hello.mp3
go run ./cmd/demo -type ssml -text '<speak ...>...</speak>' -output hello.mp3
go run ./cmd/demo -h   # all flags: -type -text -output -voice -rate -pitch -volume -stream
```

CI (`.github/workflows/ci.yml`): push to `main` + PRs, ubuntu-latest, Go version from `go.mod` via `go-version-file`. No linter, no coverage gate, no Makefile. Release (`.github/workflows/release.yml`): tag `v*` → re-runs test+vet → extracts the `## <tag>` section from `CHANGELOG.md` via awk as release notes (placeholder body if missing, release still proceeds). Bumping `go` in go.mod silently changes what CI runs.

## Code Conventions & Common Patterns

- **Functional options**: `type Option func(option *option)` mutating an unexported struct; applied in order, later wins. Per-call options re-apply all funcs on a fresh struct (no aliasing). New option funcs: `WithXxx`, canonical proxy names ALL-CAPS (`WithHTTPProxy`, `WithSOCKS5Proxy`); keep legacy aliases (`WithHttpProxy`, `WithSocket5Proxy`) byte-identical.
- **Internal/root type duplication is systematic**: `option` → `communicateOption.CommunicateOption` (names diverge: `HTTPProxy`→`HttpProxy`, `SOCKS5`→`Socket5`, `IgnoreSSLVerification`→`IgnoreSSL`); `InputType` duplicated; channel error markers (typed structs in `map["error"]`) vs root sentinels. Mirror it rather than refactoring it.
- **Naming**: camelCase, PascalCase JSON tags; pointer receivers for stateful types (`Client`, `VoiceManager`, `Communicate`), value types for DTOs. Exported API fully doc-commented; `Deprecated:` markers on `Speech`.
- **Error handling**: sentinels via `errors.New` in `errors.go` (`ErrEmptyInput`, `ErrBatchEmpty`, `ErrVoiceNotFound`, `ErrNoAudioReceived`); `fmt.Errorf("...: %w", err)` wrapping at save/stat/zip boundaries. Validation errors are bare sentinels (`InvalidVoiceError` etc.) — check with `errors.Is`. Note: root `ErrNoAudioReceived` never matches internal stream failures (internal wraps its own separate instance) — don't assert it against network flows.
- **Concurrency**: `Client` is stateless per request (fresh merged option each call) + immutable `*VoiceManager` → safe for concurrent use. `sync.Once` for package-level headers, then `Clone()` per request. Internal `Communicate` state (`shiftTime`, `finalUtterance`, `prevIdx`) is mutated during a stream — never share one `Communicate` across goroutines. `connStreamExchange` selects with a `default` arm, so ctx cancellation is only observed between blocking `conn.ReadMessage()` calls — a stalled server can hang despite a cancelled ctx, and an early consumer return can leak the producer goroutine blocked on the unbuffered channel.
- **Atomic file writes**: temp file (`path + ".tmp"`) + `os.Rename`, cleanup on failure.
- **Validation**: strict regexes — voice must end `Neural`; pitch `^[+-]\d+Hz$`; rate/volume `^[+-]\d+%$`. Semitone (`st`) and named values (`default`, `high`) are **rejected** despite being documented in `ssml.go` comments.

## Important Files

- `client.go` — `Client`, `New()`, all synthesis/batch/voice methods, `FilterVoices`, package-level one-offs, `mergeOptions`, `newCommunicate`, `saveRequest`, `streamRequest` (io.Pipe + `CloseWithError`).
- `option.go` — all `With*` options + alias pairs.
- `types.go` — `InputType`, `Request`, `Text`/`SSML` builders, `BatchItem`/`BatchResult`, `VoiceFilter`.
- `errors.go` — public sentinels.
- `internal/communicate/communicate.go` — `NewCommunicate`, `WriteStreamToContext`, `stream()`, `connStreamExchange`, `buildPayloads`.
- `internal/communicate/drm.go` — endpoint + token generation.
- `internal/businessConsts/constants.go` — `EdgeWssEndpoint`, `TrustedClientToken`, `DefaultVoice` (`zh-CN-XiaoxiaoNeural`), Chrome version.
- `CHANGELOG.md` — Keep-a-Changelog style; **every new `v*` tag needs a matching `## vX.Y.Z - YYYY-MM-DD` section**.

## Runtime/Tooling Preferences

- Go 1.25.0 (from `go.mod`); stdlib only for tests — no testify, no test-only deps. Keep tests network-free: network-facing tests use empty/blank inputs that short-circuit on sentinels; the 13 real-synthesis examples in `examples_test.go` are compile-only (**no `// Output:` comment** — adding one makes CI depend on live Edge endpoints).
- `golang.org/x/net` is used only for SOCKS5 (`internal/communicate/ws_proxy.go`) — removing proxy support makes it an unused dep.
- Docs are bilingual and tight-coupled: any public-API or demo-flag change must update `README.md`, `README.zh-CN.md`, and (flags) `cmd/demo/main.go` help text. English README is canonical; zh-CN mirrors it section-for-section plus a TOC.
- Dependabot: gomod ecosystem, weekly, root only. `.gitignore`: standard Go deny-list (`*.test`, `*.out`, `go.work`, binaries). Actions pinned by major tag — follow that convention.

## Testing & QA

- `go test ./...` — pure stdlib `testing`; white-box unit tests (package `edgetts`) in `client_test.go`, `option_test.go`, `speech_test.go`; black-box examples (package `edgetts_test`) in `examples_test.go`. No `internal/` tests, no table-driven tests, no subtests, no `t.Skip`, no env vars.
- Conventions: flat `Test<Subject><Condition>` names, `Example<Func>` / `ExampleClient_<Method>[_suffix]`; fail-fast `t.Fatal` assertions; `errors.Is` for sentinels; `t.TempDir()` for files.
- Contracts the tests pin — change with care: whitespace-trimmed `ErrEmptyInput` (blank input too), `ErrBatchEmpty` for nil batches, `toInternalOption` + `CheckAndApplyDefaultOption` defaults (`DefaultVoice`, `+0Hz`, `+0%`, `+0%`), `writeJSON` shape, `SaveBatch` no-file-left-on-error, examples compile-guarding the full public surface (signature changes break example compilation).
- No coverage target; `go vet ./...` is the only static check. Network behavior can't be tested in CI — new network-path logic must be proven with a throwaway script instead.

## Agent skills

### Issue tracker

Issues and specs live as GitHub issues in `OodavidsinoO/edge-fx-tts`, operated via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Default vocabulary, label string equals role name: `needs-triage` / `needs-info` / `ready-for-agent` / `ready-for-human` / `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` at the repo root + `docs/adr/` for decisions. See `docs/agents/domain.md`.
