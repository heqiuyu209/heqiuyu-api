# 第 8 步验收报告：注册防御测试与全量验收

> 负责人：step8-qa（T8 / R2-TESTS / INV-S3）｜首版：2026-10-24（第 5–8 步）｜R2 修复轮 2 更新：2026-10-24｜INV-S3 邀请码注册更新：2026-10-25｜依据：`.agent-teams/registration-defense-plan.md`、`.agent-teams/fix-round2-plan.md`、`.agent-teams/invite-code-plan.md`（用户已批准）
>
> 结论：**Go 测试、`go build`、`go vet`、`gofmt -l`、前端等价 typecheck、`npm run lint` 全部通过**；
> `npm run build` 按 Lead 指示不由 T8 执行（T7 负责发布构建链路）。R2 修复轮 2 的新增/更新测试与残余风险见 §8。本文档只记录测试与验收，不含产品代码改动。

## 1. 交付的测试文件

| 文件 | 类型 | 用例数 |
|---|---|---|
| `common/rate_limit_test.go` | 新增（R2；含 `common.MaskEmail` 单测） | 5 |
| `service/registration_guard_test.go` | 新增（R2 追加 2） | 15 |
| `service/mail_queue_test.go` | 新增（R2 重写 1、追加 4） | 8 |
| `service/registration_token_test.go` | 新增（R2 追加 1） | 9 |
| `middleware/turnstile-check_test.go` | 新增（R2 改写 1 为 2） | 11（其中 `TestTurnstileSessionMarkerExpiry` 含 4 个子用例） |
| `middleware/email-send-rate-limit_test.go` | 新增 | 7 |
| `router/registration_defense_e2e_test.go` | 新增（R2 追加 3） | 12 |
| `router/registration_body_limit_test.go` | 新增（R2） | 2 |
| `model/user_email_unique_test.go` | 新增（R2 收尾） | 3（其中历史重复用例含 2 子用例） |
| `router/registration_invite_e2e_test.go` | 新增（INV-S3） | 6（含 lockout 3 子用例） |
| `service/invite_code_test.go` | 新增（INV-S3） | 4 |
| `middleware/logger_test.go` | 新增（INV-S3，invite_code 日志脱敏） | 2 |
| `controller/invite_code_test.go` | 新增（INV-S3） | 5 |
| `controller/login_session_test.go` | 更新（INV-S3 追加 6 个 OAuth mock 用例，含 aff/inviter_id；保留既有 3 个） | 既有用例保持 |
| `router/user_self_update_e2e_test.go` | 更新（仅测试初始化加 guard/queue 重置，保留既有 hunk） | 既有 3 个用例保持 |
| `docs/registration-defense-acceptance.md` | 新增/更新（本文件） | — |

测试统一使用内存 SQLite / httptest / 假发信函数，**不写 `heqiuyu-api.db`、不改线上 options、不连接真实 SMTP、不修改产品代码**。

## 2. 环境与门禁命令（§6.1）

| 命令 | 结果 | 说明 |
|---|---|---|
| `$env:GOCACHE/$env:GOTMPDIR` 指向 `%TEMP%` | — | 沙箱默认 GOCACHE 不可写，按 §6.1 统一处理 |
| `go build ./...` | **exit 0** | stderr 中 GOMODCACHE `writing stat cache ... Access is denied` 为良性噪音，以 exit code 为准 |
| `go vet ./...` | **exit 0** | vet 无告警输出（stderr 可能含 GOMODCACHE 的 `writing stat cache ... Access is denied` 良性噪音，同 :28/:163；以 exit code 为准） |
| `go test -count=1 ./common/... ./setting/... ./service/... ./middleware/... ./controller/... ./router/... ./model/...` | **exit 0** | 见 §4 分包结果 |
| `gofmt -l`（本次改动全部 Go 文件） | **空输出** | 工作区 `Everyone DELETE DENY` 导致 `gofmt -w` 写文件被拒，改用 `gofmt -l` 复核；本次文件均无需格式化 |
| `node node_modules/typescript/bin/tsc -p tsconfig.app.json --noEmit --tsBuildInfoFile "$env:TEMP\tsc-app-step8-qa.tsbuildinfo"` | **exit 0** | `npm run typecheck` 在本沙箱写 `node_modules/.tmp/*.tsbuildinfo` 会 EPERM，按 §6.1 使用等价命令 |
| `node node_modules/typescript/bin/tsc -p tsconfig.node.json --noEmit --tsBuildInfoFile "$env:TEMP\tsc-node-step8-qa.tsbuildinfo"` | **exit 0** | 同上 |
| `cd web/default && npm run lint` | **exit 0** | eslint 无告警 |
| `npm run build` | 未执行（T8） | Lead 指示：发布构建验证由 T7 负责，且沙箱可能拒绝写 `web/default/dist` |

Go 版本：`go1.27.2 windows/amd64`；Node：`v24.20.0`；npm：`11.19.0`。

## 3. 用例矩阵（用例 → 验收标准 → 结果 → 证据）

### 3.1 service/registration_guard_test.go（限流/发码/验证码锁定）

| # | 用例 | 覆盖 | 结果 | 证据（关键断言） |
|---|---|---|---|---|
| G1 | `TestRegistrationGuardAttemptWindow` | §7-1 单 IP 第 11 次 429 | PASS | 前 10 次 `Allowed=true`；第 11 次 `Allowed=false, Reason=attempt_window, RetryAfter∈(0,600]`；不同 IP 放行 |
| G2 | `TestRegistrationGuardIPHourlySuccessLimit` | 单 IP 每小时成功上限 | PASS | 记录 2 次成功后下一次拒绝，`Reason=ip_success_hour` |
| G3 | `TestRegistrationGuardIPDailySuccessLimit` | 单 IP 每日成功上限 | PASS | 记录 2 次成功后下一次拒绝，`Reason=ip_success_day` |
| G4 | `TestRegistrationGuardGlobalHourlySuccessLimit` | 全站每小时成功上限（0=关闭） | PASS | 全局 2 次后第 3 个 IP 被拒 `global_success_hour`；配置 0 时恢复放行 |
| G5 | `TestRegistrationGuardAllowlist` | CIDR/单 IP 白名单永久放行 | PASS | `10.0.0.0/8` 下连续 10 次尝试全部放行（即使 `ip_max_attempts_per_window=2`）；非法条目被忽略（不产生放行）；非白名单 IP 第 3 次被拒（窗口上限 2）。注：白名单 IP 的成功计数是否累计属实现内部行为，本用例未直接断言 |
| G6 | `TestRegistrationGuardDisabled` | 总开关关闭回滚路径 | PASS | `enabled=false` 时全部放行且不累计计数；重新开启后上限立即生效 |
| G7 | `TestRegistrationGuardHotReload` | 配置热读取 | PASS | 上限 2 时第 3 次被拒；热调到 5 后第 3–5 次立即放行、第 6 次被拒（无需重置状态） |
| G8 | `TestRegistrationGuardEmailCooldown` | 同邮箱发码冷却（60s） | PASS | 首次放行；大小写/空格规范化后二次发码被拒 `email_cooldown`；不同邮箱、不同 purpose 放行；空邮箱放行 |
| G9 | `TestRegistrationGuardEmailDailyLimit` | 同邮箱每日发码上限 | PASS | `email_max_per_day=2` 时第 3 次拒绝 `email_day` |
| G10 | `TestRegistrationGuardEmailIPHourlyLimit` | 单 IP 每小时发码上限 | PASS | `email_ip_max_per_hour=1` 时同 IP 第 2 次拒绝 `email_ip_hour`，换 IP 放行 |
| G11 | `TestRegistrationGuardEmailGlobalHourlyLimit` | 全站发码预算（保护 SMTP） | PASS | `email_global_max_per_hour=1` 时第 2 个 IP/邮箱被拒 `email_global_hour` |
| G12 | `TestRegistrationGuardVerificationLockoutAndClear` | 验证码 5 次锁定 + 清除 | PASS | 前 4 次不锁定、第 5 次 `locked=true, retryAfter∈(0,600]`；`ClearVerificationFailures` 后计数清零；purpose `v`/`r` 隔离；大小写规范化命中同一计数 |
| G13 | `TestRegistrationGuardRejectsEmptyIdentifiers` | 空 IP/空邮箱不误伤 | PASS | 空标识放行，交给 controller 校验 |

