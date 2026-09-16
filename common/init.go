package common

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/heqiuyu/heqiuyu-api/constant"
)

var (
	Port         = flag.Int("port", 3000, "the listening port")
	PrintVersion = flag.Bool("version", false, "print version and exit")
	PrintHelp    = flag.Bool("help", false, "print help and exit")
	LogDir       = flag.String("log-dir", "./logs", "specify the log directory")
)

func printHelp() {
	fmt.Println("heqiuyu " + Version + " - The next-generation LLM gateway and AI asset management system supports multiple languages.")
	fmt.Println("Project: heqiuyu - https://github.com/heqiuyu209/heqiuyu-api")
	fmt.Println("Based on one-api by JustSong - https://github.com/songquanpeng/one-api")
	fmt.Println("Usage: heqiuyu-api [--port <port>] [--log-dir <log directory>] [--version] [--help]")
}

func InitEnv() {
	flag.Parse()

	envVersion := os.Getenv("VERSION")
	if envVersion != "" {
		Version = envVersion
	}

	if *PrintVersion {
		fmt.Println(Version)
		os.Exit(0)
	}

	if *PrintHelp {
		printHelp()
		os.Exit(0)
	}

	// 会话签名密钥与派生密钥。
	//
	// 安全要求：
	//   - 两者都要求 >= 32 字符。会话 cookie 只签名不加密，密钥强度直接决定
	//     能否被离线爆破后伪造出 role=root 的会话（审计报告 H9）；
	//   - 严禁两者取相同值。旧实现在未配置 CRYPTO_SECRET 时会回退复用
	//     SESSION_SECRET，使一个密钥同时承担会话签名与派生职责（审计报告 M12）；
	//   - 生产环境必须显式设置；DEBUG=true 时允许使用进程内随机值（便于本地开发，
	//     代价是每次重启都会使会话失效、派生值改变）。
	sessionSecret := os.Getenv("SESSION_SECRET")
	cryptoSecret := os.Getenv("CRYPTO_SECRET")
	debugMode := os.Getenv("DEBUG") == "true"

	if sessionSecret != "" {
		if sessionSecret == "random_string" {
			log.Println("WARNING: SESSION_SECRET is set to the default value 'random_string', please change it to a random string.")
			log.Println("警告：SESSION_SECRET被设置为默认值'random_string'，请修改为随机字符串。")
			log.Fatal("Please set SESSION_SECRET to a random string.")
		}
		if len(sessionSecret) < 32 {
			log.Fatalf("SESSION_SECRET is too short (len=%d). Please set a random string with length >= 32, e.g. `openssl rand -base64 48`.", len(sessionSecret))
		}
		if cryptoSecret != "" && sessionSecret == cryptoSecret {
			log.Fatal("SESSION_SECRET and CRYPTO_SECRET must not be the same value. Please generate two independent random secrets.")
		}
		SessionSecret = sessionSecret
	} else if debugMode {
		log.Println("WARNING: SESSION_SECRET is not set. A random value is generated for this run; sessions will be invalidated after restart. Please set SESSION_SECRET in production.")
		log.Println("警告：SESSION_SECRET 未设置，本次运行将随机生成；重启后会话全部失效。生产环境请务必设置。")
	} else {
		log.Fatal("SESSION_SECRET is not set. Please set SESSION_SECRET to a random string with length >= 32, e.g. `openssl rand -base64 48`. In dev mode, set DEBUG=true to auto-generate.")
	}

	if cryptoSecret != "" {
		if len(cryptoSecret) < 32 {
			log.Fatalf("CRYPTO_SECRET is too short (len=%d). Please set a random string with length >= 32, e.g. `openssl rand -base64 48`.", len(cryptoSecret))
		}
		CryptoSecret = cryptoSecret
	} else if debugMode {
		log.Println("WARNING: CRYPTO_SECRET is not set. A random value is generated for this run; derived keys change after restart. Please set CRYPTO_SECRET in production.")
		log.Println("警告：CRYPTO_SECRET 未设置，本次运行将随机生成；重启后派生密钥改变。生产环境请务必设置。")
	} else {
		log.Fatal("CRYPTO_SECRET is not set. Please set CRYPTO_SECRET to a random string with length >= 32, e.g. `openssl rand -base64 48`. It must differ from SESSION_SECRET and must be backed up separately from the database. In dev mode, set DEBUG=true to auto-generate.")
	}
	if os.Getenv("SQLITE_PATH") != "" {
		SQLitePath = os.Getenv("SQLITE_PATH")
	}
	if *LogDir != "" {
		var err error
		*LogDir, err = filepath.Abs(*LogDir)
		if err != nil {
			log.Fatal(err)
		}
		if _, err := os.Stat(*LogDir); os.IsNotExist(err) {
			err = os.Mkdir(*LogDir, 0777)
			if err != nil {
				log.Fatal(err)
			}
		}
	}

	// Initialize variables from constants.go that were using environment variables
	DebugEnabled = os.Getenv("DEBUG") == "true"
	MemoryCacheEnabled = os.Getenv("MEMORY_CACHE_ENABLED") == "true"
	IsMasterNode = os.Getenv("NODE_TYPE") != "slave"
	NodeName = os.Getenv("NODE_NAME")
	TLSInsecureSkipVerify = GetEnvOrDefaultBool("TLS_INSECURE_SKIP_VERIFY", false)
	if TLSInsecureSkipVerify {
		if tr, ok := http.DefaultTransport.(*http.Transport); ok && tr != nil {
			if tr.TLSClientConfig != nil {
				tr.TLSClientConfig.InsecureSkipVerify = true
			} else {
				tr.TLSClientConfig = InsecureTLSConfig
			}
		}
	}

	// Parse requestInterval and set RequestInterval
	requestInterval, _ = strconv.Atoi(os.Getenv("POLLING_INTERVAL"))
	RequestInterval = time.Duration(requestInterval) * time.Second

	// Initialize variables with GetEnvOrDefault
	SyncFrequency = GetEnvOrDefault("SYNC_FREQUENCY", 60)
	BatchUpdateInterval = GetEnvOrDefault("BATCH_UPDATE_INTERVAL", 5)
	RelayTimeout = GetEnvOrDefault("RELAY_TIMEOUT", 300)
	RelayMaxIdleConns = GetEnvOrDefault("RELAY_MAX_IDLE_CONNS", 500)
	RelayMaxIdleConnsPerHost = GetEnvOrDefault("RELAY_MAX_IDLE_CONNS_PER_HOST", 100)

	// Initialize string variables with GetEnvOrDefaultString
	GeminiSafetySetting = GetEnvOrDefaultString("GEMINI_SAFETY_SETTING", "BLOCK_NONE")
	CohereSafetySetting = GetEnvOrDefaultString("COHERE_SAFETY_SETTING", "NONE")

	// Initialize rate limit variables
	GlobalApiRateLimitEnable = GetEnvOrDefaultBool("GLOBAL_API_RATE_LIMIT_ENABLE", true)
	GlobalApiRateLimitNum = GetEnvOrDefault("GLOBAL_API_RATE_LIMIT", 180)
	GlobalApiRateLimitDuration = int64(GetEnvOrDefault("GLOBAL_API_RATE_LIMIT_DURATION", 180))

	GlobalWebRateLimitEnable = GetEnvOrDefaultBool("GLOBAL_WEB_RATE_LIMIT_ENABLE", true)
	GlobalWebRateLimitNum = GetEnvOrDefault("GLOBAL_WEB_RATE_LIMIT", 60)
	GlobalWebRateLimitDuration = int64(GetEnvOrDefault("GLOBAL_WEB_RATE_LIMIT_DURATION", 180))

	CriticalRateLimitEnable = GetEnvOrDefaultBool("CRITICAL_RATE_LIMIT_ENABLE", true)
	CriticalRateLimitNum = GetEnvOrDefault("CRITICAL_RATE_LIMIT", 20)
	CriticalRateLimitDuration = int64(GetEnvOrDefault("CRITICAL_RATE_LIMIT_DURATION", 20*60))

	SearchRateLimitEnable = GetEnvOrDefaultBool("SEARCH_RATE_LIMIT_ENABLE", true)
	SearchRateLimitNum = GetEnvOrDefault("SEARCH_RATE_LIMIT", 10)
	SearchRateLimitDuration = int64(GetEnvOrDefault("SEARCH_RATE_LIMIT_DURATION", 60))

	// 信任额度旁路：安全默认关闭。开启后余额高于阈值的账户会跳过预扣费，
	// 并发场景下可导致超额消费（审计报告 H4），仅在明确理解风险时开启。
	TrustQuotaEnabled = GetEnvOrDefaultBool("TRUST_QUOTA_ENABLED", false)
	TrustQuota = GetEnvOrDefault("TRUST_QUOTA", 0)

	// 严格模式下对渠道 base_url 施加 SSRF 策略（默认关闭，见常量定义处的说明）。
	ChannelBaseURLStrict = GetEnvOrDefaultBool("CHANNEL_BASE_URL_STRICT", false)

	// 登录失败锁定：按账号（而非按客户端 IP）计数，因此不受 X-Forwarded-For 伪造影响。
	LoginRateLimitEnable = GetEnvOrDefaultBool("LOGIN_RATE_LIMIT_ENABLE", true)
	LoginRateLimitNum = GetEnvOrDefault("LOGIN_RATE_LIMIT", 10)
	LoginRateLimitDuration = int64(GetEnvOrDefault("LOGIN_RATE_LIMIT_DURATION", 900))
	initConstantEnv()
}

