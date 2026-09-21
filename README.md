# heqiuyu API

**heqiuyu API** 是一个自托管的 LLM API 网关：将多家模型供应商统一收口为 OpenAI 兼容接口，提供令牌管理、额度计费、模型广场、渠道转发等能力，可配合 Cherry Studio、CC Switch 等客户端开箱即用。

## 功能特性

- **统一接入**：OpenAI 兼容协议，一个令牌访问多个模型，支持流式与普通请求
- **令牌管理**：额度上限、过期时间、批量创建、模型限制列表、IP 白名单（CIDR）
- **模型广场**：集中查看模型与定价，复制模型 ID 即可在客户端添加
- **渠道管理**：多供应商渠道转发，渠道密钥 AES-256-GCM 加密落库
- **计费系统**：额度预扣 / 结算 / 退款，钱包与充值，订阅退款事务化
- **安全防护**：SSRF 三层校验、OAuth 绑定、2FA、改密吊销旧会话、日志脱敏、前端 XSS 统一消毒
- **多语言**：en / zh / fr / ja / ru / vi
- **多端**：Web 控制台（classic / default 双前端）+ Electron 桌面端
- **部署**：Docker Compose 一键部署，支持 SQLite / MySQL / PostgreSQL（可选 Redis）

## 技术栈

| 层 | 技术 |
| --- | --- |
| 后端 | Go + Gin + GORM |
| 数据库 | SQLite / MySQL / PostgreSQL（可选 Redis） |
| 前端 | `web/classic`（React + JS）、`web/default`（React + TS + Rsbuild + TanStack Router） |
| 桌面端 | Electron |
| 部署 | Docker / Docker Compose，镜像见 `ghcr.io/heqiuyu209/heqiuyu-api` |

## 快速开始

```bash
git clone https://github.com/heqiuyu209/heqiuyu-api.git
cd heqiuyu-api
cp .env.example .env   # 按注释填写配置
docker compose up -d
```

1. 复制 `.env.example` 为 `.env`，按注释配置；`SESSION_SECRET` 与 `CRYPTO_SECRET` 至少 32 字符且不能相同。
2. `docker compose up -d` 启动服务（含 Redis 与 PostgreSQL 容器）。
3. 打开控制台注册账号，在「令牌管理」创建令牌。
4. 客户端基础地址填 `https://<你的域名>/v1`，密钥填令牌密钥，即可开始使用。

> 生产部署建议：设置 `TRUSTED_PROXIES` 为实际反向代理 IP/CIDR；HTTPS 下设置 `COOKIE_SECURE=true`。升级前请备份数据库、`.env` 与镜像版本，详见 `docs/upgrade-2026-09.md`。

## 项目结构

```
├── common/       通用工具（加密、脱敏、字符串等）
├── controller/   HTTP 处理层（令牌、渠道、模型、用户、账单等）
├── middleware/   中间件（鉴权、限流、敏感操作守卫等）
├── model/        数据模型与数据库访问（含渠道密钥加密 hook）
├── relay/        上游模型供应商适配与请求转发
├── router/       路由注册
├── service/      业务逻辑（任务轮询、令牌计数器等）
├── oauth/        OAuth 登录与绑定
├── web/          前端（classic / default 双栈）+ Electron
└── docs/         内部设计与实现文档
```

## 使用指南

> 注：如有问题可加管理员 QQ：3756686882；有需要的模型可联系管理员添加。

### 一、令牌的创建

#### 1. 注册并进入控制台

注册成功后，点击顶部导航栏的 **控制台** 部分。

#### 2. 进入令牌管理

在左侧控制台点击 **令牌管理**。

#### 3. 添加令牌

在令牌管理界面点击左上角 **添加令牌**。

#### 4. 填写令牌的基本信息

- **名称**（必填）：例如 `gpt-5.1-codex`
- **令牌分组**：可选，默认为用户的分组
- **过期时间**：可快捷设置 **永不过期 / 一个月 / 一天 / 一小时**
- **新建数量**：批量创建时会在名称后自动添加随机后缀