### 3.2 service/mail_queue_test.go（异步邮件）

| # | 用例 | 覆盖 | 结果 | 证据（关键断言） |
|---|---|---|---|---|
| M1 | `TestEnqueueEmailSyncFallbackWhenAsyncDisabled` | `async_email_enabled=false` 回滚路径 | PASS | 同步调用假 sender 且错误原样返回（controller 据此删码返回 503） |
| M2 | `TestMailQueueRateLimit` | 全站发信速率（R2 语义变更） | PASS | 速率槽在 worker 真实发送前获取：前 2 封发送后第 3 封不立即发送（worker 等下一窗口），窗口满后新入队请求返回 `ErrMailRateLimited`。详见 §8 R2-M1 |
| M3 | `TestMailQueueFull` | 有界队列容量（重启生效） | PASS | 队列 1/worker 1，阻塞 sender 下第 3 封返回 `ErrMailQueueFull` |
| M4 | `TestMailBreakerOpensAfterConsecutiveFailures` | 失败重试 + 连续失败熔断 + 重置 | PASS | 首封失败后间隔 2s 重试 1 次（假 sender **至少被调用 2 次**：首次 + 重试 1 次）；`async_email_breaker_failures=1` 时下一次 `ErrMailBreakerOpen`；`ResetMailQueueForTests()` 后恢复发送 |

### 3.3 service/registration_token_test.go（表单令牌）

| # | 用例 | 覆盖 | 结果 | 证据（关键断言） |
|---|---|---|---|---|
| T1 | `TestRegistrationFormTokenDisabled` | 关闭时禁用 | PASS | `form_token_enabled=false` 或 `enabled=false` → `ErrFormTokenDisabled`（issue/consume） |
| T2 | `TestIssueRegistrationFormToken` | 签发契约 | PASS | `v1:<issuedAt>:<nonce>:<hmac>` 4 段；`min_seconds`/`expires_in` 与配置一致；HMAC 校验通过 |
| T3 | `TestConsumeRegistrationFormTokenOnce` | 一次性消费 | PASS | 首次成功，第二次 `ErrFormTokenReused` |
| T4 | `TestConsumeRegistrationFormTokenSignature` | 签名篡改 | PASS | 篡改签名/nonce → `ErrFormTokenInvalid`，且不影响原令牌消费 |
| T5 | `TestConsumeRegistrationFormTokenUnknownNonce` | 未签发 nonce | PASS | 签名合法但 nonce 未知 → `ErrFormTokenInvalid` |
| T6 | `TestConsumeRegistrationFormTokenMinSeconds` | 最短填写时间 | PASS | 2 秒时 `ErrFormTokenInvalid`，达到 `form_token_min_seconds` 后可消费 |
| T7 | `TestConsumeRegistrationFormTokenExpiry` | 有效期/未来时间戳 | PASS | 超过 `form_token_max_minutes` → `ErrFormTokenExpired`；未来时间戳 → `ErrFormTokenInvalid` |
| T8 | `TestConsumeRegistrationFormTokenMalformed` | 格式非法 | PASS | 8 种非法 token 均 `ErrFormTokenInvalid` |
| T9 | （含于 T1）`RegistrationFormTokenEnabled` | 生效值 helper | PASS | 任一开关关闭时为 false |

### 3.4 middleware/turnstile-check_test.go（Turnstile strict）

| # | 用例 | 覆盖 | 结果 | 证据（关键断言） |
|---|---|---|---|---|
| C1 | `TestTurnstileStrictNoTokenRejected` | 无 token 拒绝 | PASS | 返回 `Turnstile token 为空`，handler 未执行 |
| C2 | `TestTurnstileStrictFailedVerificationRejected` | siteverify 失败拒绝 | PASS | `TurnstileVerifyURL` 指向 httptest，`success:false` 时返回统一失败文案；请求携带 secret/response/remoteip |
| C3 | `TestTurnstileStrictValidTokenAllowed` | 成功放行 | PASS | 合法 token → handler 执行；响应无 `Set-Cookie`（strict 不写 session） |
| C4 | `TestTurnstileStrictIgnoresSessionMarker` | strict 不读 session | PASS | 携带 30 分钟内有效会话标记仍因缺 token 被拒；带合法 token 时 siteverify 恰好被调用 1 次 |
| C5 | `TestTurnstileSessionMarkerExpiry`（4 子用例） | 旧会话标记过期（L5） | PASS | `fresh timestamp` 可复用；`expired timestamp` 被拒；旧 `bool true` 被视为过期；fresh+新 token 放行 |
| C6 | `TestTurnstileStrictDecoupledFromRegistrationEnabled` / `TestTurnstileStrictDisabledFallsBackToSession` | strict 与 enabled 解耦（R2-2） | PASS | `enabled=false + turnstile_strict=true` 时仍每次调用 siteverify、不读 session；只有 `turnstile_strict=false` 才退回会话复用（无会话标记时仍需 token） |
| C7 | `TestTurnstileVerifyTimeout` | siteverify 超时 | PASS | 服务端延迟 6s，请求约 5s 超时后拒绝（耗时 4s–10s 区间断言） |
| C8 | `TestTurnstileDisabledPassThrough` | Turnstile 总开关关闭 | PASS | `common.TurnstileCheckEnabled=false` 直接放行 |
| C9 | `TestTurnstileVerifyURLEmpty` | 校验地址未配置 | PASS | 返回失败且不发起请求 |
| C10 | `TestTurnstileSiteverifyResponseIsJSON` | 非 JSON 响应 | PASS | 按失败处理，不 panic |

