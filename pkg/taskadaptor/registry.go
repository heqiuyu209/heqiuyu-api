// Package taskadaptor 承载"任务轮询适配器"的接口与注册表。
//
// 为什么单独放一个叶子包：轮询逻辑在 service，而适配器实现在 relay，
// 且 relay -> service 的依赖已经存在，因此 service 不能直接引用 relay。
// 早先的解决办法是由 main 包给 service 注入一个全局函数变量——那意味着
// service 离开 main 就无法工作（任何测试或其它入口调用轮询都会因 nil 调用而崩溃），
// 而且依赖方向只存在于运行期、编译器看不见。
//
// 把接口与注册表放在这个既不依赖 service 也不依赖 relay 的叶子包里之后：
//   - 实现方（relay 包）在 init 中自注册；
//   - 消费方（service 包）直接读取；只要进程里导入了 relay，轮询就能工作；
//   - 依赖方向是编译期可见且无环的，测试中也可以替换实现。
package taskadaptor

import (
	"net/http"
	"sync"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/constant"
	"github.com/heqiuyu/heqiuyu-api/model"
	relaycommon "github.com/heqiuyu/heqiuyu-api/relay/common"
)

// TaskPollingAdaptor 定义任务轮询所需的最小适配器接口。
type TaskPollingAdaptor interface {
	Init(info *relaycommon.RelayInfo)
	FetchTask(baseURL string, key string, body map[string]any, proxy string) (*http.Response, error)
	ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error)
	// AdjustBillingOnComplete 在任务到达终态（成功/失败）时由轮询循环调用。
	// 返回正数触发差额结算（补扣/退还），返回 0 保持预扣费金额不变。
	AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int
}

var (
	resolverMu sync.RWMutex
	resolver   func(platform constant.TaskPlatform) TaskPollingAdaptor
	warnOnce   sync.Once
)

// RegisterResolver 由实现方注册平台到适配器的解析函数，通常在实现包的 init 中调用。
func RegisterResolver(f func(platform constant.TaskPlatform) TaskPollingAdaptor) {
	resolverMu.Lock()
	defer resolverMu.Unlock()
	resolver = f
}

// Get 返回指定平台的任务轮询适配器；未注册或平台不支持时返回 nil。
//
// 返回 nil 是正常情况（例如该平台没有轮询实现），调用方必须判空后再使用，
// 不要直接调用其方法。
func Get(platform constant.TaskPlatform) TaskPollingAdaptor {
	resolverMu.RLock()
	f := resolver
	resolverMu.RUnlock()

	if f == nil {
		// 只告警一次，避免在轮询循环里刷日志。
		warnOnce.Do(func() {
			common.SysLog("task adaptor resolver is not registered (package relay not imported?); task polling is disabled")
		})
		return nil
	}
	return f(platform)
}
