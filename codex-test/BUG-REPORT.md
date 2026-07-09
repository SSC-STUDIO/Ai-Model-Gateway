# Ai-Model-Gateway 实测缺陷报告 / Live Test Bug Report

> 测试目标 / Goal: 对 Ai-Model-Gateway（v1.4.4）做真实请求级的实测，覆盖各协议与转接（OpenAI Chat Completions / Responses / Anthropic Messages / 流式 / 桥接 / SSRF / 重试 / 鉴权 / CLI / 运维面），定位 bug。
> 测试日期 / Date: 2026-07-06（Phase-1/2/3），含 2026-07-05 历史日志佐证。
> 测试方式 / Method: 启动运行时三件套（aigw + gatewayd + controld + telemetryd）后，用 PowerShell 直接 HTTP 打数据面 127.0.0.1:18080 与控制面 127.0.0.1:18081，并对 gatewayd.exe 做二进制串取证。全部结论均有落盘证据，非纯分析。
> 仓库形态 / Repo: 闭源/补丁仓库，仓库内无 .go 源码与 go.mod，仅有 bin/*.exe 与 configs/config.yaml、web/admin 控制台。因此本报告定位到二进制级行为与运行时日志，修复建议面向可观测证据而非具体源码行。

---

## 1. 总览 / Summary

共确认 14 个缺陷（外加 1 个历史佐证的日志缺陷），全部经真实 HTTP 请求复现，按严重级别分布：

| 级别 | 数量 | 编号 |
|---|---|---|
| P0 阻断上线/安全 | 2 | #9 数据面完全无鉴权；#13 SSRF 守卫误报阻断 localhost/LAN 上游且健康探测/请求路径不一致 |
| P1 发布前必修 | 4 | #12 gateway-cli 状态/重载全失效；#4 include_usage 被忽略；#5 [DONE] 后有 trailing SSE；#14 infinite_on_error 必败请求无限重试 + attempt=N/0 显示 |
| P2 正确性/协议 | 5 | #1 Anthropic 丢 text；#2/#6 bridge 模型名不一致；#7 错误格式不符 OpenAI；#8 透传错误泄露 provider 名 + 无本地必填校验 |
| P3 次要/打磨 | 3 | #3 reasoning_content 直通未归一化；#10 工具 schema 校验过松；#11 /health、/ready 返回 404 |

两个 P0 的核心结论先放在前面：

- #9：数据面（127.0.0.1:18080）对 /v1/chat/completions、/v1/models、Anthropic /v1/messages 完全不校验 Authorization。无头、假头、空 Bearer 均可拿到 200 且正常出补全。控制面有 ADMIN_BOOTSTRAP_TOKEN，数据面却零鉴权，任何人能白嫖上游配额。
- #13：SSRF 守卫在请求路径会拒绝 localhost/私有 IP（err=localhost access not allowed / private IP access not allowed: 10.x），但健康探测路径不经过守卫，照样拨号 localhost 成功并标记 provider 健康。后果：一个被守卫挡死的本地上游会显示"健康"却永远无法服务，且叠加 #14 会无限重试挂死约 90s。这是最危险的"静默故障"模式。

---

## 2. 测试方法学 / Methodology

为保证结论可独立复现，我在仓库内搭了一套可复用的功率脚手架（见末节"可复用测试脚手架"），分三阶段：

- Phase-1（10 条核心请求，t1-t10）：覆盖 OpenAI Chat / Responses / Anthropic / 流式 / 工具 / 桥接。10/10 HTTP 200，从响应体中定位语义 bug（#1、#3）。
- Phase-2（s1-s3 流式 + e1-e8 错误路径 + a1-a4 鉴权 + g1-g4 健康端点）：定位协议合规与鉴权 bug（#4、#5、#6、#7、#8、#9、#10、#11）。
- Phase-3（SSRF + 重试）：起一个双栈 mock OpenAI 服务（mock-ssrf-server.py，[::]:9999），向网关注入指向 localhost/127.0.0.1/私有 IP 的 provider，比对 mock 日志与 gatewayd.log，定位 #13、#14。

所有响应体落盘于 codex-test/out/*.body，日志索引到行号。gateway-cli.exe 状态类命令整族失效（#12），故运维面一律改用原始 HTTP。

---

## 3. 缺陷清单 / Bug List

### P0 - 阻断上线/安全

#### #9 数据面完全无鉴权 / Data plane is fully unauthenticated

- 严重级别: P0（安全）
- 复现: 不带任何 Authorization 头向数据面发 POST /v1/chat/completions（a3），以及发"假 token"（a2）、空 Bearer（a4），均返回 200 并正常产出补全；/v1/models 同样无鉴权可拉（a1）。
- 期望: 缺失/无效凭证应返回 401；数据面应校验 ADMIN_TOKEN/VIEWER_TOKEN 等。
- 实际: 全部 200。a3 返回完整 chat.completion（model:"deepseek-v4-flash"，含 reasoning_content）；a4 空 Bearer 同样 200。
- 证据: codex-test/out/a3-chat-no-auth.body、codex-test/out/a4-chat-empty-bearer.body、codex-test/out/a1-models-no-auth.body
- 修复建议: 数据面路由接入与控制面同源的 token 校验中间件；区分 ADMIN_TOKEN（写）与 VIEWER_TOKEN（读/补全）。对未携带/不匹配凭证返回 401 且不走上游，避免配额被白嫖与上游成本泄漏。

#### #13 SSRF 守卫误报阻断 localhost/LAN 上游 + 健康/请求路径不一致

- 严重级别: P0（静默故障 / 可用性）
- 复现: 向配置注入三个 provider，分别指向 http://localhost:9999、http://127.0.0.1:9999、http://10.255.255.1:9999，并启动双栈 mock 服务收包。
  - 健康探测：成功拨号 localhost——mock 日志每 ~30s 收到 GET /v1/models from ::1 与 from ::ffff:127.0.0.1，无守卫拦截。
  - 请求路径：发 POST /v1/chat/completions 到这些模型，gatewayd.log 记 upstream error: status=502 err=localhost access not allowed（L729/L736/L751），mock 从未收到 POST /v1/chat/completions——守卫在转发前就拦截。
  - 历史日志亦有 private IP access not allowed: 10.240.53.160（L323 一带）佐证对私有网段的拦截。
- 期望: 健康探测与请求路径一致；要么都走守卫，要么提供显式 allowlist（如 ssrf.allow: [127.0.0.0/8, 10.0.0.0/8]）让自托管/内网模型可用且健康态真实。
- 实际: provider 显示"健康"却永不服务；运维无任何告警。二进制串确认守卫句子集合："localhost access not allowed"、"private IP access not allowed: %s"、"user info in URL not allowed"、"hex-encoded host not allowed"，且无 allowlist 出口。
- 证据: codex-test/out/mock-ssrf.log（仅 GET /v1/models，无 POST）；gatewayd.log L723-L751 重试序列；codex-test/out/ssrf3-chat-private.body 返回 {"error":"no provider available"}
- 修复建议: (1) 健康探测与请求路径共用同一 SSRF 检查器，杜绝"健康却不可服务"；(2) 增配 allowlist/unsafe_internal 开关，支持内网/loopback 上游（自托管模型是常见用例）；(3) provider 健康态应由"探测可达 且 至少一次请求/握手成功"复合判定。

### P1 - 发布前必修

#### #12 gateway-cli.exe 状态/重载命令全失效（uptime 类型不匹配）

- 严重级别: P1（阻断运维/自动化）
- 复现: bin\gateway-cli.exe status 与 bin\gateway-cli.exe provider list 均报错退出。
- 期望: CLI 能反映网关状态、列出/重载 provider。
- 实际: 二者皆报 Error: failed to get status: unmarshal response: json: cannot unmarshal number into Go struct field GatewayStatusResponse.gateway.uptime of type string。控制面把 uptime 以数字返回，CLI 却按字符串解析。受影响命令族：status、provider list、reload、runtime status。仅 validate <file>、publish history 可用。
- 证据: 实测命令输出（gateway-cli.exe status/provider list stderr）；gatewayd.exe 无 uptime 命中，说明该结构在 CLI 端，类型契约与控制面不一致。
- 修复建议: 统一 GatewayStatusResponse.gateway.uptime 的类型契约（端侧用 int64/duration，CLI 用 json.Number 或数字类型），并用契约测试钉住。

#### #4 stream_options.include_usage: false 被忽略

- 严重级别: P1（协议合规）
- 复现: s3 流式请求显式 "stream_options":{"include_usage":false}，仍发结尾 usage 字段。
- 期望: include_usage:false 时结尾不带 usage chunk；按 OpenAI 规范，usage chunk 仅在 include_usage:true 时出现。
- 实际: 流末尾仍发 ...,"finish_reason":"length","usage":{...prompt_tokens...} chunk。二进制串确认 includeUsage/include_usage 零命中——该字段从未被读取。
- 证据: codex-test/out/s3-stream-no-usage.body（结尾含 usage 的 chunk）
- 修复建议: 在流式分支读取 stream_options.include_usage，为 false 时不注入 usage chunk；include_usage:true 时再按规范发一个 choices:[] 的 usage 帧。

#### #5 [DONE] 之后有 trailing SSE 事件

- 严重级别: P1（协议合规）
- 复现: 每条流都以 data: [DONE] 结束后又跟一行 data: {"choices":[],"cost":"0"}。
- 期望: [DONE] 是 SSE 终止哨兵，其后不应再有任何 data: 事件。
- 实际: 严格的 SSE 解析器会继续解析该帧，可能把无 choices 的帧当作新事件，导致消费方卡住或重复处理。[DONE] 在二进制中命中 4 次，疑似多处写入路径都补了这个 trailing 帧。
- 证据: codex-test/out/s3-stream-no-usage.body 末尾；s2 流同样以 data: {"choices":[],"cost":"0"} 收尾。
- 修复建议: 写完 [DONE]\n\n 立即 flush 并关闭流，禁止其后任何 data: 帧；若需带 cost，并入 usage chunk（#4 之上）而非独立 trailing 帧。

#### #14 infinite_on_error 对必败请求无限重试 + attempt=N/0 显示缺陷

- 严重级别: P1（自我 DoS / 客户端长挂）
- 复现: Phase-3 对 SSRF 必被拦的 localhost 模型发请求，gatewayd.log 记到多达 6 次重试、指数退避（~3s->6s->12s->24s->45s），从 23:56:30 到 23:58:00 共约 90s 后才回 502 no provider available。更早一次 5 次重试约 35s（23:53:02->23:53:37）。
- 期望: max_retries 应是硬上限；infinite_on_error:true 与有限 max_retries 自相矛盾的配置应在加载期被拒绝或规整。
- 实际: 实测运行配置 retry.infinite_on_error: true + all_errors: false，重试按计数看"不封顶"，仅由退避/超时预算自然终止，客户端因此长挂约 90s。日志分母为 attempt=%d/%d 第二参数即 max_retries 的字面值——曾为 0（attempt=1/0...6/0），回滚到干净 revision 后为 max_retries: 2（仍带 infinite_on_error: true，矛盾依旧）。
- 证据: gatewayd.log L723-L751（6 次重试序列）；二进制含 InfiniteOnError / yaml:"infinite_on_error" / MaxRetries / attempt=%d/%d。
- 修复建议: (1) 配置校验期拒绝 infinite_on_error:true 与 max_retries 有限值并存，或二者择一；(2) 即使无限重试，也须有请求级总时限（如 request_timeout_ms）硬截断并明确回错；(3) attempt=N/0 分母在 infinite_on_error 时应显示 inf 或省略，避免 ops 误读。

### P2 - 正确性/协议

#### #1 Anthropic 适配器丢弃 text 字段

- 严重级别: P2（Anthropic SDK 兼容）
- 复现: t8 走 /v1/messages，响应 content":[{"type":"text"}]——缺少 "text":"..."。
- 期望: Anthropic messages 响应的 content[].text 必填。
- 实际: 块只有 type 无 text，Anthropic SDK 取不到正文。finish_reason/stop_reason 为 max_tokens，说明上游实际有输出但被适配层抹掉。
- 证据: codex-test/out/t8-anthropic-messages.body
- 修复建议: 适配器在构造 Anthropic content 块时务必回填 text；流式 delta 同理补 text。

#### #2 Bridge 模型名不一致（chat vs responses）

- 严重级别: P2（契约/计费一致性）
- 复现: compat.bridge 把 gpt-5.4-mini 映射到 deepseek-v4-flash。
  - chat 桥接 t3：响应 model:"deepseek-v4-flash"（泄露上游名，而非请求的 gpt-5.4-mini）。
  - responses 桥接 t5：响应 model:"gpt-5.4-mini"（正确改写）。
- 期望: 桥接后始终把 model 改写为客户端请求的别名，两协议一致。
- 实际: chat 路径保留上游名，responses 路径改写——同一桥接行为取决于入口协议。
- 证据: codex-test/out/t3-bridge-gpt5.4-mini.body（model:"deepseek-v4-flash"）对比 codex-test/out/t5-responses-bridge.body（model:"gpt-5.4-mini"）
- 修复建议: 在 bridge 层统一改写出口 model 为请求别名（chat 与 responses、流式与非流式共用一条路径）。

#### #6 Bridge 模型名在流式中未改写

- 严重级别: P2（与 #2 同源）
- 复现: s2 chat 流式桥接，每个 chunk 的 model 都是 deepseek-v4-flash（上游名）。
- 期望: 流式 chunk 的 model 应为请求别名 gpt-5.4-mini。
- 实际: 非流式 t3、流式 s2 均未改写；唯 responses 路径 t5 改写。
- 证据: codex-test/out/s2-stream-bridge-usage.body（首个 chunk 即 model":"deepseek-v4-flash"）
- 修复建议: 与 #2 同一治理点：bridge 出口统一改写所有支线的 model。

#### #7 网关自有错误不符合 OpenAI 错误格式

- 严重级别: P2（SDK 错误解析）
- 复现: 网关自身错误返回裸字符串 {"error":"..."}：
  - 未知模型 -> {"error":"model not found: does-not-exist"}（e1）
  - 缺 model -> {"error":"model is required"}（e5）
  - 非法 JSON / 空体 -> {"error":"invalid request body"}（e2/e3）
  - 无可用 provider -> {"error":"no provider available"}（ssrf3）
- 期望: OpenAI 规范为 {"error":{"message":..,"type":..,"param":..,"code":..}}。
- 实际: 裸字符串形态，OpenAI SDK err.Error() 能拿到字符串，但按结构解析（code/type）会失败；与上游透传错误（e4 是结构化的）并存，格式不统一。
- 证据: codex-test/out/e1-unknown-model.body、codex-test/out/e5-missing-model.body、codex-test/out/ssrf3-chat-private.body
- 修复建议: 统一错误构造器，所有网关自有错误走结构包装；与上游透传错误的 schema 对齐。

#### #8 透传错误泄露上游 provider 名 + 缺本地必填校验

- 严重级别: P2（信息泄漏 + 资源浪费）
- 复现: e4 请求缺少 messages 字段，返回 {"error":{"message":"Error from provider (DeepSeek): Failed to deserialize the JSON body into the target type: missing field 'messages' ..."}}，耗时 716ms。
- 期望: (1) messages 是 OpenAI 必填，应在网关本地先 400 拒绝，不发上游；(2) 错误文案不应暴露后端 provider 名 DeepSeek——网关本应屏蔽后端拓扑。
- 实际: 无效请求被透传到上游（716ms 证实真实网络往返），上游 400 文案被原样包进 message，并冠以 Error from provider (DeepSeek):，泄露后端身份。
- 证据: codex-test/out/e4-missing-messages.body
- 修复建议: 网关入口做 OpenAI 最小必填校验（model、messages）；透传错误统一剥离 provider 名前缀，按 #7 结构化包装。

### P3 - 次要/打磨

#### #3 reasoning_content 直通、content 为空且未归一化

- 严重级别: P3（语义/客户端体验）
- 复现: 走 deepseek 的 t1/t3 等，响应 content:""，全部输出在非标准 reasoning_content，且 finish_reason:"length"。
- 期望: 对外宣称 OpenAI 兼容的 chat，应把可见答案放入 content；或文档明确说明 deepseek 走透传且 reasoning_content 为主输出字段。
- 实际: 用 OpenAI SDK 读 choices[].message.content 的客户端会拿到空串，"答案丢失"。
- 证据: codex-test/out/t3-bridge-gpt5.4-mini.body（content:""、reasoning_content:"..."）
- 修复建议: 要么按上游语义把 reasoning_content 合入 content（或经配置开关），要么在文档/模型清单标注该字段的透传语义。

#### #10 工具 schema 校验过松

- 严重级别: P3（健壮性）
- 复现: e8 带 tool 但缺 parameters 字段，仍 200 并发了补全。
- 期望: 工具定义缺 parameters 应 400（OpenAI 工具 schema 要求 parameters 为对象，可空对象）。
- 实际: 被接纳并透传上游，上游自行解释。
- 证据: codex-test/out/e8-tool-bad-schema.body（200，完整补全）
- 修复建议: 入口校验工具 parameters 必须为对象（可为 {}），缺失/类型错即 400。

#### #11 /health 与 /ready 返回 404（仅 /-/health 可用）

- 严重级别: P3（运维约定）
- 复现: /health、/ready、根 / 均回 404 page not found；唯 /-/health 返回 200 与 {"status":"healthy","version":"1.4.4",...}。
- 期望: /health、/ready 是 K8s/负载均衡通用探针路径，应可用或显式跳转。
- 实际: 通用探针报 404，需运维改成自定义路径 /-/health，违反"开箱即用"约定。
- 证据: codex-test/out/g1-health.body、codex-test/out/g2-ready.body、codex-test/out/health.first
- 修复建议: 至少对 /health 与 /ready 返回 200/503 语义（不搞性能差异时可与 /-/health 同源），兼容标准探针。

#### #15（附录，历史佐证）上游非 2xx 时日志 err=<nil>

- 严重级别: P3（可观测性）
- 复现: 历史日志 gatewayd.log 多处 upstream error: status=400 err=<nil>（如 L76/L80 一带），status 非 2xx 却 err=<nil>。
- 期望: 非 2xx 应带上游错误体/原因，便于排查。
- 实际: 错误详情被吞，只剩状态码。Phase-3 未在本会话单独复现，列为历史待验证项。
- 修复建议: 解析上游响应体并入日志/错误返回，杜绝 err=<nil> 与非 2xx 并存的"信息丢失"。

---

## 4. 运行时与控制面观测 / Runtime Notes

- 三面拓扑: gatewayd（数据 127.0.0.1:18080）、controld（控制 127.0.0.1:18081，需 Authorization: Bearer $ADMIN_BOOTSTRAP_TOKEN）、telemetryd（IPC）。控制面会话 {"authenticated":true,"name":"bootstrap","role":"admin"}。
- 可用控制面端点: /api/admin/{session,config,config/history,runtime/status,diagnostics,audit,overview,status,benchmark/baselines,benchmark/runs,update/status} 均可读；config/publish、config/rollback、config/update、config/validate 可写。/update/status 显示 current_version:1.4.4，仓库 SSC-STUDIO/Ai-Model-Gateway。
- 405/404: /probe(404)、/runtime/preflight(405)、/client-error(405)、/pricing/refresh(405)、/update/check(405)。
- /api/admin/overview 观测异常（补充）: 抓取快照中 Runtime.ProviderCount:0、HealthEnabled:false、BridgeEnabled:false，但实际有 2 个活跃 provider、健康探测每 30s 在跑、bridging 工作中（gpt-5.4-mini->deepseek-v4-flash）。疑为 overview 未聚合数据面实时态，建议作为独立可观测性 bug 跟进（未计入 14 项主清单）。
- 当前配置: 已回滚到干净 revision rev_4d6dc684b38f679ac68e8d11，仅 nowcoding+opencode-deepseek，/v1/models 共 9 个模型、无 ssrf-*，configs/config.yaml 无 ssrf 字串。运行时四进程仍在运行。

---

## 5. 可复用测试脚手架 / Reusable Test Scaffold

全部位于 codex-test/，可独立重跑：

- codex-test/start-runtime.ps1 — 启动 aigw/gatewayd/controld/telemetryd 并等 /-/health。
- codex-test/run-tests.ps1（Phase-1，t1-t10）、codex-test/run-tests-2.ps1（Phase-2，s/e/a/g）、codex-test/run-tests-3.ps1（Phase-3，SSRF+重试）。
- codex-test/mock-ssrf-server.py — 双栈 mock OpenAI 服务，用以区分健康探测与请求路径是否过 SSRF 守卫。
- codex-test/inspect_bin2.py — 在 gatewayd.exe 中取证错误串/字段/格式串。
- 证据体: codex-test/out/{t1-t10,s1-s3,e1-e8,a1-a4,g1-g4,ssrf1-ssrf5,admin-*}.body，结果摘要 out/results.md、out/results-2.md。

重跑提示: 因网关为闭源二进制，无单测可挂；上述脚本是当前唯一可靠的回归手段。建议把这些脚本沉淀进 CI（可选起本地 mock，断言响应体关键字段），把 #1/#5/#6/#7/#9 等做成回归闸门。

---

## 6. 配置侧遗留矛盾 / Config Caveats

干净 revision 的重试段仍自相矛盾，建议修配置层：

    retry:
      infinite_on_error: true   # 无限重试（按计数不限）
      all_errors: false          # 仅部分错误重试
    max_retries: 2               # 与 infinite_on_error:true 矛盾

infinite_on_error:true 使 max_retries 失效；all_errors:false 决定只对部分错误重试，但与"无限"叠加语义模糊。三选一厘清语义、并在加载期校验。

---

## 7. 收尾建议 / Next Steps

1. 先修两个 P0：数据面鉴权（#9）与 SSRF 守卫一致性+allowlist（#13）——上线前阻断项。
2. 修 P1 中影响客户端与运维的四项：CLI 类型契约（#12）、include_usage（#4）、[DONE] trailing（#5）、重试上限/显示（#14）。
3. 协议合规与一致性两批：anthropic text（#1）、bridge 模型名统一改写（#2/#6）、错误结构化（#7）、provider 名脱敏+本地校验（#8）。
4. 把 codex-test/ 脚本固化成回归集与 CI 闸门，堵住"测试方法有限"的根因。

测试运行时仍在运行（未关闭）；如需我停掉进程、修代码（若有源码开放）或针对某条 P0/P1 出具体补丁思路，告知即可。
