package relay

import (
	relaycommon "github.com/heqiuyu/heqiuyu-api/relay/common"
	"github.com/heqiuyu/heqiuyu-api/types"
)

func newAPIErrorFromParamOverride(err error) *types.HeqiuyuError {
	if fixedErr, ok := relaycommon.AsParamOverrideReturnError(err); ok {
		return relaycommon.HeqiuyuErrorFromParamOverride(fixedErr)
	}
	return types.NewError(err, types.ErrorCodeChannelParamOverrideInvalid, types.ErrOptionWithSkipRetry())
}