### 3.5 middleware/email-send-rate-limit_test.go（发码限流中间件）

| # | 用例 | 覆盖 | 结果 | 证据（关键断言） |
|---|---|---|---|---|
| E1 | `TestEmailSendBurstPerIPLimit` | 原 30s/2 次短窗语义 | PASS | 同 IP 第 3 次 429 + 有效 `Retry-After` + `success:false`；换 IP 放行 |
| E2 | `TestEmailSendCooldownGuard` | 同邮箱冷却 | PASS | 二次发码 429 + `Retry-After`；文案经 i18n 翻译而非原始 key |
| E3 | `TestEmailSendMissingEmailPassesThrough` | 缺参数不拦截 | PASS | 放行给 controller 返回既有参数错误 |
| E4 | `TestEmailSendDailyLimitGuard` | 同邮箱日上限（跨 IP） | PASS | 第 3 次 429 + `Retry-After` |
| E5 | `TestEmailSendIPHourlyLimitGuard` | 单 IP 小时上限 | PASS | 第 2 次 429 |
| E6 | `TestEmailSendGlobalHourlyLimitGuard` | 全站预算 | PASS | 第 2 个 IP/邮箱 429 |
| E7 | `TestEmailSendPurposeIsolation` | purpose 由路径推导 | PASS | `/api/verification`(“v”) 与 `/api/reset_password`(“r”) 冷却键独立；同 purpose 二次仍 429 |

### 3.6 router/registration_defense_e2e_test.go（端到端）

环境：`common.RedisEnabled=false`（在 `SetApiRouter` 之前设置）、`TurnstileCheckEnabled=false`、真实 `SetApiRouter` + 内存 SQLite、每用例 `ResetRegistrationGuardForTests()`。

| # | 用例 | 覆盖 | 结果 | 证据（关键断言） |
|---|---|---|---|---|
| R1 | `TestRegistrationRateLimitE2E11thAttempt` | §7-1 第 11 次 429 | PASS | 同 IP 前 10 次非 429；第 11 次 **429 + Retry-After∈(0,600]** + `success:false`；不同 IP 放行 |
| R2 | `TestRegistrationSuccessRecordsCountersE2E` | 成功注册回写计数 | PASS | 首次注册 `success:true` 且落库 1 条；`ip_max_success_per_hour=1` 时同 IP 再次 429；不同 IP 成功 |
| R3 | `TestRegistrationHoneypotE2E` | 蜜罐拒绝 | PASS | `website` 非空 → **400** + `success:false` + i18n 文案且不落库；`website` 为空正常注册 |
| R4 | `TestRegistrationFormTokenDisabledE2E` | form_token 关闭 | PASS | 注册无需 token；`GET /api/user/register/token` → **404** |
| R5 | `TestRegistrationFormTokenEnabledE2E` | form_token 开启 | PASS | 取 token 200（token/min_seconds/expires_in=1800）；缺 token→400；篡改→400；合法 token 注册成功；**重放→400**；`enabled=false` 时取 token→404 |
| R6 | `TestRegistrationVerificationLockoutE2E` | 验证码 5 次锁定 | PASS | 前 4 次非 429；第 5 次 **429 + Retry-After**；锁定后验证码被删除（正确码也不可用） |
| R7 | `TestRegistrationSuccessDeletesVerificationCodeE2E` | 成功注册清码 | PASS | 正确码注册成功；`VerifyCodeWithKey` 变 false；同码重放不成功 |
| R8 | `TestVerificationEmailFailureDeletesCodeE2E` | 先存码后入队 / 失败删码 | PASS | 同步假 sender 能从邮件内容读到验证码（证明先存码后入队）；发送失败 → **503** 且码被删；发送成功 → 200 且码保留 |
| R9 | `TestStatusIncludesRegistrationFieldsE2E` | `/api/status` 5 字段 | PASS | 5 个字段均存在；`form_token_enabled`/`honeypot_enabled` 热变更立即反映；`RegisterEnabled=false` 时 3 个 register 字段为 false |

### 3.7 router/user_self_update_e2e_test.go（既有测试更新）

- 在 `TestRegisterRejectsEmptyUsername` 初始化处新增 `service.ResetRegistrationGuardForTests()` 与 `service.ResetMailQueueForTests()`（保留既有 hunk 不动），保证与 `registration_defense_e2e_test.go` 同包运行时“默认阈值下单次注册通过”。
- 既有 3 个用例（改密保用户名、管理员空用户名防护、空用户名注册拒绝）在 `go test ./router/` 中全部通过。

## 4. 全量测试结果（计划 §6 命令）

```text
go test -count=1 ./common/... ./setting/... ./service/... ./middleware/... ./controller/... ./router/... ./model/...
ok  github.com/heqiuyu/heqiuyu-api/common                          2.784s
ok  github.com/heqiuyu/heqiuyu-api/setting/config                1.866s
ok  github.com/heqiuyu/heqiuyu-api/setting/model_setting         2.470s
ok  github.com/heqiuyu/heqiuyu-api/setting/operation_setting     2.474s
ok  github.com/heqiuyu/heqiuyu-api/service                       8.101s
ok  github.com/heqiuyu/heqiuyu-api/middleware                   11.863s
ok  github.com/heqiuyu/heqiuyu-api/controller                    5.396s
ok  github.com/heqiuyu/heqiuyu-api/router                        8.038s
ok  github.com/heqiuyu/heqiuyu-api/model                         4.338s
FULL_TEST_EXIT=0
```

## 5. 验收标准（计划 §7）对照

