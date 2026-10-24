# 注册防护发布与运维手册（Registration Defense Runbook）

> 适用版本：`v1.2.0` 工作区（2026-10-24）。对应实施计划（团队内部文件）第 5–8 步（已获用户批准）。
>
> 本手册面向发布/运维人员，说明注册防护的配置项、上线顺序、回滚开关、监控日志、垃圾账号处置、文件清单与 PR 拆分；**不是**用户使用说明（用户说明见仓库根 `README.md`）。
>
> 当前线上状态：**注册已关闭**（用户决定，P0 已完成）。本手册不指导重新开启线上注册；如需开放注册，须由用户批准并单独走变更单。
>
> 基线说明：本手册基于 2026-10-24 T5/T6 实现冻结态编写；2026-10-25 追加「仅限邀请码注册（INV）」章节（见 §3.11/§8.9）。若后续代码变更（含代码审查意见修复），以最终代码与 diff 为准，并同步更新本手册对应段落。

## 目录

1. [能力总览与请求链路](#1-能力总览与请求链路)
2. [配置项（24 项）](#2-配置项24-项)
3. [上线步骤（SMTP → 邮箱验证 → Turnstile，含 §3.11 仅限邀请码注册）](#3-上线步骤smtp--邮箱验证--turnstile)
4. [发布前验证与构建顺序](#4-发布前验证与构建顺序)
5. [回滚开关与副作用](#5-回滚开关与副作用)
6. [监控与日志](#6-监控与日志)
7. [垃圾账号处置 runbook（待执行）](#7-垃圾账号处置-runbook待执行)
8. [文件清单与 PR 拆分检查表](#8-文件清单与-pr-拆分检查表)
9. [已知限制与待处理项](#9-已知限制与待处理项)
10. [附录：快速命令模板](#10-附录快速命令模板)

---

## 1. 能力总览与请求链路

### 1.1 能力清单

| 能力 | 实现位置 | 开关 | 默认 |
|---|---|---|---|
| Turnstile 严格校验（每次校验，不复用 session） | `middleware/turnstile-check.go` | `turnstile_strict`（**不再受 `registration_setting.enabled` 影响**，R2 已解耦） | 开启 |
| 注册限流（per-IP 尝试数 / 小时、日成功上限 / 全站成功上限 / IP·CIDR 白名单） | `middleware/registration-rate-limit.go`、`service/registration_guard.go` | `registration_setting.enabled` 及各阈值 | 开启 |
| 发码防护-配额类（同邮箱冷却/日上限、per-IP 小时上限、全站小时预算） | `middleware/email-send-rate-limit.go`（配额检查部分）、`service/registration_guard.go` | `registration_setting.enabled`（基线「单 IP 30 秒 2 次」短窗不受该开关控制，见下行） | 开启 |
| 发码短窗（原 `EmailVerificationRateLimit`：单 IP 30 秒内最多 2 次） | `middleware/email-send-rate-limit.go`（`emailSendBurstAllow`） | **基线常开保护，不读 `registration_setting`**；`enabled=false` 也仍然生效 | 常开 |
| 注册请求体上限（16KB） | `middleware/registration-body-limit.go` | 固定常量 `RegistrationBodyMaxBytes`（非配置项） | 常开 |
| 邮箱大小写不敏感查询 + best-effort 唯一索引 | `model/user.go`（`LOWER(email)` 查询）、`model/main.go`（启动时建索引） | 无开关；启动自动执行，重复/失败则跳过并告警 | 启动生效 |
| OAuth 首登建号注册限流（六条 OAuth 路径） | `controller/{github,linuxdo,oidc,discord,wechat,oauth}.go` | `registration_setting.enabled` 及各注册阈值 | 开启 |
| 验证码错误次数限制（5 次/10 分钟锁定） | `service/registration_guard.go` + controller | `verification_max_attempts` / `verification_attempt_window_seconds` | 开启 |
| 蜜罐字段（`website` 非空拒绝） | `middleware/registration-payload-guard.go` | `honeypot_enabled` | 开启 |
| 一次性 HMAC 表单令牌（最短填写时间/有效期/防重放） | `service/registration_token.go`、`controller/registration_token.go`、`middleware/registration-payload-guard.go` | `form_token_enabled` | **关闭**（前端就绪后再开） |
| 仅限邀请码注册（`invite_code_required`，独立于注册防护总开关） | `service/invite_code.go`、`controller/invite_code.go`、`middleware/registration-invite-guard.go` | `registration_setting.invite_code_required`（默认关闭，仅 root 可改） | 关闭 |
| 异步邮件队列（有界队列 / worker 真实发送前取速率槽 / 连续失败熔断；`async=false` 同步回退也受限速/熔断） | `service/mail_queue.go` | `async_email_enabled` | 开启 |
| `/api/status` 注册状态字段（含 `invite_code_required`） | `controller/registration_status.go` | 只读 | — |

### 1.2 路由与中间件链（以 `router/api-router.go` 为准）

```
POST /api/user/register
  api 组（RouteTag/压缩/BodyStorageCleanup/GlobalAPIRateLimit）
  → CriticalRateLimit（基线保留）
  → RegistrationRateLimit
  → RegistrationBodyLimit(16KB)      // R2 新增：Content-Length 超限直接 413，其余包 MaxBytesReader
  → TurnstileCheckStrict
  → RegistrationPayloadGuard（蜜罐 / form_token；读取 body 超限时 413）
  → controller.Register

GET /api/verification
  api 组
  → TurnstileCheckStrict             // 先过人机验证
  → RegistrationInviteGuard          // INV：仅 purpose=register 且开启时校验邀请码（20 次/600s 失败上限）
  → EmailSendRateLimit（含原 EmailVerificationRateLimit 的「单 IP 30 秒 2 次」短窗语义）
  → controller.SendEmailVerification（purpose=register 时写 "vr"）

GET /api/reset_password
  api 组
  → CriticalRateLimit（基线保留）
  → TurnstileCheckStrict             // R2：先过人机验证
  → EmailSendRateLimit
  → controller.SendPasswordResetEmail

GET /api/user/register/token
  api 组（仅 GlobalAPIRateLimit 等既有中间件；不加 CriticalRateLimit/RegistrationRateLimit）
  → controller.GetRegistrationToken

POST /api/user/reset（重置密码提交，验证码尝试限制在 controller 内）
  api 组 → CriticalRateLimit → controller.ResetPassword
```

说明：

- **发码顺序（R2）**：`/api/verification`、`/api/reset_password` 均为 Turnstile 在 `EmailSendRateLimit` 之前；未通过人机验证的请求**不会消耗**邮箱冷却/日上限、IP 小时、全站小时预算，也不消耗「单 IP 30 秒 2 次」短窗计数。
- **注册链（R2）**：`RegistrationBodyLimit(16KB)` 在 Turnstile 之前；`RegistrationPayloadGuard` 移到 Turnstile 之后，未过人机验证的请求不会被读取/解析 body。16KB 上限只作用于 `/api/user/register`。
- **OAuth 首登建号（R2）**：GitHub/LinuxDO/OIDC/Discord/WeChat/通用 OAuth 六条路径在 `RegisterEnabled` 检查通过、新用户入库前调用 `service.CheckRegistrationAttempt(c.ClientIP())`，入库成功后调用 `service.RecordRegistrationSuccess(c.ClientIP())`；拒绝时返回 HTTP 200 失败 JSON（`user.registration_rate_limited`，无 `Retry-After`），**不建号**。
- `TurnstileCheckEnabled=false`（系统设置未启用 Turnstile）时，`TurnstileCheckStrict` 与 `TurnstileCheck` 均直接放行。
- `RegistrationFormTokenEnabled()` 与 `registrationStatusFields()` 均为「生效值」：`registration_setting.enabled=false` 时表单令牌视为关闭，token 接口返回 404。
- **仅限邀请码注册（INV，详见 §3.11）**：开关只读 `registration_setting.invite_code_required`，**独立于** `registration_setting.enabled`。开启时密码注册在 controller 内（邮箱验证码校验**之前**）校验邀请码；GitHub/LinuxDO/OIDC/Discord/自定义 OAuth 五条与微信首登（**共六条建号路径**）在**建号前**校验，老用户登录/绑定邮箱不校验；关闭时完全不校验。
- **注册发码 purpose**：注册页调用 `/api/verification?purpose=register`，后端写注册专用验证码 purpose `"vr"`（`common.RegistrationEmailVerificationPurpose`）；绑定邮箱等其它用途仍用 `"v"`。开启邀请码时注册**只接受 `"vr"`**；关闭时优先 `"vr"`，仅当不存在 `"vr"` 时才接受 `"v"`（不会在有 `"vr"` 时回退 `"v"`）。
- **OAuth/微信邀请码携带路径**：注册页把邀请码写入 localStorage（`invite_code`），`getOAuthState` 附加 `invite_code`；`GenerateOAuthCode` 每次覆盖写入 session；微信登录通过 query 携带；服务端从 session/query 读取后交给 `service.ValidateInviteCode`。

### 1.3 响应语义

| 接口 | 触发条件 | HTTP | 关键内容 |
|---|---|---|---|
| `POST /api/user/register` | `CriticalRateLimit` 超限（基线） | 429 | 仅状态码（无 JSON body），无 `Retry-After` |
| `POST /api/user/register` | `RegistrationRateLimit` 命中（窗口尝试数/小时/日/全站成功上限） | 429 | `Retry-After` + `{"success":false,"message":"注册过于频繁，请稍后再试"}` |
| `POST /api/user/register` | 蜜罐 `website` 非空，或 `form_token` 缺失/签名错/过期/重放 | 400 | `{"success":false,"message":"注册请求被拒绝"}` + `[registration-guard] registration rejected (...)` 日志 |
| `POST /api/user/register` | Turnstile（启用时）token 为空 | **200** | `{"success":false,"message":"Turnstile token 为空"}` |
| `POST /api/user/register` | Turnstile（启用时）校验失败/超时/重放 | **200** | `{"success":false,"message":"Turnstile 校验失败，请刷新重试！"}`（历史行为：校验失败用 200 返回，前端按 message 提示，**不要**按 429 排查） |
| `POST /api/user/register` | 请求体超过 16KB（`Content-Length` 已知超限，或分块传输被读取时触发） | **413** | `user.request_body_too_large`（`Request body is too large` / `请求内容过大`） |
| `POST /api/user/register` | 邮箱验证码错误达到上限 | 429 | `Retry-After` + `user.verification_attempts_exceeded`，同时删除验证码 |
| `POST /api/user/register` | 注册总开关/密码注册关闭、参数错误、用户已存在、唯一索引冲突 | 既有失败 JSON | 按基线约定（不新增状态码） |
| `POST /api/user/register` | INV 开启且邀请码缺失/无效 | **200** | `user.invite_code_required` / `user.invite_code_invalid`；在邮箱验证码校验**之前**返回，不消耗验证码、不进入邮箱验证/占用检查 |
| `GET /api/verification` | 30 秒 2 次短窗、同邮箱冷却、邮箱日上限、IP 小时上限、全站小时预算 | 429 | `Retry-After` + `user.email_send_rate_limited` |
| `GET /api/verification` | 队列满 / 熔断中 | 503 | `user.email_send_unavailable`；**刚写入的验证码已被删除**（不会留下可用的悬挂验证码） |
| `GET /api/verification` | 队列/同步回退的全站每分钟限速 | 429 | `Retry-After: 60` + `user.email_send_rate_limited`；同样删码 |
| `GET /api/verification?purpose=register` | INV 开启且邀请码缺失/无效 | **200** | `user.invite_code_required` / `user.invite_code_invalid`；不进入邮箱配额、不发信 |
| `GET /api/verification?purpose=register` | 邀请码失败次数超过 20 次/600s（同 IP，单实例内存计数） | 429 | `Retry-After: 600` + `user.invite_code_attempts_limit` |
| `GET /api/reset_password` | 上述发码限流类 | 429 | `Retry-After` + `user.email_send_rate_limited` |
| `GET /api/reset_password` | 邮件入队失败（队列满/熔断/限速，含 `async=false` 同步回退） | **200** | 仍返回 `{"success":true}`（防止「邮箱是否注册」枚举 oracle）；仅日志告警，验证码已删除 |
| OAuth 首登建号（GitHub/LinuxDO/OIDC/Discord/WeChat/通用 OAuth） | 注册限流命中（窗口/小时/日/全站成功上限） | **200** | `{"success":false,"message":"注册过于频繁，请稍后再试"}`（`user.registration_rate_limited`）；不建号、无 `Retry-After` |
| OAuth/微信首登建号 | INV 开启且邀请码缺失/无效 | **200** | `user.invite_code_required` / `user.invite_code_invalid`（i18n 文案）；不建号、不写 `inviter_id`；老用户登录不校验 |
| `GET /api/user/register/token` | 表单令牌未启用（或注册防护总开关关闭） | 404 | `{"success":false,"message":"Not Found"}` |
| `GET /api/user/register/token` | 正常 | 200 | `{"success":true,"data":{"token":"...","min_seconds":3,"expires_in":1800}}` |
| `POST /api/user/reset` | 验证码错误达到上限 | 429 | `Retry-After` + `user.verification_attempts_exceeded`，同时删除验证码 |
| `GET /api/status` | — | 200 | 新增 6 字段（见下） |

> 说明：`TurnstileCheckStrict` 为注册、发码、重置密码三条路由共用同一套校验：**缺失 token（空字符串）**返回 `Turnstile token 为空`；**校验失败/超时**返回 `Turnstile 校验失败，请刷新重试！`。两者均为 HTTP 200 + `success:false`。R2 起严格模式**只由 `turnstile_strict` 控制**，不再受 `registration_setting.enabled` 影响。
>
> INV（仅限邀请码注册）错误均为 HTTP 200 + `success:false`（文案区分 required/invalid），不消耗邮箱验证码/邮箱配额、不发信；仅 `/api/verification?purpose=register` 的邀请码**失败计数**超过 20 次/600s 时返回 429 `user.invite_code_attempts_limit`。
>
> 16KB 注册请求体边界：`Content-Length` 已知超限时由 `RegistrationBodyLimit` 直接 413；未知长度（分块传输）由 `RegistrationPayloadGuard` 在读取时识别并返回 413。若蜜罐与表单令牌均关闭（PayloadGuard 不读 body），分块传输且超过 16KB 的注册请求可能回落到 HTTP **200 参数错误**（失败 JSON），不会返回 413；已知 `Content-Length` 的场景始终 413。`http.MaxBytesReader` 只限制读取时的内存上限，不保证错误状态码。

`/api/status` 新增字段（均为「生效值」）：

- `register_enabled`
- `password_register_enabled`
- `oauth_register_enabled`
- `registration_form_token_enabled`
- `registration_honeypot_enabled`
- `invite_code_required`（独立于 `registration_setting.enabled`，只读该键）

---

## 2. 配置项（24 项）

- 配置前缀：`registration_setting.`；存储于数据库 `options` 表，键为 `registration_setting.<json tag>`。
- 管理入口：**系统设置 → 认证（Auth）→ 注册防护**（前端路由 `/_authenticated/system-settings/auth/registration-protection`；该路由仅 `SUPER_ADMIN` 可进，保存走 `/api/option` RootAuth，即**仅 root 可见/可改**）。
- 生效方式：options 更新后本实例热生效；其他实例每 `SYNC_FREQUENCY`（默认 60 秒）从数据库重载一次（**配置同步不需要 Redis**）。
- 例外：`async_email_queue_size`、`async_email_workers` **重启生效**（队列在首次发信时惰性创建；重启会丢弃队列中尚未发送的邮件）。
- 保存设置后：前端对 `registration_setting.*` 会立即失效并刷新 `/api/status` 与系统配置缓存（R2 `use-update-option.ts`），无需等待缓存过期；其它实例仍受 `SYNC_FREQUENCY` 同步周期影响。

| # | 键（省略 `registration_setting.` 前缀） | 默认 | 单位 | 含义与取值说明 |
|---|---|---|---|---|
| 1 | `enabled` | `true` | 开关 | 注册防护总开关。关闭后注册限流、验证码尝试限制、蜜罐、表单令牌失效；发码防护的邮箱冷却/日上限、IP 小时上限、全站小时预算失效，但基线「单 IP 30 秒 2 次」发码短窗仍生效（**不受该开关控制**）；OAuth 首登建号 guard 也随该开关失效；**Turnstile 严格模式不受该开关影响（R2 解耦，回滚 Turnstile 只能用 `turnstile_strict=false`）**。 |
| 2 | `turnstile_strict` | `true` | 开关 | `true`：注册/发码每次强制校验 Turnstile，不读写 session；`false`：退回 30 分钟会话复用（L5 语义），仅临时回滚用。 |
| 3 | `ip_attempt_window_seconds` | `600` | 秒 | 注册尝试窗口；窗口内每个注册请求都计数（含失败）。`<=0` 时兜底 600。 |
| 4 | `ip_max_attempts_per_window` | `10` | 次 | 单 IP 窗口内最多注册请求数；第 11 次起 429。`0` = 不限制该项。 |
| 5 | `ip_max_success_per_hour` | `5` | 次 | 单 IP 每小时成功注册上限；`0` = 关闭该项。 |
| 6 | `ip_max_success_per_day` | `20` | 次 | 单 IP 每天成功注册上限；`0` = 关闭该项。 |
| 7 | `ip_allowlist` | `""` | 列表 | 逗号分隔 IP/CIDR 永久放行。命中后跳过注册限流与发码配额类限流（含成功计数回写），但**不跳过** Turnstile、基线「单 IP 30 秒 2 次」发码短窗、蜜罐/表单令牌、验证码尝试限制。 |
| 8 | `global_max_success_per_hour` | `0` | 次 | 全站每小时成功注册上限；`0` = 关闭。 |
| 9 | `email_cooldown_seconds` | `60` | 秒 | 同一邮箱（规范化后 sha256 散列）两次发码的最小间隔。 |
| 10 | `email_max_per_day` | `5` | 次 | 同一邮箱每天发码上限；`0` = 关闭。 |
| 11 | `email_ip_max_per_hour` | `10` | 次 | 单 IP 每小时发码上限；`0` = 关闭。 |
| 12 | `email_global_max_per_hour` | `120` | 次 | 全站每小时发码上限（保护 SMTP）；`0` = 关闭。 |
| 13 | `verification_max_attempts` | `5` | 次 | 验证码错误次数上限；达到后锁定并删除验证码。`<=0` 时兜底 5。 |
| 14 | `verification_attempt_window_seconds` | `600` | 秒 | 验证码错误计数窗口；`<=0` 时兜底 600。 |
| 15 | `honeypot_enabled` | `true` | 开关 | `website` 蜜罐字段开关；非空即拒绝注册（400）。 |
| 16 | `form_token_enabled` | `false` | 开关 | 一次性 HMAC 表单令牌开关；**前端就绪后再开启**（见 §3.6）。 |
| 17 | `form_token_min_seconds` | `3` | 秒 | 表单最短填写时间；早于该时间提交视为无效。`0` = 不限制。 |
| 18 | `form_token_max_minutes` | `30` | 分钟 | 令牌最长有效期；`<=0` 时兜底 1800 秒。 |
| 19 | `async_email_enabled` | `true` | 开关 | 异步发信开关；`false` = 同步发送回退（副作用见 §5）。 |
| 20 | `async_email_queue_size` | `256` | 封 | 队列长度；`<=0` 兜底 256。**重启生效**（workers 会被限制为 `<= queue_size`）。 |
| 21 | `async_email_workers` | `2` | 个 | worker 数；`<=0` 兜底 2。**重启生效**。 |
| 22 | `async_email_max_per_minute` | `60` | 封/分 | 全站发信速率上限；`<=0` 兜底 60。热生效。R2 起由 worker 在**真实 SMTP 发送前**取速率槽（达到上限时等待到下一分钟窗口，最长等待 60 秒，超时丢弃该邮件并记 `[mail-queue] drop ...` 日志）；`async_email_enabled=false` 的同步回退也受同一预算限制。 |
| 23 | `async_email_breaker_failures` | `5` | 次 | 连续失败熔断阈值（熔断 60 秒）；`<=0` 兜底 5。热生效。注意单封邮件失败后先间隔 2 秒重试 1 次，重试仍失败才计入连续失败；`async=false` 同步回退的失败同样计入该阈值。 |
| 24 | `invite_code_required` | `false` | 开关 | **仅限邀请码注册（INV）**：开启后密码注册、OAuth 五条与微信首登（共六条建号路径）必须携带有效邀请码（任意现有用户的 `aff_code`）；**独立于 `registration_setting.enabled`**（关总开关不关它）；仅 root 可改（设置页路由仅 `SUPER_ADMIN`，`/api/option` 为 RootAuth）；关闭时完全不校验（详见 §3.11）。 |

补充：

- 「`0` = 关闭」仅适用于上表标注的各类上限（代码按 `> 0` 判断）；窗口秒数、队列参数为 `<=0` 时使用兜底值，不是关闭。
- 阈值热调不会追溯已经写入的窗口计数（例如把窗口从 600 秒改为 300 秒，已存在的 600 秒窗口键仍按原 TTL 存在）。
- 修改 `form_token_min_seconds` / `form_token_max_minutes` 会立即影响已签发但尚未提交的令牌。
- 固定上限与启动行为（非配置项，R2）：注册请求体上限 **16KB**（`middleware/registration-body-limit.go`）；启动时 best-effort 创建**非空邮箱唯一索引**（历史重复数据存在则跳过并 `SysLog` 告警）；进程内限流器键数量上限 **100000**（`common.InMemoryRateLimiterMaxKeys`，超限按最旧/过期键逐出）。
- INV 相关固定值（非配置项）：邀请码即用户 `aff_code`，由 `common.GetRandomString(4)` 生成（4 位字母数字、区分大小写）；`/api/verification?purpose=register` 的邀请码失败计数上限 **20 次/600s/IP**（`middleware/registration-invite-guard.go` 硬编码、单实例内存实现）；不新增表/不迁移数据。

---

## 3. 上线步骤（SMTP → 邮箱验证 → Turnstile）

> 顺序要求：先确认 SMTP 可用，再开启邮箱验证，最后开启 Turnstile。任一步失败应停止并回退该步开关，不带病上线。
>
> 当前线上注册为关闭状态；本节的「开启」均指在**测试环境**或经用户批准开放注册后执行，不要自行打开线上注册。

### 3.1 前置检查

1. 备份：数据库（含 `options` 表）、`.env`、当前镜像/二进制版本。
2. 密钥：`SESSION_SECRET`、`CRYPTO_SECRET` 必须显式设置（各 ≥32 字符且互不相同）；多实例必须一致；`CRYPTO_SECRET` 丢失/更换会使表单令牌全部失效，并影响所有派生密钥。
3. Redis：确认 `REDIS_CONN_STRING` 与 `SYNC_FREQUENCY`。**多实例必须启用 Redis**（限流计数、验证码、表单令牌 nonce 跨实例共享）；**例外：INV 邀请码失败计数为单实例内存，启用 Redis 也不会共享**，多实例 INV 需 WAF/反向代理前置限速或后续扩展。单实例可无 Redis（内存实现，重启清零）。
4. 代理：`TRUSTED_PROXIES` 设为真实反向代理 IP/CIDR；未配置时 `ClientIP()` 退化为代理地址，限流会把所有用户当成同一 IP。生产禁止 `*`。HTTPS 部署设 `COOKIE_SECURE=true`。
5. 前端：确认 `web/default/dist` 已由**正常权限**重新构建（见 §4），否则新设置页/注册页逻辑不存在，`form_token`/Turnstile 严格模式会直接失败。
6. 启动日志检查 best-effort 邮箱唯一索引（R2）：正常应无相关日志；若出现 `[registration-guard] skip email unique index: ... duplicate email group(s) exist` 或 `[registration-guard] create email unique index failed: ...`，说明索引未创建（历史重复邮箱或 DB 不支持），应用层 `LOWER(email)` 查询仍兜底，需人工清理重复邮箱后重启（索引创建失败不阻断启动）。

### 3.2 配置 SMTP 并验证真实发信

- 配置项（系统设置 → 邮件相关）：`SMTPServer`、`SMTPPort`、`SMTPAccount`、`SMTPFrom`、`SMTPToken`、`SMTPSSLEnabled`、`SMTPForceAuthLogin`。
- 验证方式：注册关闭期间可用「忘记密码」对**已存在的测试邮箱**发信；或在测试环境临时开启注册走完整发码流程。
- 常见排错：端口 465 自动走 TLS；`smtp: server doesn't support AUTH` 检查 `SMTPForceAuthLogin`；发信失败日志 `failed to send email to ***@domain: ...`——R2 起由 `common.MaskEmail` 脱敏为 `***@域名`（基线实现：保留域名、**不保留首字符**；比计划文字的“保留首字符与域名”更严格，按实现记录），不会出现明文收件邮箱；仍注意日志访问权限。

### 3.3 开启邮箱验证

- 开启 `EmailVerificationEnabled`（系统设置）。
- 预期行为：
  - 邮箱验证码为 **8 位**（后端 `GenerateVerificationCode(8)`），10 分钟内有效；
  - 同邮箱 60 秒内二次发码 429；验证码错误 5 次/10 分钟锁定并删除验证码；
  - 注册成功后验证码被删除（不可重放）；
  - 前端邮箱绑定弹窗的验证码输入长度已同步为 8 位（T6 审查修复，前端需为新版 dist）。
- 邮箱大小写不敏感（R2）：注册、发码、重置密码、邮箱绑定、登录均先 `strings.ToLower(strings.TrimSpace())` 再以 `LOWER(email)` 查询；历史大写邮箱**无需迁移**即可被识别，重复注册（仅大小写不同）返回 `user.exists`。
- EmailBind（R2）：绑定成功后立即删除验证码并清除错误计数，验证码不可重放。
- 唯一性（R2，best-effort）：启动时尝试为非空邮箱创建唯一索引（SQLite/PostgreSQL 部分索引 `WHERE email <> ''`；MySQL 生成列 `email_unique` + 唯一索引）；若历史数据存在重复邮箱则跳过创建并告警（不删数据、不阻断启动）。**残余风险**：DB 唯一索引对大小写敏感，并发提交仅大小写不同的同一邮箱仍可能出现竞态（应用层 `LOWER(email)` 查询兜底）；MySQL 分支未实测。
- 只开邮箱验证、不开 Turnstile 也应能正常注册（回归项）。

### 3.4 开启 Turnstile

- 系统设置 → Bot Protection：填写 `TurnstileSiteKey`、`TurnstileSecretKey`，开启 `TurnstileCheckEnabled`；保持 `turnstile_strict=true`（默认）。
- 预期行为：
  - 注册、发码（`GET /api/verification`）、重置密码邮件（`GET /api/reset_password`，注意不是提交新密码的 `POST /api/user/reset`）每次请求都必须携带有效 Turnstile token，重放/缺失/失败被拒（HTTP 200 + `success:false`，见 §1.3）；
  - 严格模式不读写 session；`turnstile_strict=false` 才回退 30 分钟会话复用（L5 语义，仅临时）。
  - **R2 解耦**：严格模式只由 `turnstile_strict` 控制，`registration_setting.enabled=false` **不再**使 Turnstile 回退会话复用；回滚 Turnstile 严格模式必须显式设 `turnstile_strict=false`。
- 注意：启用 Turnstile 后，**个人中心「绑定邮箱」发码**与**「忘记密码」发码**需要新版前端（带 token / 失败后重置 token）才能正常使用，必须确认 `web/default/dist` 已重建（T6 审查修复项）。
- 排错：日志 `[turnstile] verification failed for ip=...`；siteverify 超时 5 秒（独立 HTTP client），检查服务器出网/Cloudflare 可达性、SecretKey 与 SiteKey 是否同一 Turnstile widget。

### 3.5 设置注册防护阈值

- `registration_setting.enabled` 保持 `true`（默认）。
- 按业务设定 §2 中阈值；默认值即推荐初始值。
- 提醒：`RegisterEnabled`（注册总开关）与 `registration_setting.enabled`（防护总开关）是两个独立开关；本手册不改 `RegisterEnabled`。

### 3.6 表单令牌（前端就绪后再开）

开启前提（缺一不可）：

1. `web/default/dist` 已重建且包含表单令牌逻辑（新注册页会调用 `GET /api/user/register/token` 并在提交时带 `form_token`）；
2. `CRYPTO_SECRET` 已按 §3.1 配置且多实例一致；
3. **多实例必须启用 Redis**：Redis 模式下 nonce 一次性判定用 `SETNX` 完成；无 Redis 时 nonce 存于单进程内存，A 实例签发的令牌在 B 实例会被判无效（随机注册失败）。

开启后：

- `GET /api/user/register/token` 返回 `{token,min_seconds,expires_in}`；未开启时该接口 404；
- 缺失/签名错误/过早提交/过期/重放均返回 400 `user.registration_rejected`；
- Redis 故障时 nonce 校验 fail-open（记录日志，不阻塞注册）。

### 3.7 异步邮件

- 保持 `async_email_enabled=true`（默认）。`async_email_queue_size` / `async_email_workers` 调整后需重启。
- 上线后观察 `[mail-queue]` 日志与 §6 指标；SMTP 慢时注册/发码接口应快速返回（邮件在队列中异步发送）。
- **R2 真实发送峰值限速**：worker 在真正 SMTP 发送前取全站发信速率槽（`async_email_max_per_minute`）；达到上限时等待到下一分钟窗口，最长等待 60 秒，超时则丢弃该邮件并记 `[mail-queue] drop <kind> email (total dropped: N): ...`（N 为累计丢弃计数，`MailQueueDroppedCount()`；入队仍保留队列满/熔断背压 → 503）。
- **R2 同步回退**：`async_email_enabled=false` 时同步发送也经过同一限速/熔断（共享 `mailSendGate`），可能返回 429 `user.email_send_rate_limited` 或 503 `user.email_send_unavailable`；无队列、无重试，SMTP 慢会阻塞请求。
- 注意：重启会丢弃队列中尚未发送的邮件（用户需重新获取验证码）。

### 3.8 Redis 与多实例

- 限流计数、验证码、表单令牌 nonce 在 Redis 启用时跨实例共享；**例外：INV 邀请码失败计数（`inviteAttempts:<ip>`）为单实例内存 `InMemoryRateLimiter`，Redis 不共享**，多实例 INV 需 WAF/反代前置限速或后续扩展；无 Redis 时均为单进程内存实现。
- Redis 故障策略：注册/发码/验证码错误计数、表单令牌 nonce 校验均 **fail-open 并记录日志**（`[registration-guard] redis ... failed (fail-open)`），验证码存储回退内存；不会因 Redis 故障整体拒绝注册。
- 建议：Redis 开启持久化与内存告警；变更配置后等待一个 `SYNC_FREQUENCY` 周期或逐实例重启。

### 3.9 反向代理与 WAF 建议

- `TRUSTED_PROXIES` 必须与实际代理层匹配；CDN/WAF 需正确传递真实客户端 IP。
- WAF 建议：
  - 对 `POST /api/user/register`、`GET /api/verification`、`GET /api/reset_password` 配置速率限制（阈值应**高于**应用阈值，避免掩盖应用层 429 日志）；
  - 启用 Turnstile 前置（Cloudflare Bot Management）；
  - **siteverify 外呼保护**：发码/重置改为 Turnstile 前置后，未通过 Turnstile 的请求也会先触发 siteverify 外呼；若未启用 `GlobalAPIRateLimit`（或其阈值偏松），建议在 WAF/反向代理层对 `/api/verification`、`/api/reset_password` 做**前置限速**，避免 siteverify 被刷（应用层 429 只发生在 Turnstile 之后）；
  - 基于 IP 信誉/威胁情报拦截；
  - **不要**缓冲/改写 429 与 JSON 响应，不要拦截或规范化 POST JSON body（蜜罐与表单令牌字段需原样到达后端）；
  - 不要对 `/api/user/register/token` 做缓存（一次性令牌）。
- 仅靠 WAF 不能替代应用内核：IP 白名单、邮箱维度冷却、全站预算仍需应用层配置。

### 3.10 发布构建（顺序：前端 → Go embed）

1. **前端**（正常权限，非 agent 沙箱）：
   ```powershell
   cd web/default
   npm run build          # 等价：bun run build；本机无 bun，统一用 npm
   ```
   确认 `web/default/dist/index.html` 与 `web/default/dist/static/` 存在，且产物包含 T6 文案（如 `Registration Protection`、`registration_form_token_enabled`）。
2. **后端**：Go 二进制通过 `//go:embed web/default/dist` 与 `//go:embed web/default/dist/index.html` 嵌入前端；必须先完成第 1 步再构建 Go（Dockerfile/CI 即此顺序）。
3. **验证**：用新二进制启动后检查 `/api/status` 6 字段、系统设置页出现「注册防护」section、注册页行为符合 §1.3（INV 前端见 §3.11）。

> **重要**：当前工作区 `web/default/dist` 是 2026-10-17 的旧构建（不含 T6/R2/INV 前端：设置页「注册防护」section、注册页蜜罐/表单令牌/Turnstile、8 位验证码、保存设置后 `/api/status` 立即刷新、6 locale 键集合一致化、邀请码字段与 OAuth/微信携带）。直接发布现有二进制会缺失上述前端逻辑，**发布前必须由用户/CI 在正常权限下重建 dist**。详见 §9.1。

### 3.11 仅限邀请码注册（INV，`invite_code_required`）

#### 3.11.1 目的、权限与邀请码来源

- 配置键：`registration_setting.invite_code_required`，**默认 `false`**（第 24 键），**独立于** `registration_setting.enabled`：关闭注册防护总开关不会关闭邀请码校验，反之亦然。
- 权限：系统设置页路由仅 `SUPER_ADMIN` 可进、保存走 `/api/option` RootAuth，即**仅 root 可见/可改**。
- 合法邀请码 = **任意现有用户的 `aff_code`（含管理员/root）**；INV 校验使用 `service.ValidateInviteCode`（`service/invite_code.go:37-38`：DB `Select id, aff_code` 后**应用层 `user.AffCode == code` 精确比较**，MySQL 默认 CI collation 下也不直接采信 DB 结果），`trim` 后**精确匹配、区分大小写**，**不记录邀请码明文**。`model.GetUserIdByAffCode` 仅用于**关闭邀请码时**的既有 `aff` 推荐推断（如 `controller/user.go:279`、`controller/oauth.go:294`），**不参与 INV 校验**。
- `aff_code` 由 `common.GetRandomString(4)` 生成（4 位字母数字）；需要时自动生成：
  - 用户钱包「邀请奖励」卡片显示邀请链接（`/register?aff=<aff_code>`），可复制/分享（即邀请码传播入口）；
  - 管理员：系统设置 → 认证 → 注册防护页底部「我的邀请码」显示/复制；数据来自 `GET /api/user/aff`，**root 的 `aff_code` 为空时该接口会自动生成一个 4 位码并落库**。
- 不新增表、不迁移数据；关闭开关即「吊销」全部邀请码的效果。

#### 3.11.2 开启 / 关闭行为矩阵

| 场景 | 开启（`invite_code_required=true`） | 关闭（默认 false） |
|---|---|---|
| 密码注册（邮箱验证关闭） | 必须携带有效邀请码；缺失/无效 → HTTP 200 `user.invite_code_required` / `user.invite_code_invalid`，**不建号** | 完全不校验；请求里残留 `invite_code`/`aff` 不影响 |
| 邮箱验证码发送（`GET /api/verification?purpose=register`） | 顺序 Turnstile → **邀请码** → 邮箱配额 → 发信；缺/错码不发信、不消耗邮箱配额；失败超 20 次/600s → 429 | 跳过邀请码校验；仍走 Turnstile → 邮箱配额 → 发信；发码 purpose 仍为注册专用 `"vr"`（旧客户端未带 `purpose` 时写 `"v"`，关闭态注册可回退 `"v"`） |
| 密码注册（邮箱验证开启） | 先校验邀请码，再校验邮箱验证码；注册**只接受 `"vr"`** 验证码 | 不校验邀请码；注册验证码优先 `"vr"`，仅当无 `"vr"` 时才接受 `"v"` |
| OAuth 五条（GitHub/LinuxDO/OIDC/Discord/自定义 OAuth）与微信首登（共六条建号路径） | 注册页把邀请码随 OAuth state 携带，建号前校验；缺失/无效 → 200 错误文案、**不建号**；成功码的邀请人写入 `users.inviter_id` | 不校验；保留既有 `aff` 推荐逻辑 |
| 微信首登建号 | 微信登录 query 携带邀请码，建号前校验；规则同上 | 不校验；保留既有推荐逻辑 |
| 已有用户的 OAuth/微信登录、OAuth 账号绑定 | **不校验**（老用户直接登录） | 不校验 |
| 邮箱绑定（`POST /api/oauth/email/bind`） | 使用普通 `"v"` 验证码，**`"vr"` 不能用于绑定**；不校验邀请码 | 同左 |
| 密码找回（`/api/reset_password` → `/api/user/reset`） | 不受邀请码影响（`"r"` 用途） | 不受影响 |
| 管理员后台建号（`POST /api/user/` CreateUser 等） | **不受限**（CreateUser/UpdateUser 均不校验邀请码） | 不受限 |
| 注册页 UI | 显示「邀请码」输入框；未填时禁发码、禁第三方/微信并提示；localStorage 预填/写回；可用 `aff` 链接预填 | 隐藏输入框、清除残留 localStorage、不随请求发送 |
| `GET /api/status` | `invite_code_required=true` | `false` |

#### 3.11.3 顺序、purpose 与归因

- **发码顺序**：`TurnstileCheckStrict → RegistrationInviteGuard（仅 purpose=register）→ EmailSendRateLimit（邮箱冷却/日上限/IP/全站）→ 发信`；未过邀请码不会打满邮箱配额、不会发信。
- **注册顺序**：邀请码（controller 内，邮箱验证码校验之前）→ 邮箱验证码 → 用户存在性/占用检查 → 建号 → `RecordRegistrationSuccess`。
- **验证码 purpose**：注册用途写 `common.RegistrationEmailVerificationPurpose = "vr"`，其它用途（绑定邮箱等）写 `"v"`；注册成功后删除**实际使用**的 purpose 并清理两个 purpose 的错误计数。
- **归因**：密码注册与 OAuth/微信首登成功时，邀请人 ID 写入 `users.inviter_id`（0 也显式写入）；OAuth/微信建号同时仍受注册 guard（per-IP/全站上限）约束并计入成功计数。

#### 3.11.4 上线、切换与发布顺序

- **发布顺序（必须）**：先在正常权限下重建 `web/default/dist`（含邀请码输入与 OAuth/微信携带逻辑）并发布新二进制，**再**开启 `invite_code_required`。
- **旧前端 + 开启开关 = 阻断注册**：旧 dist（2026-10-17）没有邀请码字段与携带逻辑，开启后密码注册、OAuth/微信**新用户**会被后端拒绝（老用户登录不受影响）。
- **开 → 关（回滚）**：立即恢复不校验；已发出的 `"vr"` 验证码在关闭后仍可用于注册（关闭态优先 `"vr"`）。
- **关 → 开**：旧 `"v"` 验证码**不能**用于注册（只接受 `"vr"`），用户需在注册页重新发码；已打开的页面需刷新以拿到新的 `/api/status`（保存后前端会立即 invalidate，但未刷新的旧页面可能短暂 stale）。
- **中间态**：切换期间新旧请求可能分别走「需码/不需码」逻辑；建议在低峰期切换，观察注册 429/邀请码错误指标。
- **关闭态残留（已收窄，task-41）**：注册页关闭态已清除 localStorage 且不传码；残余仅「登录页直达 OAuth 或用户未重新访问注册页」时旧 localStorage 可能仍被发送——后端关闭态完全忽略（不影响注册），access log 已掩码；旧 dist 未更新前开启会阻断注册（先前端、后开关）。

#### 3.11.5 安全与风险

- 邀请码可复用、**无过期时间、无使用次数限制、不支持单独吊销**（只能关闭开关；或直接改数据库 `users.aff_code`，无 UI/API 支持轮换）。
- 4 位字母数字码（约 1470 万组合；`common.GetRandomString(4)`）存在被枚举/猜中的风险，现有防护：Turnstile、发码接口邀请码失败限流 **20 次/600s/IP**、注册接口 `RegistrationRateLimit`（默认 10 次/600s；**仅在 `registration_setting.enabled=true` 且阈值 >0 时生效——`enabled=false` 时 `CheckRegistrationAttempt` 直接放行，注册接口只剩 CriticalRateLimit/GlobalAPIRateLimit**）、验证码错误锁定；建议生产同时用 WAF/反向代理限速。
- 邀请码失败限流为**单实例内存实现**（`InMemoryRateLimiter`，非 Redis、非配置项）：多实例部署各实例独立计数，重启清零；**启用 Redis 也不会共享该计数**，多实例需 WAF/反代前置限速或后续扩展（Redis 化）。
- 邀请码校验失败**无专项 SysLog**（仅 HTTP 响应与 access log 可见）；access log 掩码仅匹配字面 `invite_code` 参数（`invite%5Fcode` 等 percent-encoded key/大小写变体为残余，见 §9.2）。
- 不新增表/迁移；关闭开关即等效吊销全部邀请码。

---

## 4. 发布前验证与构建顺序

### 4.1 Go 构建与静态检查

```powershell
# 沙箱/受限环境下必须把缓存指向 %TEMP%（默认 GOCACHE 可能不可写）
$env:GOCACHE = Join-Path $env:TEMP "gocache-release"
$env:GOTMPDIR = Join-Path $env:TEMP "gotmp-release"
go build ./...
go vet ./...

# 不覆盖仓库产物：
go build -o "$env:TEMP\heqiuyu-check.exe" .
```

- 本机 `GOMODCACHE`（`C:\Users\何\go\pkg\mod`）不可写时，stderr 会出现 `writing stat cache: ... Access is denied`，属**良性噪音**，以退出码与是否真正编译失败为准（计划 §6.1）；不得因此修改 `go.mod/go.sum`。
- Go 版本：`go.mod` 要求 `go 1.27.2`；本机 PATH 上为 go1.27.1，但 `GOTOOLCHAIN=auto` 且 `golang.org/toolchain@v0.0.1-go1.27.2` 已在模块缓存，会自动切换（已验证构建成功）。

### 4.2 前端检查与构建验证

本机无 bun（`web/default` 的 `test` 脚本依赖 bun，前端测试无法在本环境运行；前端验收以 typecheck + lint + build + 代码审查为准）。

```powershell
cd web/default

# 沙箱等价 typecheck（npm run typecheck 即 tsc -b，会写 node_modules/.tmp 被拒）
node node_modules/typescript/bin/tsc -p tsconfig.app.json --noEmit --tsBuildInfoFile "$env:TEMP\tsc-app-release.tsbuildinfo"
node node_modules/typescript/bin/tsc -p tsconfig.node.json --noEmit --tsBuildInfoFile "$env:TEMP\tsc-node-release.tsbuildinfo"

npm run lint

# 构建验证：输出到临时目录，不写仓库 dist（仓库 dist 受 ACL 限制且发布需正常权限重建）
Remove-Item -Recurse -Force "$env:TEMP\dist-verify" -ErrorAction SilentlyContinue
node node_modules/@rsbuild/core/bin/rsbuild.js build --dist-path "$env:TEMP\dist-verify"
# 期望：exit 0；检查 $env:TEMP\dist-verify\index.html 与 static\ 存在，且 JS 内含 T6 文案
```

已知沙箱噪音（**不是代码缺陷**）：

- `Rspack persistent cache reset failed ... 拒绝访问`：`node_modules/.cache/rspack/...` 目录因 ACL 无法删除，Rsbuild 本次会话禁用持久缓存，构建仍成功；
- `The dist path is not a subdir of root path, Rsbuild will not empty it`：`--dist-path` 指向 `%TEMP%`，Rsbuild 不会自动清空，**每次验证前请手动删除旧临时目录**，避免混入上次产物。

发布构建（必须由用户/CI 在正常权限执行）：

```powershell
cd web/default
npm run build
# 或与 CI/Dockerfile 一致：bun install --frozen-lockfile && bun run build
```

### 4.3 手工冒烟清单

在测试环境按开关组合逐项验证（当前线上注册关闭，以下需用户批准后执行）：

| # | 场景 | 操作 | 预期 |
|---|---|---|---|
| 1 | 注册关闭 | 打开注册页 / 调 `/api/status` | `register_enabled=false`，前端提示注册关闭 |
| 2 | 注册限流窗口 | 同 IP 连续发 11 次注册请求 | 第 11 次 429 + `Retry-After` + `user.registration_rate_limited` |
| 3 | 发码冷却 | 同邮箱 60 秒内二次发码 | 二次 429 + `Retry-After` |
| 4 | 验证码锁定 | 同一邮箱连续输错 5 次验证码 | 第 5 次 429 + `user.verification_attempts_exceeded`，验证码被删 |
| 5 | 验证码一次性 | 注册成功后用同一验证码再次注册 | 失败（码已删除） |
| 6 | 蜜罐 | `website` 非空提交注册 | 400 `user.registration_rejected` |
| 7 | form_token 关闭 | 不带 `form_token` 注册 | 正常（默认关闭） |
| 8 | form_token 开启 | 缺 token / 过期 / 重放 / 早于 min_seconds | 400 `user.registration_rejected`；token 接口关闭时 404 |
| 9 | Turnstile 严格 | 无 token / 重放 token | 拒绝（HTTP 200 + `success:false`） |
| 10 | 只开 Turnstile | 不开邮箱验证，前端注册页 | 正常渲染并可完成注册 |
| 11 | 异步邮件 | 注入 SMTP 延迟/故障（测试环境） | 接口快速返回；队列满/熔断时 `/api/verification` 503 且码被删；`/api/reset_password` 仍返回成功 |
| 12 | 注册 body 16KB（R2） | 提交 >16KB 的注册 JSON（`Content-Length` 已知） | 413 `user.request_body_too_large`；正常大小仍成功 |
| 13 | Turnstile 不计配额（R2） | Turnstile 失败/缺 token 调 `/api/verification` 多次 | 每次都在 Turnstile 被拒（200 + `success:false`），**邮箱配额/短窗计数不被消耗**（随后携带有效 token 可正常发码） |
| 14 | 邮箱大小写（R2） | 用 `User@x.com` 与 `user@x.com` 先后注册 | 第二次返回 `user.exists`；历史大写邮箱可被发码/重置识别 |
| 15 | EmailBind 一次性（R2） | 绑定成功后重放同一验证码 | 失败（码已删除） |
| 16 | 同步回退限速（R2） | `async_email_enabled=false` + 触发速率/熔断 | `/api/verification` 返回 429 或 503（不再绕过限速/熔断） |
| 17 | OAuth 首登限流（R2） | OAuth 新用户建号达到 per-IP 限额 | 返回 200 失败 JSON `user.registration_rate_limited`，不建号；既有用户登录不受影响 |
| 18 | 邮箱唯一索引（R2，SQLite 测试环境） | 启动后直插重复非空邮箱 / 多条空邮箱 | 重复非空邮箱插入失败；空邮箱可多条；有历史重复时启动日志给出跳过告警 |
| 19 | 状态字段 | `/api/status` | 6 个新字段齐全且值符合开关（含 `invite_code_required`） |
| 20 | INV 开启-密码注册（INV） | 开启 `invite_code_required` 后：无码/错码/有效码分别提交注册 | 无码/错码 → 200 `user.invite_code_required`/`invalid` 且不建号；有效码 → 正常建号且 `users.inviter_id`=码所属用户 |
| 21 | INV 开启-发码（INV） | 开启后 `GET /api/verification?purpose=register` 无码/错码/有效码 | 无码/错码 → 200 错误文案且不发信、不消耗邮箱配额；有效码 → 发信且验证码 purpose 为 `"vr"`；失败超 20 次/600s → 429 |
| 22 | INV purpose 隔离（INV） | 用 `"vr"` 验证码做邮箱绑定 / 用旧 `"v"` 验证码注册 | `"vr"` 不能绑定；开启态 `"v"` 不能注册（仅接受 `"vr"`）；关闭态优先 `"vr"`、无 `"vr"` 才接受 `"v"` |
| 23 | INV OAuth/微信首登（INV） | 开启后无码/有效码走 OAuth 或微信新用户建号 | 无码/错码 → 200 错误文案且不建号；有效码 → 建号且 `inviter_id` 正确；老用户登录不校验 |
| 24 | INV 关闭（INV） | 关闭后带任意残留 `invite_code`/不带码注册、发码 | 完全不校验、正常注册/发码；`/api/status.invite_code_required=false`；注册页隐藏邀请码输入 |
| 25 | 回归 | 登录、2FA、OAuth、找回密码、管理后台、用户列表 | 无异常 |

### 4.4 格式检查

- `gofmt -l` 对本次改动的 Go 文件必须为空；
- `npx prettier --check` 仅检查本次改动的前端文件（不要全仓检查）。

---

## 5. 回滚开关与副作用

按风险从低到高排列；所有开关均为热生效（`async_email_queue_size/workers` 除外）。

| # | 操作 | 影响 | 副作用 / 注意 |
|---|---|---|---|
| 1 | 放宽/置 0 单项阈值（如 `ip_max_attempts_per_window=0`） | 仅关闭对应限流项 | 已写入的窗口计数不追溯；`0` 语义是「关闭该项」，不是「最严格」 |
| 2 | `honeypot_enabled=false` | 关闭蜜罐拒绝 | 前端不再渲染该隐藏字段（注册页仅在 `honeypotEnabled` 时渲染 `website` 字段）；后端也跳过拒绝 |
| 3 | `form_token_enabled=false` | 关闭表单令牌，token 接口立即 404 | 已签发令牌作废；前端需能处理 404（新版前端已处理） |
| 4 | `invite_code_required=false`（关闭 INV） | 立即停止邀请码校验（密码注册/OAuth/微信首登） | 注册页隐藏邀请码字段并清除 localStorage；已发出的 `"vr"` 在关闭态仍可用于注册（优先 `"vr"`）；旧 dist 也可恢复注册；**不影响**其它注册防护开关 |
| 5 | `turnstile_strict=false` | 退回「校验一次、会话复用 30 分钟」 | 即 L5 语义：一次通过的会话在 30 分钟内重复注册/发码不再校验，**仅临时回滚**；R2 后这是回滚 Turnstile 严格模式的**唯一**方式（`registration_setting.enabled` 不再影响它）；需尽快恢复 |
| 6 | `async_email_enabled=false` | 同步发送回退 | **阻塞请求**：SMTP 慢/故障时注册、发码、重置请求同步等待并可能失败；R2 后同步路径**仍受全站发信限速与熔断约束**，可能返回 429 `user.email_send_rate_limited` 或 503 `user.email_send_unavailable`；无队列、无重试；`/api/reset_password` 仍返回成功（防枚举） |
| 7 | `registration_setting.enabled=false` | 关闭注册防护（配额类） | 注册限流、验证码尝试限制、蜜罐、表单令牌、OAuth 首登建号 guard 失效；发码防护的邮箱冷却/日上限、IP 小时上限、全站小时预算失效，**但基线「单 IP 30 秒 2 次」短窗仍生效（无条件先执行、不读该开关，回滚后仍可能 429）**；Turnstile 严格模式**不受影响**（R2 解耦，回滚 Turnstile 用 `turnstile_strict=false`）；**不影响** `invite_code_required`（INV 独立）；异步邮件仍由 `async_email_enabled` 控制；**不改变** `RegisterEnabled` 注册总开关，也不影响启动时的邮箱唯一索引 |
| 8 | 移除 `REDIS_CONN_STRING`（环境级） | 退化为单进程内存实现 | 多实例部署下各实例独立计数、验证码/nonce 不共享，**不要在多实例上这么做**；重启清零；INV 的邀请码失败计数同样为单实例内存（重启清零） |
| 9 | 回退二进制/镜像版本 | 恢复旧行为 | DB 中残留的 `registration_setting.*` options 会被旧版本忽略（旧版本未注册该配置模块），不影响启动；建议保留，便于再次上线 |

回滚流程建议：

1. 先在设置页改单个开关（优先低风险项），观察日志 1–2 分钟；
2. 多实例等待一个 `SYNC_FREQUENCY`（默认 60 秒）或逐实例重启；
3. 若涉及发信，确认已入队邮件情况（重启会丢队列）；
4. 记录变更：时间、操作人、开关、原因、观察结果，便于复盘。

---

## 6. 监控与日志

### 6.1 日志位置

- 服务日志：`logs/heqiuyu-<yyyymmddHHMMSS>.log`（启动时创建/轮转，可用 `-log-dir` 改目录），同时输出到 stdout（容器环境看容器日志）。
- Gin access log 与 `[SYS]` 日志写入同一 writer；`SysError` 写 stderr writer。

### 6.2 关键字与含义

| 关键字 / 片段 | 来源 | 含义与处置 |
|---|---|---|
| `[registration-guard] registration rejected (honeypot)` | `registration-payload-guard.go` | 蜜罐命中；观察来源 IP 与频率，必要时 WAF 处置 |
| `[registration-guard] registration rejected (missing form_token)` / `(form_token: ...)` | 同上 | 表单令牌缺失/无效/过期/重放；若大量出现且真实用户受影响，检查前端是否为最新 dist、CRYPTO_SECRET 是否多实例一致 |
| `[registration-guard] email send limited ip=... reason=...` | `email-send-rate-limit.go` | 发码被限流；`reason` 取值：`email_cooldown`、`email_day`、`email_ip_hour`、`email_global_hour` |
| `[registration-guard] email send burst limited ip=...` | 同上 | 命中「单 IP 30 秒 2 次」短窗 |
| `[registration-guard] email send burst counter redis error ...` | 同上 | Redis 计数异常，回退内存实现 |
| `[registration-guard] redis ... failed (fail-open)` | `registration_guard.go` | Redis 故障 fail-open；关注 Redis 可用性/延迟，计数准确性下降 |
| `[mail-queue] enqueue verification email failed for <masked>: ...` | `controller/misc.go` | 入队失败；已删验证码；`ErrMailRateLimited`→429，否则→503 |
| `[mail-queue] enqueue password reset email failed for <masked>: ...` | 同上 | 重置邮件入队失败；接口仍返回成功（防枚举），需关注 SMTP/队列健康 |
| `[mail-queue] retrying <kind> email after failure (attempt 1/1): ...` | `service/mail_queue.go` | 首次失败后 2 秒重试一次 |
| `[mail-queue] send <kind> email failed (consecutive failures: N): ...` | 同上 | 连续失败计数增加；达到 `async_email_breaker_failures` 即熔断 60 秒 |
| `[mail-queue] circuit breaker opened for 1m0s after N consecutive failures` | 同上 | **熔断已触发**；期间发码返回 503，需立即排查 SMTP |
| `[turnstile] verification failed for ip=...: ...` | `middleware/turnstile-check.go` | Turnstile 校验失败（token 无效/超时/网络）；关注失败率与 siteverify 可达性 |
| `[GIN] ... /api/verification?invite_code=***...`（access log） | `middleware/logger.go` | INV 邀请码 query 值已掩码为 `***`（覆盖 `/api/verification`、`/api/oauth/state`、`/api/oauth/wechat` 等）；**残余**：仅匹配字面 key `invite_code`，percent-encoded `invite%5Fcode` 与大小写变体不会被掩码 |
| `registration form token store exceeded 10000 entries; evicted N oldest entries` | `service/registration_token.go` | 单进程 nonce 表达到上限并逐出（多为脚本刷 token）；建议限流/WAF 处置 |
| `failed to store verification code in redis, falling back to in-memory store` | `common/verification.go` | Redis 写入验证码失败，回退内存（多实例下该验证码仅本实例可见） |
| `failed to delete verification code in redis: ...` | 同上 | 删码失败；注意验证码可能仍可重放（极端情况） |
| `failed to send email to ***@domain: ...` | `common/email.go` | 发信失败；R2 起使用基线 `common.MaskEmail` 脱敏为 `***@域名`（保留域名、不保留首字符） |
| `[mail-queue] drop <kind> email (total dropped: N): ...` | `service/mail_queue.go` | worker 等待发信速率槽超过 60s（或队列停止）而丢弃该邮件；N 为进程内累计丢弃数（`MailQueueDroppedCount()`）；不重试，用户需重新获取 |
| `[registration-guard] skip email unique index: ... duplicate email group(s) exist` | `model/main.go` | 历史数据存在重复邮箱（大小写不敏感），跳过创建唯一索引；应用层 `LOWER(email)` 兜底，需人工清理后重启 |
| `[registration-guard] skip email unique index: ...`（其它原因：`duplicate check failed` / `inspect generated column failed` / `add generated column failed` / `inspect index failed`） | 同上 | 启动时唯一索引保障未完成；不阻断启动（函数返回 error，调用方忽略） |
| `[registration-guard] create email unique index failed: ...` | 同上 | 唯一索引创建失败（如权限/方言差异）；**日志原文无 `(ignored)` 字样**，不阻断启动，应用层兜底 |

> 脱敏两种实现（R2 findings 修正）：`common.MaskEmail`（`common/email.go` 发信失败日志）输出 `***@域名`（基线实现，**不保留本地部分首字符**，比计划文字“保留首字符与域名”更严格，按实现记录）；`controller/misc.go` 的本地 `maskEmail`（`[mail-queue]` 日志）输出 `a***@域名`（保留首字符）。两类日志均不含完整明文邮箱。

> 可观测性注意：**注册限流命中（`RegistrationRateLimit`）本身没有专项 `SysLog`**，只能通过 Gin access log 的 429 状态码与 `Retry-After` 头识别；`CriticalRateLimit`（基线）的 429 也无 JSON body。**INV 邀请码校验失败同样没有专项 SysLog**（仅 HTTP 响应与 access log），邀请码失败计数超过 20 次/600s 时返回 429 `user.invite_code_attempts_limit`。相关项已记录为已知可观测性限制（§9.2）。

### 6.3 建议告警

- 注册/发码/重置接口 429 比例突增（access log 或网关指标）；
- INV：`/api/verification?purpose=register` 的邀请码错误/429（`user.invite_code_attempts_limit`）突增，可能为枚举邀请码；同时关注注册 200 `user.invite_code_invalid` 比例；
- `[mail-queue] circuit breaker opened` 任意命中（立即告警）；
- `[mail-queue] ... failed` 连续出现、队列满导致的 503；
- `[registration-guard] redis ... failed (fail-open)` 次数突增；
- `[turnstile] verification failed` 比例突增（可能是配置错误或攻击）；
- 表单令牌 nonce 逐出日志（异常刷 token）；
- `[mail-queue] drop ...` 数量（发信预算被耗尽/worker 等待超时）；累计值可用 `MailQueueDroppedCount()` 暴露给监控；
- `[registration-guard] skip email unique index` / `create email unique index failed`（启动即告警，确认是否需人工清理重复邮箱）；
- Redis 连接/延迟告警。

### 6.4 Redis 键与 TTL（排查用）

| 键模式 | TTL | 说明 |
|---|---|---|
| `registration:guard:attempt:<ip>:<windowBucket>` | 到窗口结束 | 注册尝试计数（窗口内每个请求都计数） |
| `registration:guard:success:hour:<ip>:<hourBucket>` | 2h | 单 IP 每小时成功计数 |
| `registration:guard:success:day:<ip>:<dayBucket>` | 48h | 单 IP 每天成功计数 |
| `registration:guard:success:global:hour:<hourBucket>` | 2h | 全站每小时成功计数 |
| `registration:guard:email:cooldown:<purpose>:<hash>` | `email_cooldown_seconds` | 同邮箱发码冷却；`purpose`：`v`=邮箱验证码、`r`=密码重置 |
| `registration:guard:email:day:<purpose>:<hash>:<dayBucket>` | 48h | 同邮箱每日发码计数 |
| `registration:guard:email:ip:hour:<ip>:<hourBucket>` | 2h | 单 IP 每小时发码计数 |
| `registration:guard:email:global:hour:<hourBucket>` | 2h | 全站每小时发码计数 |
| `registration:guard:verify:fail:<purpose>:<hash>` | 验证码错误窗口（默认 600s） | 验证码错误计数；成功后删除 |
| `registration_form_token:<nonce>` | 令牌有效期 + 60s | 表单令牌一次性 nonce |
| `emailVerification:EV:<ip>` | 30s | 发码短窗计数（原 EmailVerificationRateLimit 语义） |
| `verify:<purpose>:<hash>` | 10 分钟 | 验证码本身（Redis 启用时） |

> 说明：`<hash>` 为 `sha256(规范化邮箱)` 前 16 字节的 hex（32 字符）；**键中**不出现明文邮箱。R2 起两类发信日志均脱敏但**格式不同**：`common/email.go` 发信失败日志用基线 `common.MaskEmail` → `***@域名`（不保留首字符）；`controller/misc.go` 的 `[mail-queue]` 日志用本地 `maskEmail` → `a***@域名`（保留首字符）。均不含完整明文邮箱。
>
> INV 邀请码失败计数为**进程内存**（`inviteAttempts:<ip>`，`InMemoryRateLimiter`；不写 Redis、重启清零、多实例不共享），上限 20 次/600s。

### 6.5 自检

- `GET /api/status` 检查 6 个字段（含 `invite_code_required`）；
- 系统设置 → 认证 → 注册防护页可读取并保存 24 项配置（保存后热生效，前端立即刷新 `/api/status`）；右侧「我的邀请码」可读取/复制（`GET /api/user/aff`）；
- `GET /api/option` 可导出当前全部配置值（root 权限）；
- 注册 body 上限 16KB（提交超大 body 应得 413）、启动日志中的邮箱唯一索引告警、`[mail-queue] drop` 日志、access log 中 `invite_code=***` 掩码。

---

## 7. 垃圾账号处置 runbook（待执行）

> 状态：**待执行**。本次发布不执行任何垃圾账号处置；本节仅供后续（原方案第 9/10 步）按用户批准执行。

### 7.1 原则

- **只禁不删**：不删除用户、不删除其令牌/日志/账务记录（外键与审计需要）。
- **先备份**：处置前全量备份数据库（含 options）并记录备份路径与校验和。
- **留证据**：导出疑似账号清单、关联日志时间窗、处置操作记录，双人复核。
- **分批**：先小批量验证影响面，再分批处理；每批留观察窗口。

### 7.2 识别（只读）

```sql
-- 示例：按注册时间窗 + 邮箱域名聚合，人工复核后再处置
SELECT id, username, email, created_at, status
FROM users
WHERE created_at >= :start AND created_at < :end
ORDER BY created_at ASC;

-- 邮箱域名聚合（SQLite / MySQL 语法）
SELECT lower(substr(email, instr(email, '@') + 1)) AS domain, count(*) AS cnt
FROM users
WHERE created_at >= :start
GROUP BY domain
ORDER BY cnt DESC
LIMIT 50;

-- PostgreSQL 版本（PG 无 instr()，改用 split_part）
SELECT lower(split_part(email, '@', 2)) AS domain, count(*) AS cnt
FROM users
WHERE created_at >= :start
GROUP BY domain
ORDER BY cnt DESC
LIMIT 50;
```

> 注意数据现状（R2）：启动时会 best-effort 创建非空邮箱唯一索引，若历史存在重复邮箱（大小写不敏感）则跳过创建并告警；历史数据仍可能有大小写/重复邮箱，比对时使用 `lower(email)` 并人工复核，不要仅凭 SQL 结果批量处置。

### 7.3 处置

1. 确认注册开关已关闭（当前线上已关闭）；
2. 备份数据库并校验；
3. 导出疑似账号清单（id/username/email/created_at/最近活跃），双人复核；
4. 通过管理后台「用户管理」逐批**禁用**账号（`status=2`），保留记录；
5. 吊销/禁用相关令牌（按业务需要）；
6. 记录处置批次、数量、操作人、时间与复核人。

### 7.4 禁止事项

- 禁止 `DELETE FROM users` 或级联删除令牌/日志/账务；
- 禁止批量改密码/邮箱后不通知用户；
- 禁止修改历史账务数据；
- 禁止在未备份、未复核情况下执行批量更新。

---

## 8. 文件清单与 PR 拆分检查表

> 下列清单按计划 §3 作用域与实现核对整理，用于发布/审查对照；精确逐行差异以最终 `git diff` 为准。工作区当前为未提交状态。

### 8.1 第 5 步（T5，后端运行时防护）

新增：

- `middleware/registration-rate-limit.go`
- `middleware/registration-payload-guard.go`
- `middleware/email-send-rate-limit.go`
- `service/registration_guard.go`
- `service/mail_queue.go`

修改：

- `middleware/turnstile-check.go`（严格校验、5s 超时、会话标记时间戳 30 分钟）
- `middleware/email-verification-rate-limit.go`（清空为 Deprecated 空壳，部署时可删除）
- `controller/user.go`（注册链、验证码尝试限制、成功计数回写、删码、邮箱规范化、唯一冲突）
- `controller/misc.go`（发码/重置入队与失败处理、8 位验证码、删码）
- `model/user.go`（`IsEmailAlreadyTaken` 改 `>0` 等）
- `router/api-router.go`（三条路由链 + token 路由)

### 8.2 第 6 步（T6，契约、后端 i18n、前端）

新增：

- `setting/operation_setting/registration_setting.go`
- `service/registration_token.go`
- `controller/registration_token.go`
- `controller/registration_status.go`
- `web/default/src/features/system-settings/auth/registration-protection-section.tsx`

修改（后端）：

- `i18n/keys.go`（5 个 key；R2 另加 `user.request_body_too_large`，INV 另加 3 个 invite 相关 key，共 9 个，见 §8.3/§8.9）
- `i18n/locales/en.yaml`、`i18n/locales/zh-CN.yaml`、`i18n/locales/zh-TW.yaml`

修改（前端，已核对到的核心文件）：

- `web/default/src/features/auth/types.ts`（`RegisterPayload.website/form_token`、`SystemStatus` 字段；INV 另加 `invite_code` 与 `invite_code_required`）
- `web/default/src/features/auth/api.ts`（新增 `getRegistrationToken()`，404 时跳过全局错误提示（`skipErrorHandler`）；`sendEmailVerification` / `sendPasswordResetEmail` 的 `turnstile` 参数为 HEAD 基线已有，非本次新增）
- `web/default/src/components/turnstile.tsx`（`resetSignal`）
- `web/default/src/features/auth/hooks/use-turnstile.ts`、`web/default/src/features/auth/hooks/use-email-verification.ts`
- `web/default/src/features/auth/sign-up/components/sign-up-form.tsx`（注册页 Turnstile/蜜罐/form_token/失败重置/注册关闭提示）
- `web/default/src/features/system-settings/types.ts`、`.../auth/section-registry.tsx`、`.../auth/index.tsx`
- `web/default/src/i18n/locales/{en,fr,ja,ru,vi,zh}.json`（6 个 locale，新增键）

修改（T6 审查 A 修复项，2026-10-24 21:03 合入）：

- `web/default/src/features/auth/constants.ts`（新增 `EMAIL_VERIFICATION_CODE_LENGTH = 8`，与后端 8 位验证码一致）
- `web/default/src/features/profile/components/dialogs/email-bind-dialog.tsx`（验证码输入长度改为 8；绑定邮箱发码接入 Turnstile `turnstileToken`/失败后 `resetTurnstile()`，使用 HEAD 基线已有的 `turnstile` 参数）
- `web/default/src/features/auth/forgot-password/components/forgot-password-form.tsx`（忘记密码发码携带 Turnstile token，失败后重置；同样使用 HEAD 基线已有的 `turnstile` 参数）

> 上述修复已包含在 T7 最终构建验证中；发布前仍须由用户/CI 在正常权限下重新执行 `npm run build` 生成仓库 `web/default/dist`（若之后前端还有变更，需再次重建，否则见 §9.1 的失效风险）。

### 8.3 修复轮 2（R2，2026-10-24）增量文件

后端（R2）：

- 新增：`middleware/registration-body-limit.go`（16KB 请求体硬上限 + 413）
- 修改：`router/api-router.go`（注册链加 body limit；发码/重置顺序改为 Turnstile→配额）
- 修改：`middleware/registration-payload-guard.go`（body 超限 413；注册链中移到 Turnstile 之后）
- 修改：`middleware/turnstile-check.go`（`turnstile_strict` 与 `registration_setting.enabled` 解耦）
- 修改：`controller/user.go`（EmailBind 成功删码 + 清错误计数）
- 修改：`controller/github.go`、`controller/linuxdo.go`、`controller/oidc.go`、`controller/discord.go`、`controller/wechat.go`、`controller/oauth.go`（六条 OAuth 首登建号接入注册 guard）
- 修改：`model/user.go`（邮箱查询统一 `LOWER(email)` / 规范化）
- 修改：`model/main.go`（启动 best-effort 非空邮箱唯一索引；导出 `EnsureUserEmailUniquenessBestEffort(db)` 便于测试；重复/失败跳过并告警。MySQL 生成列已改为 `VARCHAR(50)`，与 `email` 列一致）
- 修改：`common/email.go`（发信失败日志改为调用**基线已有**的 `common.MaskEmail`；`common/str.go` 的 `MaskEmail` 实现本身未改，见 D1 已批准偏离）
- 修改：`common/rate-limit.go`（`InMemoryRateLimiter` 100000 键上限 + 最旧逐出 + `Close()`/`Size()`）
- 修改：`service/mail_queue.go`（`mailSendGate` 与队列解耦；worker 真实 SMTP 前取速率槽，超 60s 丢弃并日志；**新增累计丢弃计数 `mailDroppedTotal`/`MailQueueDroppedCount()`**；`async=false` 同步回退同样受限速/熔断）
- 修改：`service/registration_guard.go`（测试重置时先 `Close()` 旧内存 limiter，避免 goroutine 泄漏）
- 修改：`i18n/keys.go`、`i18n/locales/en.yaml`、`i18n/locales/zh-CN.yaml`、`i18n/locales/zh-TW.yaml`（`user.request_body_too_large`）

> 基线文件（**不列入上述修改清单**，R2 未改）：`common/str.go` 的 `MaskEmail`、`common/gin.go` 的 `IsRequestBodyTooLargeError`；R2 仅在 `common/email.go` 调用 `MaskEmail`，并在注册 body 上限中复用 `IsRequestBodyTooLargeError`。

前端（R2）：

- `web/default/src/features/system-settings/hooks/use-update-option.ts`（保存 `registration_setting.*` 后立即失效并刷新 `/api/status`）
- `web/default/src/i18n/locales/{en,fr,ja,ru,vi,zh}.json`（清理重复 `"heqiuyu"` 键、补齐 5 语言缺失的 2 个键；6 个 locale 键集合一致；INV 后又新增 10 键，当前各 3717 键）

测试（R2，第 8 步交付）：路由顺序（Turnstile 失败不消耗邮箱配额）、16KB→413、大小写邮箱重复拒绝、EmailBind 清码重放、`MaskEmail`、同步回退限速/熔断、内存 limiter 上限/逐出/`Close`、邮箱唯一索引（重复直插失败/空邮箱放行）、worker 峰值限速/丢弃计数、strict 与 enabled 解耦、nonce 逐出等；**OAuth 邀请码 mock 6 用例覆盖 generic 路径（含 `inviter_id`）；内置 legacy（GitHub/LinuxDO/OIDC/Discord）、custom、微信端到端由代码审查覆盖（Lead 批准）**。

### 8.4 第 7 步（T7，本次交付）

- `docs/registration-defense-runbook.md`（新增，本文件）
- `docs/README.md`（仅追加索引一行）

### 8.5 第 8 步（T8，测试与验收）已交付，见 acceptance §9

- `service/registration_guard_test.go`、`service/mail_queue_test.go`、`service/registration_token_test.go`
- `middleware/turnstile-check_test.go`、`middleware/email-send-rate-limit_test.go`
- `router/registration_defense_e2e_test.go`、`router/user_self_update_e2e_test.go`（更新）
- R2 新增：`common/rate_limit_test.go`、`router/registration_body_limit_test.go`、`model/user_email_unique_test.go`
- R2 新增/更新测试：路由顺序（Turnstile 失败不消耗邮箱配额）、16KB→413、大小写邮箱、EmailBind 清码、`MaskEmail`、同步回退限速/熔断、内存 limiter 上限/`Close`、邮箱唯一索引（重复直插/空邮箱）、worker 峰值限速/丢弃计数、strict 与 enabled 解耦、nonce 逐出等；**OAuth 邀请码 mock 6 用例覆盖 generic 路径（含 `inviter_id`）；内置 legacy（GitHub/LinuxDO/OIDC/Discord）、custom、微信端到端由代码审查覆盖（Lead 批准）**
- `docs/registration-defense-acceptance.md`（验收矩阵）已交付，见 acceptance §9

### 8.6 既有基线未提交改动（保留，不属于本功能）

以下 5 个文件在本功能开始前已有未提交改动，实施过程中只做增量、未回滚；拆分提交时需与本次功能改动分开（`controller/user.go`、`model/user.go` 同时含基线与本次改动，需按 hunk 拆分）：

- `controller/setup.go`
- `controller/user.go`
- `model/user.go`
- `model/user_column_write_test.go`
- `router/user_self_update_e2e_test.go`

### 8.7 PR 拆分建议

| PR | 内容 | 依赖 |
|---|---|---|
| PR1 | 后端注册防护核心（T5：middleware/service/router/controller/model） | PR2a（T6 后端契约）先合入 |
| PR2a | 后端契约（T6：registration_setting、后端 i18n、registration_token、registration_status） | 无 |
| PR2b | 前端（T6：前端类型/API、注册页、Turnstile 组件、系统设置「注册防护」页、6 个 locale） | PR2a、PR1 |
| PR3 | 测试与验收（T8：测试文件 + acceptance 文档） | PR1、PR2a、PR2b |
| PR4 | 文档与发布（T7：本 runbook + docs/README 索引） | PR1–PR3 |

合并顺序建议：**PR2a（后端契约）→ PR1（后端防护）→ PR2b（前端）→ PR3（测试）→ PR4（文档）**；每个 PR 内部保证 `go build`/typecheck 通过。之所以拆 PR2a/PR2b：后端契约（配置模块、i18n、token/status）是 PR1 编译与 PR2b 前端类型的前置，必须单独先行合入。修复轮 2（R2）的后端内容并入 PR1、前端设置缓存/i18n 并入 PR2b、测试并入 PR3，不单独新增 PR；**INV 同理**（后端 `service/middleware/controller` 并入 PR1，前端邀请码 UI 并入 PR2b，测试并入 PR3）。

### 8.8 发布前检查表

- [ ] `go build ./...`、`go vet ./...`、`go test -count=1 ./common/... ./setting/... ./service/... ./middleware/... ./controller/... ./model/... ./router/...` 通过（F6 后命令含 `./model/...`）
- [ ] `gofmt -l` 对本次改动 Go 文件为空
- [ ] 前端 typecheck（沙箱等价命令）/ `npm run lint` / 正常权限 `npm run build` 通过
- [ ] `web/default/dist` 已重建，且包含 T6 文案（`Registration Protection` 等）与 INV 邀请码 UI
- [ ] 邮箱绑定验证码 8 位、绑定邮箱/忘记密码 Turnstile token 修复已包含在前端产物中
- [ ] R2 冒烟：16KB 注册 body → 413；Turnstile 失败不消耗邮箱配额；OAuth 首登限流；`async=false` 同步回退 429/503；大小写邮箱重复被拒；EmailBind 重放失败
- [ ] INV 冒烟：开启后无码/错码注册与发码被拒且不发信；有效码建号 `inviter_id` 正确；OAuth/微信首登同规则；关闭后完全不校验；旧 dist 不会在开启态被误发布
- [ ] R2 启动日志无 `skip email unique index` / `create email unique index failed` 告警（或已确认历史重复邮箱并人工清理）
- [ ] R2/INV 前端 6 个 locale 键集合一致（各 3717 键）、prettier 通过
- [ ] 24 项配置默认值一致性测试通过（`InviteCodeRequired=false`）
- [ ] 无新增依赖（`go.mod/go.sum`、`package.json`/锁文件无意外变更）
- [ ] 无新增环境变量（本功能全部走 `registration_setting.*`）
- [ ] 无 secret 泄漏（gitleaks/人工检查）
- [ ] 既有 5 个基线未提交改动完整保留
- [ ] `.probe-write.txt` 已由用户删除；空壳 `middleware/email-verification-rate-limit.go` 部署时已删除
- [ ] 数据库/`.env`/镜像版本已备份
- [ ] 回滚方案（§5）已确认，关键开关可热回退
- [ ] §4.3 冒烟清单通过

### 8.9 仅限邀请码注册（INV，2026-10-25）增量文件

后端新增：

- `service/invite_code.go`（`InviteCodeRequired` / `ValidateInviteCode` / `ErrInviteCodeRequired` / `ErrInviteCodeInvalid`）
- `controller/invite_code.go`（校验委托、i18n 映射、OAuth session 邀请码读取）
- `middleware/registration-invite-guard.go`（`/api/verification?purpose=register` 邀请码校验 + 20 次/600s 单实例失败限流 + 测试重置钩子）

后端修改：

- `setting/operation_setting/registration_setting.go`（第 24 键 `invite_code_required`，默认 `false`）
- `controller/registration_status.go`（`/api/status` 增加 `invite_code_required`）
- `common/verification.go`（新增 `RegistrationEmailVerificationPurpose = "vr"`；新增 `HasVerificationCode`——用 Redis `EXISTS`（不可用时回退内存表）判断指定 purpose 验证码是否存在，供关闭邀请码时选择 `vr`/`v`、避免错误回退 `v`）
- `controller/user.go`（Register 邀请码校验/`vr`·`v` purpose/邀请人归因；`GetAffCode` 为**基线既有**能力（`/api/user/aff`，aff 为空时自动生成 4 位码），本轮未改，INV 仅在管理员设置页读取/展示时依赖它）
- `controller/misc.go`（`purpose=register` 写 `"vr"` + controller 侧邀请码复核）
- `controller/oauth.go`、`controller/wechat.go`、`controller/github.go`、`controller/linuxdo.go`、`controller/oidc.go`、`controller/discord.go`（首登建号邀请码校验 + `users.inviter_id` 归因）
- `router/api-router.go`（`/api/verification` 链插入 `RegistrationInviteGuard`）
- `middleware/logger.go`（access log `invite_code` query 掩码）
- `i18n/keys.go`、`i18n/locales/en.yaml`、`i18n/locales/zh-CN.yaml`、`i18n/locales/zh-TW.yaml`（`user.invite_code_required/invalid/attempts_limit`）

前端：

- `web/default/src/features/auth/types.ts`、`features/auth/api.ts`、`features/auth/sign-up/components/sign-up-form.tsx`、`features/auth/components/oauth-providers.tsx`、`features/auth/hooks/use-email-verification.ts`、`web/default/src/lib/oauth.ts`
- `web/default/src/features/system-settings/auth/registration-protection-section.tsx`（开关 + 「我的邀请码」查看/复制）、`.../types.ts`、`.../auth/index.tsx`、`.../auth/section-registry.tsx`
- `web/default/src/i18n/locales/{en,fr,ja,ru,vi,zh}.json`（10 个新键；6 个 locale 键集合一致，各 **3717** 键）

测试：

- `service/invite_code_test.go`、`controller/invite_code_test.go`、`middleware/logger_test.go`（邀请码掩码与残余）
- `router/registration_invite_e2e_test.go`（6 个测试：status 默认/切换、关闭态、开启态发码、开启态注册、purpose 隔离、验证码锁定；**不含 OAuth/微信**）
- `controller/login_session_test.go`（OAuth 邀请码 mock 6 用例：无码/错码/有效码/关闭无 aff/关闭带 aff/老用户；**微信与真实第三方端到端回调无自动化，代码审查覆盖**）
- `router/registration_body_limit_test.go`（`TestRegistrationDefaultConfigCopyMatchesProduct:34`：24 项默认值一致性、`InviteCodeRequired=false`；及 R2 16KB body e2e）
- `router/registration_defense_e2e_test.go`（R2 注册防护 e2e；**不含** 24 项默认值测试）
- 隔离复跑（acceptance task-49 F1）：`go test -count=2 -run 'TestOAuthInvite' ./controller/` 与 `go test -count=2 -run 'TestInviteCode' ./router/` 均通过（两轮均 PASS），另有 `-shuffle=on -count=1 ./controller/ ./router/` 通过

门禁命令：见 §4.1/§4.2；Go 全量测试用 `go test -count=1 ./common/... ./setting/... ./service/... ./middleware/... ./controller/... ./model/... ./router/...`；前端 typecheck/lint/build 同 §4.2。不改 dist、不 commit。

---

## 9. 已知限制与待处理项

### 9.1 本环境（agent 沙箱）限制 —— 非代码缺陷

- **文件 ACL**：工作区根 `E:\heqiuyu-api-main` 存在继承的 `Everyone:(CI)(DENY)(DC)`，agent 子进程不能删除/覆盖仓库内文件（含 `web/default/dist`、旧 middleware 文件）。因此：
  - 构建验证只能输出到 `%TEMP%\dist-verify`；**发布前必须由用户/CI 在正常权限下 `npm run build` 重建 `web/default/dist`**；
  - `.probe-write.txt` 需用户手工删除；
  - `middleware/email-verification-rate-limit.go` 现为仅含 Deprecated 说明的空文件，部署时可删除（不得再添加逻辑）。
- **无 bun**：本机只能用 npm；`web/default` 的前端测试脚本依赖 bun，前端测试无法在本环境运行（前端验收 = typecheck + lint + build + 代码审查）。
- **tsc 缓存**：`npm run typecheck`（`tsc -b`）会写 `node_modules/.tmp/*.tsbuildinfo` 被拒（EPERM），使用 §4.2 的等价命令。
- **Go 缓存**：默认 `GOCACHE` 不可写，需指向 `%TEMP%`；`GOMODCACHE` 不可写产生的 `writing stat cache: ... Access is denied` 属良性噪音。
- **无 Redis**：本机未设置 `REDIS_CONN_STRING`，Redis 分支未做运行时验证（接口/代码审查与单测覆盖，Redis 行为需部署环境验证）。
- **dist 陈旧**：仓库 `web/default/dist` 为 2026-10-17 构建，不含 T6/R2/INV 前端（设置页「注册防护」section、注册页蜜罐/表单令牌、8 位验证码、Turnstile 重置、保存设置后 `/api/status` 立即刷新、6 locale 键集合一致化、邀请码字段与 OAuth/微信携带等）。**发布前必须重建**，否则：设置页看不到注册防护 section、`form_token_enabled=true` 时前端不会取/带 token 导致注册 400、Turnstile 严格模式下绑定邮箱/忘记密码发码会失败、改设置后状态缓存最长 5 分钟不刷新、**INV 开启后旧前端无法提供邀请码导致注册被阻断**。

### 9.2 已知限制（R2 后仍存在）

> R2 已修复并**不再列为已知问题**：发码配额先于 Turnstile 消耗、注册 body 无上限、`common/email.go` 明文邮箱日志、EmailBind 成功不删码、邮箱大小写查询不一致、同步回退绕过限速/熔断、guard 内存计数无上限、前端 i18n 重复键/缺键（变更文件见 §8.3）。

- **DB 唯一索引为 best-effort**：启动时仅在历史数据无重复（大小写不敏感）时创建非空邮箱唯一索引；历史重复邮箱存在则跳过并告警，需人工清理后重启（应用层 `LOWER(email)` 查询兜底）。
- **DB 唯一索引对大小写敏感**：并发提交仅大小写不同的同一邮箱仍可能穿透 DB 索引（应用层查询兜底，极端并发下仍有残余竞态）。
- **MySQL 分支未实测**：生成列 `email_unique` + 唯一索引的实现仅经代码审查与非 MySQL 环境验证，需在 MySQL 环境实测。生成列已改为 **`VARCHAR(50)`**（与 `email` 列一致）；旧版 InnoDB 的索引键长限制风险仍存；MySQL 分支为 best-effort，失败/重复仅告警跳过、不阻断启动。
- **OAuth 语义细节**：仅「首登建号」受注册 guard 约束（per-IP 窗口/小时/日/全站成功上限 + 白名单）；既有用户 OAuth 登录/绑定不受影响；被拒时返回 HTTP 200 失败 JSON（`user.registration_rate_limited`，无 `Retry-After`）；OAuth 建号不经过 Turnstile/蜜罐/表单令牌/邮箱配额；**OAuth 邀请码 mock 6 用例覆盖 generic 路径（含 `inviter_id`）；内置 legacy（GitHub/LinuxDO/OIDC/Discord）、custom、微信端到端由代码审查覆盖（Lead 批准）**。
- **worker 60s 超时丢弃未做长时实测**：仅有 `MailQueueDroppedCount()` 计数钩子单测，**60s 丢弃分支未实测**（未做长时间高压实测）；生产需观察 `[mail-queue] drop ... (total dropped: N)` 的累计丢弃计数（`MailQueueDroppedCount()`）判断是否需调高 `async_email_max_per_minute`/worker 数。
- **MaskEmail 不保留首字符 = 已批准偏离（D1）**：`common.MaskEmail` 为基线实现，输出 `***@域名`（仅保留域名），比计划文字「保留首字符与域名」更严格；已批准按实现记录，不再改回保留首字符。
- **注册限流命中无专项日志**：`RegistrationRateLimit` 命中只能通过 Gin access log 429 与 `Retry-After` 识别（可观测性限制，本次未加日志）。
- **内存限流器逐出的有界降级**：`InMemoryRateLimiter` 达到 100000 键后按最久活跃逐出（约 10%，并先清理过期键）；被逐出的键可能包含登录失败锁定、注册 guard、发码限流等其他安全计数，导致短期内计数偏松（有界降级，非安全漏洞）。生产多实例建议启用 Redis（跨实例一致，且不受单进程键上限影响）。
- **i18n 两套体系/报告口径**：R2 已清理前端 6 个 locale 的重复键与缺键；INV 又新增 10 个邀请码相关键，6 个 locale 键集合一致（各 **3717** 键）。前端 JSON 与后端 yaml 仍为两套 i18n 体系，历史验收报告中的 i18n 目录/口径记录保留，本次不合并。
- **设计取舍（有意保留）**：`registration_setting.enabled=false` 时「单 IP 30 秒 2 次」发码短窗仍生效（基线保护）；Turnstile 校验失败返回 HTTP 200（基线行为）；`async=false` 同步回退仍为同步阻塞，但已受限速/熔断约束；`common.MaskEmail` 见上方「已批准偏离（D1）」。
- **测试期/环境项**：nonce 逐出、e2e 默认配置复制一致性等测试覆盖由第 8 步验收文档（`docs/registration-defense-acceptance.md`）记录；本机无 Redis/无 bun/沙箱 ACL 见 §9.1。

**审查 A 识别、本轮未修的残余风险（如实记录）：**

- **[中] `LOWER(email)` 使 `users.email` 原索引失效**：`ValidateAndFill`（登录）、`CheckUserExistOrDeleted` 等改为函数比较 `LOWER(email) = ?` 后，可能无法使用 `users.email` 上的原有索引（无表达式索引），用户量大时有全表扫描/CPU 放大风险。建议生产启用 `GlobalAPIRateLimit`/WAF，或后续在 best-effort 迁移中增加 `LOWER(email)` 表达式索引（SQLite/PG 原生表达式索引、MySQL 生成列/函数索引）；**本轮未做**。
- **[中] Turnstile 前置后通过前无应用层限速（条件性）**：`GlobalAPIRateLimit` **默认开启**（180 次/180s/IP，可由 `GLOBAL_API_RATE_LIMIT_ENABLE` 关闭；api 组已全局挂载）；`/api/verification` **未单独挂** `CriticalRateLimit`（`/api/reset_password` 有）。若生产关闭 `GlobalAPIRateLimit`，则 Turnstile 通过前无应用层限速，未认证请求仍可触发 siteverify 外呼；仍建议 WAF/反代前置限速降低 siteverify 外呼风险（见 **§3.9「siteverify 外呼保护」**）。
- **[低] EmailBind / OAuth 建号唯一索引冲突未统一映射**：`Register` 已把唯一约束冲突映射为 `user.exists`，但 EmailBind 与 OAuth 首登建号在 DB 唯一索引冲突时仍可能把驱动错误文本返回前端；建议后续统一映射。
- **[低] `mail_queue` 重试与发信速率槽**：失败重试（首次 + 1 次）只在 `acquireSendSlot` 时占用 1 个全站速率槽，实际 SMTP 尝试次数最多为预算的 2 倍；如需严格峰值，重试也应占槽。
- **[低] `common.MaskEmail` 多 `@` 输入**：对 `a@b@c` 类输入输出 `***@b@c`（取第一个 `@` 之后内容），极端输入可能把非域名内容带入日志；建议后续按最后一个 `@` 截断。
- **[信息] OAuth 首登占用注册配额**：OAuth 首登 guard 使用 `c.ClientIP()`，与密码注册共享 per-IP 窗口/成功计数——运营需知 **OAuth 首登也会消耗注册配额**（达到上限后 OAuth 新用户建号与密码注册同样被拒）。

**仅限邀请码注册（INV）的已知限制（如实记录）：**

- 邀请码可复用、**无过期、无使用次数、不支持单独吊销**（只能关闭开关；或直接改数据库 `users.aff_code`，无 UI/API 支持轮换）；3.11.5 已写明。
- 4 位字母数字码存在被枚举风险；发码接口的邀请码失败限流为 **20 次/600s/IP 单实例内存**（多实例不共享、重启清零、非配置项）；注册接口的邀请码校验**不经过**该失败限流，只受 `RegistrationRateLimit`（默认 10 次/600s；**仅在 `registration_setting.enabled=true` 且阈值>0 时生效——`enabled=false` 时 `CheckRegistrationAttempt` 直接放行，注册接口只剩 CriticalRateLimit/GlobalAPIRateLimit**）等约束；建议 WAF/反代前置限速。
- 邀请码校验失败**无专项 SysLog**；access log 掩码只匹配字面 `invite_code`，percent-encoded key（`invite%5Fcode`）与大小写变体为残余；OAuth/微信的邀请码失败同样只有 HTTP 响应可观测。
- 旧 dist（2026-10-17）不含邀请码 UI 与携带逻辑，**未重建前开启会阻断密码注册与 OAuth/微信新用户**（见 §3.11.4）；关闭态 localStorage 残留已收窄（注册页关闭态清除；残余仅登录页直达 OAuth/未重访注册页时旧值可能仍被发送，后端关闭态完全忽略，access log 已掩码）。
- 未新增表/迁移；INV 校验为普通 `aff_code` 查询 + 进程内存失败计数，**MySQL/PG 与多实例路径未专门实测**；INV 失败计数**不随 Redis 共享**，多实例需 WAF/反代前置限速或后续扩展（Redis 化），不能依赖「启用 Redis」解决。
- **前端登录页直达第三方的无码拒绝/提示无自动化**：本机无 bun，前端仅能 typecheck/lint/build；后端 OAuth mock 覆盖「无码/错码拒绝且不建号」，前端提示/跳转需人工冒烟（acceptance §9.4-9，未做端到端验证）。
- 「登录页直达第三方」的新用户无邀请码时会被拒绝，需从注册页填写邀请码后再走第三方（符合设计，运营需知）。

### 9.3 待用户处理项

- 删除工作区根 `.probe-write.txt`；
- 部署时删除空壳文件 `middleware/email-verification-rate-limit.go`；
- 在正常权限下执行 `npm run build` 重建 `web/default/dist` 后再发布（**INV 开启前必须先重建**，否则旧前端会阻断注册）；
- 历史重复邮箱需人工清理后重启，以创建 best-effort 邮箱唯一索引；MySQL 部署需实测唯一索引分支（生成列 + 唯一索引）；
- 多实例部署若启用 INV，建议在 WAF/反代对 `/api/verification`、`/api/user/register` 做邀请码枚举前置限速（应用层失败计数为单实例内存）；
- 根 `README.md` 的文档索引未包含本 runbook（不在 T7 写作用域，Lead 汇总为待批准小改动）。

---

## 10. 附录：快速命令模板

```powershell
# ===== 项目根：E:\heqiuyu-api-main\heqiuyu-api-main =====
$env:GOCACHE = Join-Path $env:TEMP "gocache-release"
$env:GOTMPDIR = Join-Path $env:TEMP "gotmp-release"

go build ./...
go vet ./...
go build -o "$env:TEMP\heqiuyu-check.exe" .

# ===== 前端：cd web/default =====
node node_modules/typescript/bin/tsc -p tsconfig.app.json --noEmit --tsBuildInfoFile "$env:TEMP\tsc-app.tsbuildinfo"
node node_modules/typescript/bin/tsc -p tsconfig.node.json --noEmit --tsBuildInfoFile "$env:TEMP\tsc-node.tsbuildinfo"
npm run lint

# 构建验证（不写仓库 dist）
Remove-Item -Recurse -Force "$env:TEMP\dist-verify" -ErrorAction SilentlyContinue
node node_modules/@rsbuild/core/bin/rsbuild.js build --dist-path "$env:TEMP\dist-verify"
Test-Path "$env:TEMP\dist-verify\index.html"
(Get-ChildItem "$env:TEMP\dist-verify\static" -Recurse -File).Count

# 发布构建（用户/CI 正常权限）
npm run build

# 监控（示例，按实际日志目录调整）
Select-String -Path logs\*.log -Pattern '\[registration-guard\]','\[mail-queue\]','\[turnstile\]' |
  Select-Object -Last 50
```

### 本环境构建验证记录（2026-10-24，T7）

| 项 | 结果 |
|---|---|
| `rsbuild build --dist-path %TEMP%\dist-verify`（修复前源码，21:01 左右） | 退出码 0，53.2 秒，产物 `index.9953e02e23.js`，总计 13,086.7 kB（gzip 3,266.9 kB） |
| `rsbuild build --dist-path %TEMP%\dist-verify`（T6 审查 A 修复后重跑，21:05 左右） | 退出码 0，产物 `index.d305976338.js`，总计 13,087.1 kB（gzip 3,267.1 kB）；`index.html` 1.1 kB、`static/` 共 119 个文件；产物 JS 含 `Registration Protection` / `registration_form_token_enabled` 等 T6 文案 |
| `go build -o %TEMP%\heqiuyu-check.exe .`（`GOCACHE`/`GOTMPDIR` 指向 `%TEMP%`） | 退出码 0，产物 84,218,880 字节；stderr 仅有 `writing stat cache: ... Access is denied` 良性噪音（GOMODCACHE 不可写） |
| 仓库 `web/default/dist` / `heqiuyu-api.exe` | **未被本轮验证改动**（dist 仍为 2026-10-17 15:47:18 / 1096 字节；exe 仍为 2026-10-17 16:05:10） |
| Rsbuild 已知噪音 | `Rspack persistent cache reset failed ... 拒绝访问`（`node_modules/.cache/rspack` 无法删除，本次会话禁用持久缓存后仍构建成功）；`The dist path is not a subdir of root path, Rsbuild will not empty it`（临时目录需每次手动删除） |

结论：前端构建链路（临时 dist）+ Go embed 构建在本沙箱可用；仓库内 `dist` 必须由用户/CI 在正常权限下重建后才能发布（见 §9.1）。

> **R2 说明**：修复轮 2（2026-10-24）改动了后端路由/中间件/邮件队列/模型与前端设置缓存、6 个 locale，上表为 T7 首轮（R2 之前）的构建记录；R2 后的完整构建/测试验证由第 8 步（T8）验收与 Lead 门禁重跑覆盖，发布时以最新 `npm run build` + `go build` 结果为准。

> **INV 说明**：仅限邀请码注册（2026-10-25）新增后端 `service/controller/middleware`、第 24 配置键、i18n 3 键与前端邀请码 UI/6 locale 10 键；同样**未在本记录中重跑构建**，构建/测试由 T8 与 Lead 门禁覆盖；INV 开启前必须先重建 `web/default/dist`（见 §3.11.4）。

---

## 变更记录

| 日期 | 变更 |
|---|---|
| 2026-10-24 | 初版：注册防护（第 5–7 步）发布与运维手册；基于 T5/T6 实现冻结态与沙箱构建验证结果。 |
| 2026-10-24 | R2 修订：发码顺序改为 Turnstile→配额、注册链 16KB body 与 413、邮箱 `LOWER(email)` 与 best-effort 唯一索引、EmailBind 清码、发信日志脱敏、同步回退受限速/熔断、worker 真实发送峰值限速、`turnstile_strict` 与总开关解耦、OAuth 首登建号接入 guard、内存 limiter 上限；已知限制按 R2 后现状重写，文件清单/PR 拆分补 R2 增量。 |
| 2026-10-25 | INV 修订：新增「仅限邀请码注册」（`registration_setting.invite_code_required`，第 24 键、默认 false、仅 root）；新增 §3.11 行为矩阵/发码与注册顺序/`vr`·`v` purpose/切换与发布顺序/4 位码风险；`/api/status` 增 `invite_code_required`；`/api/verification` 链插入 `RegistrationInviteGuard`；access log 掩码与残余风险、文件/测试清单（§8.9）与门禁命令同步。 |