func initConstantEnv() {
	constant.StreamingTimeout = GetEnvOrDefault("STREAMING_TIMEOUT", 300)
	constant.DifyDebug = GetEnvOrDefaultBool("DIFY_DEBUG", true)
	constant.MaxFileDownloadMB = GetEnvOrDefault("MAX_FILE_DOWNLOAD_MB", 64)
	constant.StreamScannerMaxBufferMB = GetEnvOrDefault("STREAM_SCANNER_MAX_BUFFER_MB", 128)
	// MaxRequestBodyMB 请求体最大大小（解压后），用于防止超大请求/zip bomb导致内存暴涨
	constant.MaxRequestBodyMB = GetEnvOrDefault("MAX_REQUEST_BODY_MB", 128)
	// ForceStreamOption 覆盖请求参数，强制返回usage信息
	constant.ForceStreamOption = GetEnvOrDefaultBool("FORCE_STREAM_OPTION", true)
	constant.CountToken = GetEnvOrDefaultBool("CountToken", true)
	constant.GetMediaToken = GetEnvOrDefaultBool("GET_MEDIA_TOKEN", true)
	constant.GetMediaTokenNotStream = GetEnvOrDefaultBool("GET_MEDIA_TOKEN_NOT_STREAM", false)
	constant.UpdateTask = GetEnvOrDefaultBool("UPDATE_TASK", true)
	constant.AzureDefaultAPIVersion = GetEnvOrDefaultString("AZURE_DEFAULT_API_VERSION", "2025-04-01-preview")
	constant.NotifyLimitCount = GetEnvOrDefault("NOTIFY_LIMIT_COUNT", 2)
	constant.NotificationLimitDurationMinute = GetEnvOrDefault("NOTIFICATION_LIMIT_DURATION_MINUTE", 10)
	// GenerateDefaultToken 是否生成初始令牌，默认关闭。
	constant.GenerateDefaultToken = GetEnvOrDefaultBool("GENERATE_DEFAULT_TOKEN", false)
	// 是否启用错误日志
	constant.ErrorLogEnabled = GetEnvOrDefaultBool("ERROR_LOG_ENABLED", false)
	// 任务轮询时查询的最大数量
	constant.TaskQueryLimit = GetEnvOrDefault("TASK_QUERY_LIMIT", 1000)
	// 异步任务超时时间（分钟），超过此时间未完成的任务将被标记为失败并退款。0 表示禁用。
	constant.TaskTimeoutMinutes = GetEnvOrDefault("TASK_TIMEOUT_MINUTES", 1440)

	soraPatchStr := GetEnvOrDefaultString("TASK_PRICE_PATCH", "")
	if soraPatchStr != "" {
		var taskPricePatches []string
		soraPatches := strings.Split(soraPatchStr, ",")
		for _, patch := range soraPatches {
			trimmedPatch := strings.TrimSpace(patch)
			if trimmedPatch != "" {
				taskPricePatches = append(taskPricePatches, trimmedPatch)
			}
		}
		constant.TaskPricePatches = taskPricePatches
	}

	// Initialize trusted redirect domains for URL validation
	trustedDomainsStr := GetEnvOrDefaultString("TRUSTED_REDIRECT_DOMAINS", "")
	var trustedDomains []string
	domains := strings.Split(trustedDomainsStr, ",")
	for _, domain := range domains {
		trimmedDomain := strings.TrimSpace(domain)
		if trimmedDomain != "" {
			// Normalize domain to lowercase
			trustedDomains = append(trustedDomains, strings.ToLower(trimmedDomain))
		}
	}
	constant.TrustedRedirectDomains = trustedDomains
}