| 验收标准 | 覆盖用例 | 结果 |
|---|---|---|
| 单 IP 第 11 次注册 429 且带 Retry-After；成功注册受小时/日上限约束 | R1、R2、G1–G4 | PASS |
| Turnstile 开启后无 token/重放被拒；只开 Turnstile 不开邮箱验证时前端也能正常渲染并注册 | C1–C10（后端）；前端渲染由 T6 实现 + T7 构建验证 | PASS（后端 C1–C4；**重放拒绝由 Cloudflare 单次性保证，本地 mock siteverify 未模拟单次性**；C4 仅证明 strict 每请求都调用 siteverify）/ 见 §6 限制 |
| 同邮箱 60 秒内二次发码 429；日上限 429；全站预算生效；SMTP 延迟时注册/发码接口仍快速返回（异步） | E2、E4、E6、G8–G11、M1–M4 | PASS（机制覆盖：M3 证明异步入队非阻塞——队列满立即返回 `ErrMailQueueFull`、不阻塞请求；未做 HTTP 层延迟计时） |
| 验证码错误 5 次锁定；注册成功后验证码被删除（不可重放） | G12、R6、R7 | PASS |
| 蜜罐非空被拒；`form_token_enabled=true` 时缺 token/过期/重放被拒 | R3、R5、T1–T8 | PASS |
| `/api/status` 5 个新字段存在；设置热生效（异步队列参数标注重启生效） | R9、G7、M3 | PASS |
| Go 测试、`go vet`、前端 typecheck/lint/build 通过；既有 5 个未提交改动完整保留 | §2 门禁命令、§3.7；既有改动未触碰（测试只新增/更新测试文件） | Go/typecheck/lint PASS；build 由 T7 负责 |
| R2-1 路由顺序：Turnstile 失败不消耗邮箱配额（verification/reset） | R2-R1 | PASS |
| R2-2 注册 body >16KB → 413 且不落库；小 body 正常注册 | R2-R2 | PASS |
| R2-3 邮箱大小写不敏感（占用/存在/注册拒绝/重置识别历史大写） | R2-R3 | PASS |
| R2-4 EmailBind 成功后验证码删除、重放失败 | R2-R4 | PASS |
| R2-5 `common.MaskEmail` 日志脱敏 | R2-R5 | PASS |
| R2-6 `async_email_enabled=false` 同步发送受限速/熔断（含并发原子占槽） | R2-R6、R2-R13 | PASS |
| R2-7 `InMemoryRateLimiter` 100000 上限逐出 + Close 无泄漏 | R2-R7、R2-R8 | PASS |
| R2-8 worker 发信峰值限速（等待不立即发送 + 入队快速失败） | R2-M1 | PASS |
| R2-9 Turnstile strict 与 `registration_setting.enabled` 解耦 | R2-C1 | PASS |
| R2-10 nonce 逐出专项 / e2e 默认配置一致性 | R2-T1、R2-R11 | PASS |
| DB 级 email 唯一索引（迁移） | R2-R12 | PASS（SQLite 已实测；PostgreSQL 共用同一 SQL 分支但未在真实 PG 实例验证；MySQL 生成列分支未实测） |
| OAuth 首登建号 guard | generic/built-in 建号路径已有 6 个 mock 用例（§9 INV-9）；真实第三方/微信端到端回调与 custom/微信/legacy 分支由代码审查覆盖 | PASS（mock）/ 见 §8.3 |

## 6. 已知限制与环境说明

1. **前端测试无法自动化**：`web/default/package.json` 的 `test` 脚本依赖 bun（`bun test tests`），本机未安装 bun；按计划前端验收以等价 tsc typecheck + `npm run lint` + T7 的 `npm run build` 为准。
2. **本机无 Redis**：所有测试强制 `common.RedisEnabled=false`，Redis 分支（限流计数、nonce、TTL 兜底）只经代码审查，未做运行时验证。
3. **`npm run typecheck` 等价命令**：`tsc -b` 在本沙箱写 `node_modules/.tmp/*.tsbuildinfo` 会 EPERM，按 §6.1 使用 `tsc -p ... --noEmit --tsBuildInfoFile %TEMP%\...`。
4. **`gofmt -w` 受限**：工作区 `Everyone DELETE DENY` 使 `gofmt -w` 无法写文件；改用 `gofmt -l` 复核，本次改动文件全部为空输出（无需格式化）。
5. **GOMODCACHE 噪音**：`go build/test` stderr 可能出现 `writing stat cache: ... Access is denied`，属良性（模块缓存只读），以 exit code 为准，未修改 go.mod/go.sum。
6. **测试隔离**：注册防护配置/内存限流器/邮件队列/表单令牌时钟都是进程级全局，用例统一快照 + `t.Cleanup` 恢复；e2e 每用例重置 guard，且不写 `heqiuyu-api.db`（内存 SQLite）、不调用真实 SMTP（`SetMailSenderForTest` 注入假 sender）。
7. **测试耗时**：Turnstile 超时用例约 5s、邮件失败重试用例约 2s（产品实现的重试间隔固定 2s），属预期。
8. **计划外问题**：T8 未新增计划外产品问题；P2 待批准项（含历史邮箱/唯一索引等已知接受偏差）由 Lead 汇总，见计划 §9。
9. **R2 修复轮 2 后已解决的原记录项**：① 内存表单令牌 nonce 逐出已有专项测试（`TestRegistrationFormTokenStoreEvictsOldest`）；② e2e 默认配置副本漂移已有守护测试（`TestRegistrationDefaultConfigCopyMatchesProduct`）；③ `ResetRegistrationGuardForTests` 已先 `Close()` 旧 limiter 再新建，并有 goroutine 泄漏冒烟测试。仍存在的残余风险见 §8.3。

### 6.1 R2 修复轮 2 后的残余风险（截至本报告）