##### 额度设置

设置令牌可用额度和数量。

> 令牌的额度仅用于限制令牌本身的最大额度使用量，实际的使用受到账户剩余额度的限制。

##### 访问限制

- **模型限制列表**：在模型广场可以查看模型定价；不进行选择则支持所有模型。非必要，不建议启用模型限制。
- **IP 白名单（支持 CIDR 表达式）**：允许的 IP 一行一个，不填写则不限制。
  - 请勿过度信任此功能，IP 可能被伪造，请配合 nginx 和 cdn 等网关使用。

#### 5. 提交创建

点击 **提交** 后即可成功创建令牌。

### 二、令牌的使用

> 注：使用任何聊天工具需要先在电脑安装，以下附 Cherry Studio 与 CC Switch 安装链接：
> - Cherry Studio：[Cherry Studio 官方网站 - 全能的 AI 助手](https://www.cherry-ai.com)
> - CC Switch：[Release CC Switch v3.14.1](https://github.com/farion1231/cc-switch/releases)
>
> 链接无法打开的话可以找管理员获取安装包。

在令牌管理界面点击 **聊天** 即可使用（这里用 Cherry Studio 与 CC Switch 展示），推荐使用 Cherry Studio，CC Switch 需要较高熟练度。

#### Cherry Studio 使用步骤

##### 1. 打开聊天面板

点击聊天右侧小三角，然后点击 **Cherry Studio**。

##### 2. 添加 NewAPI 服务商

打开 Cherry Studio 后添加：

| 配置项 | 值 |
| --- | --- |
| 服务商名称 | NewAPI |
| 服务商 ID | new-api |
| 基础 URL | `https://api.heqiuyu.xyz` |
| API 密钥 | 填入令牌的密钥 |

- 记得打开右上角开关。
- 在 **API 地址** 处，在 `https://api.heqiuyu.xyz` 后加上 `/v1`。
- 预览地址：`https://api.heqiuyu.xyz/v1/chat/completions`

##### 3. 添加模型

点击模型栏右侧加号添加：

1. 前往 **模型广场** 复制模型名称。
2. 将模型名称复制至 **模型 ID** 一栏，然后点击 **添加模型**。
3. 端点类型均为 **OpenAI**。

##### 4. 检测连接

在 API 密钥一栏点击右侧 **检测**，选中刚才添加的模型开始检测，链接成功即可。

##### 5. 开始聊天

在 Cherry Studio 点击左侧最上方聊天部分，在顶部选择 **NewAPI** 平台，选中刚才添加的模型后即可开始聊天。

#### CC Switch 使用步骤

##### 1. 打开并选择服务类型

1. 手动打开 CC Switch。
2. 在顶部选择 **OpenAI**。
3. 点击右侧橙色加号，进入配置界面，按照提示填写（供应商名称可以随意填写）：

| 配置项 | 值 |
| --- | --- |
| 供应商名称 | 可随意填写（如 newapi） |
| 备注 | 例如：公司专用账号 |
| 官网链接 | `https://api.heqiuyu.xyz` |
| API Key | 填入令牌的密钥 |
| API 请求地址 | `https://api.heqiuyu.xyz` |

##### 2. 获取模型列表

- 在 API 请求地址填写完成后，**先不要打开"完整 URL"**。
- 在不打开完整 URL 的情况下，点击模型名称右侧 **获取模型列表**，选择需要的模型。
- 在模型选择完成后，再打开 **完整 URL**。

##### 3. 保存配置

模型名称之后的都不用手动填写，保存即可。

##### 4. 检测连接

点击 **检测** 确认连接是否成功，连接成功即可使用。

## 文档

- `docs/` — 内部设计与实现文档（非用户使用说明，用户使用说明即本文档）
- `docs/upgrade-2026-09.md` — 版本升级配置与验证清单

## 许可证

[GNU AGPL-3.0](LICENSE)