1. **DB 级 email 唯一索引的残余风险**：已由 `model/user_email_unique_test.go` 直接调用导出的 `EnsureUserEmailUniquenessBestEffort(db)` 覆盖（**SQLite 已实测**；**PostgreSQL 共用同一 `CREATE UNIQUE INDEX ... WHERE email <> ''` SQL 分支，但未在真实 PG 实例验证**）；**MySQL 生成列 `email_unique`（VARCHAR(50)）分支未实测**（本机无 MySQL）。SQLite 下该索引大小写敏感，大小写不敏感去重仍依赖应用层 `LOWER(email)` 查询；迁移因历史重复被跳过时，并发注册仍存在重复残余风险，需后续 DB 清理/迁移专项。
2. **OAuth 首登 guard 的自动化边界**：generic/built-in 首登建号路径已由 `controller/login_session_test.go` 的 6 个 mock 用例覆盖（无码/错码/有效码/关闭无 aff/关闭带 aff/老用户，见 §9 INV-9）；真实第三方（GitHub/LinuxDO/OIDC/Discord）与微信的端到端回调依赖外部 provider 授权码交换，无法在无网络 sandbox 稳定触发，custom/微信/legacy 分支由代码审查覆盖。
3. **发信峰值等待上限（60s）超时丢弃路径未做长时测试**：已提供 `MailQueueDroppedCount()` 计数钩子与 `(total dropped: N)` 日志；测试只覆盖等待态、入队快速失败与钩子读取/Reset 归零，60s 超时丢弃分支未等待实测（避免测试挂 60s）。
4. **`common.MaskEmail` 实际只保留域名**（`***@domain`），不保留首字符；与计划文字"保留首字符与域名"存在差异，测试按实际行为断言并已上报，未改产品代码。
5. **Turnstile 失败仍返回 HTTP 200**（基线行为，未改变）；`registration_setting.enabled=false` 时发码短窗仍生效（有意保留，见 fix-round2-plan「不在本轮」）。
6. **`LOWER(email)` 使 `users.email` 原索引失效**：登录（`ValidateAndFill`）、存在性检查等改为函数比较后可能无法使用原有 email 索引（无表达式索引），用户量大时有全表扫描/CPU 放大风险。建议生产启用 `GlobalAPIRateLimit`/WAF，或后续在迁移中增加 `LOWER(email)` 表达式索引；**本轮未做**（runbook §「已知风险」同项）。
7. **发码/重置 Turnstile 前置后的限速边界**：`GlobalAPIRateLimit` 默认开启（180 次 / 180s / IP，可由环境变量 `GLOBAL_API_RATE_LIMIT_ENABLE` 关闭），api 组已挂载（`router/api-router.go:19`）；`/api/verification` 未单独挂 `CriticalRateLimit`（`:35`），`/api/reset_password` 额外挂 `CriticalRateLimit`（默认 20 次/20min，`:36`）。因此默认配置下 Turnstile 前仍有全局限速；**若生产关闭 `GlobalAPIRateLimit`，则 Turnstile 前无应用层限速**，未认证请求可触发 siteverify 外呼，建议 WAF/反代前置限速（runbook §3.9「siteverify 外呼保护」）。
8. **EmailBind / OAuth 建号唯一索引冲突未统一映射**：`Register` 已映射为 `user.exists`，但 EmailBind 与 OAuth 首登建号在 DB 唯一索引冲突时仍可能返回驱动错误文本，建议后续统一。
9. **`mail_queue` 重试与速率槽**：失败重试（首次 + 1 次）只在 `acquireSendSlot` 占 1 个速率槽，实际 SMTP 尝试最多为预算 2 倍；如需严格峰值，重试也应占槽。
10. **`common.MaskEmail` 多 `@` 输入边界**：`a@b@c` → `***@b@c`（取第一个 `@` 之后内容）；极端输入可能把非域名内容带入日志，建议后续按最后一个 `@` 截断。测试已覆盖该实际行为。
11. **OAuth 首登 guard 与密码注册共享 per-IP 窗口/成功计数**：OAuth 首登建号会占用同 IP 的注册尝试/成功配额（同一 `CheckRegistrationAttempt`/`RecordRegistrationSuccess`）；属预期共享语义，运维需知悉 OAuth 首登也算注册配额。

## 7. 复跑方式

```powershell
Set-Location E:\heqiuyu-api-main\heqiuyu-api-main
$env:GOCACHE = Join-Path $env:TEMP "gocache-step8-qa"
$env:GOTMPDIR = Join-Path $env:TEMP "gotmp-step8-qa"
go build ./...
go vet ./...
go test -count=1 ./common/... ./setting/... ./service/... ./middleware/... ./controller/... ./router/... ./model/...

Set-Location web\default
node node_modules\typescript\bin\tsc -p tsconfig.app.json --noEmit --tsBuildInfoFile "$env:TEMP\tsc-app-step8-qa.tsbuildinfo"
node node_modules\typescript\bin\tsc -p tsconfig.node.json --noEmit --tsBuildInfoFile "$env:TEMP\tsc-node-step8-qa.tsbuildinfo"
npm run lint
```

## 8. R2 修复轮 2 验收补充（2026-10-24）

> 依据：`.agent-teams/fix-round2-plan.md`（用户已批准，含「用户澄清后追加范围」）、task-16、task-23。
> 产品代码由 step5-defense 交付；T8 只更新测试与本文档，未改产品代码。

### 8.1 新增/更新用例矩阵

| # | 用例（文件） | 覆盖 R2 项 | 结果 | 证据（关键断言） |
|---|---|---|---|---|
| R2-R1 | `TestVerificationTurnstileBeforeEmailQuotaE2E`（router/registration_defense_e2e_test.go） | §1 路由顺序 | PASS | `/api/verification`：bad token 被拒（200 success:false）后，关闭 Turnstile 同邮箱首次发码成功、紧接着二次 429（配额未被失败请求消耗）；`/api/reset_password` 同样验证 |
| R2-R2 | `TestRegistrationBodyLimitE2E`（router/registration_body_limit_test.go） | §2 注册 16KB body 限制 | PASS | 正常小 body 注册成功；Content-Length>16KB → **413**、i18n 文案非原始 key、不落库；chunked（Content-Length 未知）同样 413 且不落库 |
| R2-R3 | `TestRegistrationEmailCaseInsensitiveE2E` | §3 邮箱大小写不敏感 | PASS | 历史 `Old@Example.COM` 可被 `IsEmailAlreadyTaken("old@example.com")`/`CheckUserExistOrDeleted` 识别；注册同邮箱（小写、不同用户名）被拒且不落库；重置流程 LOWER 查询成功改密 |
| R2-R4 | `TestEmailBindSuccessDeletesCodeE2E` | §4 EmailBind 清码 | PASS | 绑定成功、邮箱规范化为小写；`VerifyCodeWithKey` 变 false；同一验证码重放绑定失败 |
| R2-R5 | `TestMaskEmail`（common/rate_limit_test.go） | §5 日志脱敏 | PASS | 空值/无 `@` → `***masked***`；`alice@example.com` → `***@example.com`；local part 不出现在结果中（实现实际保留域名、不保留首字符，已记录 §6.1-4） |
| R2-R6 | `TestEnqueueEmailSyncRespectsRateLimit` / `TestEnqueueEmailSyncRespectsBreaker`（service/mail_queue_test.go） | §6 sync 回退受限速/熔断 | PASS | async=false 时第 3 封同步发送返回 `ErrMailRateLimited`；失败 sender 首次返回底层错误、第二次因熔断返回 `ErrMailBreakerOpen`。并发原子占槽见 R2-R13 |
| R2-R7 | `TestInMemoryRateLimiterDefaultMaxKeys` / `TestInMemoryRateLimiterEvictsWhenFull`（common/rate_limit_test.go） | §7 内存 limiter 硬上限 | PASS | 默认上限 100000；调小到 100 后新增 key 触发逐出：最旧 key 被逐出、最新与新 key 保留、尺寸受控、计数语义保持（上限 2 的第 3 次仍拒绝） |
| R2-R8 | `TestInMemoryRateLimiterClose` / `TestInMemoryRateLimiterCloseStopsGoroutines` / `TestResetRegistrationGuardForTestsClosesOldLimiter` / `TestResetRegistrationGuardForTestsDoesNotLeakGoroutines` | R2-2 Close/Reset 不泄漏 | PASS | Close 未初始化/重复调用不 panic；50 次 Init+Close 后 goroutine 数不增长；`ResetRegistrationGuardForTests` 创建新实例并关闭旧实例，30 次 reset 无 goroutine 线性增长 |
| R2-M1 | `TestMailQueueRateLimit`（service，重写旧语义） | worker 发信峰值限速 | PASS | 前 2 封真实发送后，第 3 封 400ms 内未发送（worker 在等速率槽）；窗口预算满时新入队快速返回 `ErrMailRateLimited`；`windowCount==2`（未预占）。丢弃计数钩子 `MailQueueDroppedCount()`、日志 `(total dropped: N)` 与 60s 分支限制见 §8.6/§8.3-3 |
| R2-C1 | `TestTurnstileStrictDecoupledFromRegistrationEnabled` / `TestTurnstileStrictDisabledFallsBackToSession`（middleware，替换旧回退用例） | strict 与 enabled 解耦 | PASS | `enabled=false + strict=true`：有有效会话标记仍因缺 token 被拒，合法 token 必须调用 siteverify；只有 `strict=false` 才回退会话复用（无会话标记时仍需 token） |
| R2-T1 | `TestRegistrationFormTokenStoreEvictsOldest` | nonce 逐出专项 | PASS | 预填 10000 条后仍可签发；表大小保持上限；最旧 nonce 被逐出、最新与新 nonce 保留（无 panic） |
| R2-R11 | `TestRegistrationDefaultConfigCopyMatchesProduct`（router/registration_body_limit_test.go） | e2e 默认配置复制一致性 | PASS | 测试副本与产品 `GetRegistrationSetting()` 默认值逐字段相等（R2 为 23 项；INV 起为 24 项，含 `InviteCodeRequired`） |
| R2-R12 | `TestEnsureUserEmailUniquenessBestEffortNilDB` / `...CreatesIndex` / `...SkipsWhenDuplicatesExist`（model/user_email_unique_test.go，3 个 Test、历史重复含 2 子用例） | DB 级 email 唯一索引（R2-BACKEND-2） | PASS（SQLite 已实测；PG 共用同一 SQL 分支但未在真实 PG 实例验证；MySQL 生成列分支未实测） | nil db→error 不 panic；唯一数据：首次 `err==nil` 且 `sqlite_master` 出现 `idx_users_email_unique`、二次幂等、空 email 多行共存、完全重复 email 直插被 UNIQUE 约束拒绝；历史重复（完全/大小写变体）：返回 error、不 panic、索引未创建、数据不删。详见 §8.3-1 |
| R2-R13 | `TestEnqueueEmailSyncAtomicRateLimitConcurrent`（service/mail_queue_test.go） | sync 路径原子 `tryAcquireSendSlot`（task-25） | PASS | `async_email_max_per_minute=2` 时 12 个 goroutine 并发 `EnqueueEmail`：成功数**恰好 2**、其余 10 次均 `ErrMailRateLimited`、无其他错误；同步路径 `MailQueueDroppedCount()==0`，`ResetMailQueueForTests()` 后仍为 0 |

### 8.2 旧测试语义变更（未削弱既有覆盖）

| 旧用例 | R2 后的处理 | 说明 |
|---|---|---|
| `TestMailQueueRateLimit` 旧断言"第 3 次 EnqueueEmail 直接 `ErrMailRateLimited`" | **重写**为 worker 峰值语义（见 R2-M1） | 新实现中入队只做背压/熔断检查，速率槽在发送前获取；旧断言不再适配新行为，非降低覆盖 |
| `TestTurnstileStrictFallsBackToSessionWhenDisabled` | **替换**为 `TestTurnstileStrictDecoupledFromRegistrationEnabled` + `TestTurnstileStrictDisabledFallsBackToSession` | strict 只读 `turnstile_strict`；回滚路径改为 `turnstile_strict=false` |
| 其余既有用例（guard/email/token/form_token/蜜罐/429/e2e 等） | 保留并按新行为断言 | 未删除、未 t.Skip、未改为恒真断言 |

### 8.3 未覆盖/受限项（需 Lead 协调或审查确认）

1. **DB 级 email 唯一索引（R2-BACKEND-2）**：**已覆盖**——由 `model/user_email_unique_test.go` 直接调用导出的 `EnsureUserEmailUniquenessBestEffort(db *gorm.DB) error` 验证：nil db、首次建索引+二次幂等、空邮箱多行共存、完全重复插入被 UNIQUE 拒绝、历史重复（完全/大小写变体）返回 error 且不删数据/不建索引。**SQLite 已实测**；**PostgreSQL 共用同一 SQL 分支但未在真实 PG 实例验证**；**MySQL 生成列 `email_unique`（VARCHAR(50)）分支未实测**（本机无 MySQL，测试固定 `common.UsingMySQL=false`）。SQLite 下 DB 索引大小写敏感、应用层 `LOWER(email)` 兜底与并发竞态残余风险见 §6.1-1/§8.4-1。
2. **OAuth 首登建号 guard（R2-BACKEND-2）的自动化边界**：generic/built-in 建号路径已有 6 个 mock 用例覆盖（§9 INV-9）；真实第三方（GitHub/LinuxDO/OIDC/Discord）/微信的端到端回调依赖外部 provider 授权码交换，无网络 sandbox 无法稳定触发，custom/微信/legacy 分支由代码审查覆盖。
3. **发信峰值 60s 超时丢弃分支**：**未等待实测**（只验证等待态、入队快速失败、`MailQueueDroppedCount()` 读取与 Reset 归零，避免测试挂 60s）；T5 已提供丢弃计数钩子与日志 `(total dropped: N)`，见 §8.6。
4. **Redis 分支**仍仅代码审查（本机无 Redis）。

### 8.4 残余风险

1. **邮箱唯一性并发竞态**：应用层 `LOWER(email)` 查询 + best-effort 唯一索引；若迁移因历史重复被跳过、或并发注册在索引缺失时仍可能重复，需后续 DB 迁移/清理专项。
2. **`common.MaskEmail` 与计划文字差异**：实际只保留域名（`***@domain`），未保留首字符；日志不会泄露 local part，但如需"保留首字符"需产品改动（本轮未改）。
3. **`async_email_enabled=false` 同步路径与异步队列共用包级 `mailSendGate`**：关闭异步后仍受限速/熔断；运维回滚时需知悉该副作用（同步发送也会被 429/熔断拒绝）。
4. **Turnstile 失败 HTTP 200**（基线行为未改）；`registration_setting.enabled=false` 时发码短窗仍生效（有意保留）。
5. **测试期全局状态**：配置/guard/queue/session 均为进程级，用例已统一快照 + Cleanup；e2e 默认配置副本新增守护测试，但后续新增配置键时需同步更新副本。
6. **产品级残余风险（审查 A 记录，未修；详细见 §6.1-6..11）**：`LOWER(email)` 可能致 email 索引失效/全表扫描；默认 `GlobalAPIRateLimit` 开启，但若生产关闭则 Turnstile 前无应用层限速、可致 siteverify 外呼；EmailBind/OAuth 唯一冲突错误文本未统一；mail 失败重试实际 SMTP 尝试最多为预算 2 倍；`MaskEmail` 多 `@` 边界；OAuth 首登与密码注册共享 per-IP 配额。

### 8.5 R2 复跑命令与结果

| 命令 | 结果 |
|---|---|
| `go build ./...` | **exit 0** |
| `go vet ./...` | **exit 0**（无告警；GOMODCACHE stat cache 噪音为良性，见 §2） |
| `go test -count=1 ./common/... ./setting/... ./service/... ./middleware/... ./controller/... ./router/... ./model/...` | **exit 0**（分包结果见 §4） |
| `gofmt -l`（本轮全部改动/新增 Go 测试文件，R2 + INV） | **空输出** |
| `go test -count=1 ./model/`（task-23 追加） | **exit 0**（3.436s，含既有 model 测试） |
| 定向复跑 | 新增 R2 用例均 PASS（verbose 证据：R2-R1..R2-R13、R2-M1、R2-C1、R2-T1） |

### 8.6 R2-2 后端收口事实（task-25）

| 项 | 实现 / 测试覆盖 |
|---|---|
| sync 路径原子占槽 | `sendEmailSync` 先 `allowSend`（熔断）再 `tryAcquireSendSlot()` 原子占用全站发信速率槽：超限立即返回 `ErrMailRateLimited`（不等待）。测试 `TestEnqueueEmailSyncAtomicRateLimitConcurrent`（R2-R13）：12 并发成功数恰好等于上限 2，其余全为 `ErrMailRateLimited` |
| worker 丢弃计数 | 新增导出 `MailQueueDroppedCount() int64`；worker 等待速率槽超过 60s 丢弃时 `mailDroppedTotal.Add(1)`，日志为 `[mail-queue] drop <kind> email (total dropped: N): ...`；`ResetMailQueueForTests()` 归零。测试 `TestMailQueueDroppedCountHookAvailable` 覆盖读取/归零；60s 丢弃分支未长测（§8.3-3） |
| MySQL 生成列 | `email_unique VARCHAR(50) GENERATED ALWAYS AS (IF(email = '', NULL, email)) STORED`（与 `users.email` 的 50 长度一致）。本机无 MySQL，**未实测**；SQLite/PG 走部分唯一索引分支 |
| OAuth 邮箱规范化 | GitHub / OIDC / 通用 OAuth 首登建号时 `user.Email = strings.ToLower(strings.TrimSpace(...))`，与注册/发码/`LOWER(email)` 查询/唯一索引策略一致；OAuth guard 的 generic/built-in 建号路径已有 6 个 mock 用例（§9 INV-9），真实第三方/微信端到端回调与 custom/微信/legacy 分支由代码审查覆盖 |
| async 路径快速失败 | 异步 `EnqueueEmail` 仍只做熔断 + 当前速率余量快速检查与有界队列背压；真实发信速率槽由 worker 在发送前通过同一 `tryAcquireSendSlot` 循环获取 |

前端门禁：T8 本轮未改前端代码，`npm run typecheck`（等价 tsc）/`npm run lint` 由 T6/T8 前一轮及 Lead 最终门禁覆盖；`npm run build` 由 T7 负责。

## 9. INV-S3 邀请码注册验收补充（2026-10-25）

> 依据：`.agent-teams/invite-code-plan.md`（用户已批准）与 task-30、task-44、task-49。产品代码由 step5-defense / step6-contract-web 交付；T8 只更新测试与本文档，未改产品代码。

### 9.1 配置与契约事实

- 新配置键 `registration_setting.invite_code_required`（默认 **false**，第 **24** 项）；`/api/status` 增加 `invite_code_required`。
- 邮箱验证码 purpose：注册用途 `"vr"`、普通邮箱验证/绑定用途 `"v"`、密码重置 `"r"`；开启邀请码时注册发码写入 `"vr"`，关闭时 `purpose=register` 亦写入 `"vr"`（注册时优先 `"vr"`、无 `"vr"` 才回退 `"v"`）。
- 路由顺序：`/api/verification` = TurnstileCheckStrict → RegistrationInviteGuard → EmailSendRateLimit → SendEmailVerification；注册链不变，邀请码在 controller 内先校验。
- i18n 新增 `user.invite_code_required` / `user.invite_code_invalid` / `user.invite_code_attempts_limit`（3 语言）。
- 邀请码复用任意用户 `aff_code`：仅 trim、精确匹配、区分大小写；不新增表/迁移。
- 邀请归因：密码注册/OAuth 首登（generic/custom）与微信、legacy 首登建号前均显式写 `users.inviter_id`（关闭邀请码时写 aff 对应 ID，无邀请/推荐写 0；老用户不改写）。task-43 修复后与密码注册行为一致，测试见 INV-9。

### 9.2 新增/更新用例矩阵

| # | 用例（文件） | 覆盖 | 结果 | 证据（关键断言） |
|---|---|---|---|---|
| INV-1 | `TestInviteCodeStatusDefaultAndSwitchE2E`（router/registration_invite_e2e_test.go） | 默认关闭 + status 字段 + 与 enabled 解耦 | PASS | `/api/status` 默认 `invite_code_required=false`；置 true 后为 true；`registration_setting.enabled=false` 时仍为 true |
| INV-2 | `TestRegistrationDefaultConfigCopyMatchesProduct`（router/registration_body_limit_test.go） | 24 项默认值一致性 | PASS | 测试副本与产品默认值逐字段相等（含 `InviteCodeRequired=false`）；显式快照/重置，单独 `-run` 与全量均稳定 |
| INV-3 | `TestInviteCodeDisabledAllowsRegistrationE2E` | 关闭态完全不校验 | PASS | 普通发码（"v"）+ 错误 invite_code → 注册成功且 inviter 为空；关闭态携带**合法** invite_code 也不得被当作邀请（`inviter_id=0`，防回归）；`purpose=register` 发码（"vr"）+ 错误 invite_code 不被拦截，用 "vr" 注册成功 |
| INV-4 | `TestInviteCodeEnabledVerificationRequiresCodeE2E` | 开启态发码守卫 | PASS | 缺码/错码（含大小写变体）→ 200 success:false 且 fake mail **0 封**；有效码成功并写入 "vr"（不写 "v"）；连续 20 次无效→第 **21 次 429 + Retry-After 600**；有效码不被失败限速误伤；无效码不消耗邮箱日配额（`email_max_per_day=1` 下首次有效成功、第二次换 IP 429 `email_day`） |
| INV-5 | `TestInviteCodeEnabledRegistrationE2E` | 开启态注册 | PASS | 缺码/错码/大小写变体拒绝且 **"vr" 码不被消耗**；连续 5 次错误邀请码不触发验证码锁定；有效码+"vr" 注册成功且 `users.inviter_id` = 邀请人 ID；"v" 码不能注册且注册失败后仍保留 |
| INV-6 | `TestInviteCodePurposeSeparationE2E` | purpose 隔离 | PASS | 开启态 EmailBind 用 "v" 码成功绑定；"vr" 码不能用于绑定（失败） |
| INV-7 | `TestInviteCodeRegistrationLockoutPurposeE2E`（3 子用例） | D2 锁定按实际 purpose、不回退 | PASS | 开启：用 "v" 码注册连续 5 次 → 第 5 次 429，删除 "vr"、保留 "v"（锁定不回退）；关闭且存在 "vr"：同样按 "vr" 计数锁定并删 "vr"、保留 "v"；关闭仅 "v"：回退 "v" 注册成功，且成功前的错误计数被清理（成功后再错 5 次才 429） |
| INV-8 | `TestValidateInviteCode` / `TestValidateInviteCodeWithoutDB` / `TestValidateInviteCodeErrorTypeIsStable`（service/invite_code_test.go）+ `TestControllerValidateInviteCode` 等（controller/invite_code_test.go） | D1 服务层/Controller 层语义 | PASS | `AbC123` 有效；`abc123`/`ABC123` invalid（区分大小写）；前后空白 trim 后有效；空→required；未知→invalid；DB 未初始化→invalid 不 panic；错误信息不含邀请码明文；controller 委托/错误映射/session 读取/统一 200+success:false 文案均验证 |
| INV-9 | OAuth mock 6 用例（controller/login_session_test.go） | OAuth 首登 + inviter_id 归因 | PASS | 开启无码→拒绝且不建号；错码→拒绝且不建号；有效码→建号成功、邮箱 lower+trim、**`users.inviter_id` = 邀请人 ID**，且邀请人 `aff_count` +1（`QuotaForInviter>0`）；关闭无 aff→建号成功且 inviter_id=0；关闭带 aff→inviter_id = aff 所属用户 ID；老用户（IsUserIDTaken/Fill）无码可登录、不新建账号且原 inviter_id 不被改写（42 保持） |
| INV-10 | `TestMaskInviteCodeInPath`（middleware/logger_test.go） | 访问日志脱敏 | PASS | `invite_code` 值替换为 `***`，其它 query 参数与顺序不变；无 query/空 query 不变；多值全部掩码；percent-encoded key `invite%5Fcode` 与大小写变体 key 不脱敏（已知残余，测试明确记录） |

### 9.3 验证命令与结果

| 命令 | 结果 |
|---|---|
| `go build ./...` | **exit 0** |
| `go vet ./...` | **exit 0** |
| `go test -count=1 ./common/... ./setting/... ./service/... ./middleware/... ./controller/... ./router/... ./model/...` | **exit 0**（common 2.784s / service 8.101s / middleware 11.863s / controller 5.396s / router 8.038s / model 4.338s；setting 各包含测试包均 ok，见 §4） |
| `gofmt -l`（本轮全部改动/新增 Go 测试文件） | **空输出** |
| 定向复跑 | `-run TestInviteCode ./router/`、`-run TestOAuthInvite ./controller/`、unit 三包定向用例均 PASS；task-44 后 `go test -count=1 ./controller/... ./router/... ./model/...` 亦 exit 0 |
| 隔离复跑（task-49，F1） | `go test -count=2 -run 'TestOAuthInvite' ./controller/` **exit 0**（6.293s，两轮均通过；OAuth setup 已 `ResetRegistrationGuardForTests()` + 清零该测试注册限流上限）；`go test -count=2 -run 'TestInviteCode' ./router/` **exit 0**（7.411s）；`go test -shuffle=on -count=1 ./controller/ ./router/` **exit 0**（controller 4.633s / router 7.175s） |

### 9.4 残余风险与已修复项

1. **邀请码失败限速仅单实例且仅覆盖发码接口**：`RegistrationInviteGuard` 的 20 次/600s 失败计数使用进程内 `InMemoryRateLimiter`（非 Redis），多实例不共享；仅作用于 `/api/verification?purpose=register`，注册/OAuth 的邀请码错误由注册限流/邮箱验证码锁定等既有机制约束。限速常量为硬编码，不可配置。**前提：注册侧 `RegistrationRateLimit`（`CheckRegistrationAttempt`）仅在 `registration_setting.enabled=true` 时生效；`enabled=false` 时注册侧仅剩 `CriticalRateLimit`/`GlobalAPIRateLimit`（或 WAF）**——INV 邀请码校验本身仍独立于 enabled 生效，与 runbook §3.11.5 口径一致。
2. **邀请码无过期/吊销/次数限制**：复用 4 位 `aff_code`，安全性依赖 Turnstile、发码限流、20 次失败锁定与注册限流；不支持单码吊销，只能关闭开关。注册侧限流前提同上：仅在 `registration_setting.enabled=true` 时生效；`enabled=false` 时注册侧仅剩 `CriticalRateLimit`/`GlobalAPIRateLimit`（或 WAF）。
3. **日志脱敏仅匹配字面 `invite_code`**：percent-encoded key（`invite%5Fcode`）与大小写变体 key 不会被掩码（测试已注明）；gin 通常保留原始 RawQuery，具体取决客户端编码方式。
4. **已修复（task-43）：OAuth/微信/legacy 首登 inviter_id 落库**。修复前 OAuth 首登仅把邀请码用于邀请奖励、不写 `users.inviter_id`；现 generic/custom/微信/GitHub/LinuxDO/OIDC/Discord 建号前显式 `user.InviterId = inviterId`（0 也写）。OAuth mock 已断言开启有效码/关闭带 aff/关闭无 aff/老用户不变四种取值；generic/custom/微信/legacy 分支由代码审查覆盖（本机无法端到端触发真实第三方/微信回调）。
5. **关闭态旧 localStorage（残余已收窄，task-41）**：注册页关闭态已清 localStorage/不传码（S2/task-41）；残余仅“登录页直达 OAuth、或用户未重新访问注册页时”旧 localStorage 可能仍被发送——后端关闭态完全忽略（不影响注册），access log 已掩码。旧 dist 未更新前开启会阻断注册（计划边界：先前端、后开开关）。
6. **Redis 分支未运行时验证**：`common.HasVerificationCode` 的 Redis 分支、Redis 限流等仍仅代码审查（本机无 Redis）。
7. **OAuth state 中邀请码会被同浏览器后续流程覆盖写入（每次覆盖为空/新值）**：并发多标签注册可能互相覆盖，属低风险，计划已接受。
8. **本机无 MySQL/真实 PG**：DB 唯一索引 MySQL 生成列分支、PG 实例行为仍未实测（见 §6.1-1）；`LOWER(email)` 索引失效等既有残余风险继续保留。
9. **前端登录页直达第三方的新用户无码拒绝/提示无自动化**：仅有后端 OAuth mock 覆盖“无码/错码拒绝且不建号”；前端登录页的提示/跳转逻辑因本机无 bun 无法自动化，**未做端到端验证**，需人工冒烟（如实记录，勿视为已端到端验证）。
