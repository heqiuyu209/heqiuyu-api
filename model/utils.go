package model

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/heqiuyu/heqiuyu-api/common"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

const (
	BatchUpdateTypeUserQuota = iota
	BatchUpdateTypeTokenQuota
	BatchUpdateTypeUsedQuota
	BatchUpdateTypeChannelUsedQuota
	BatchUpdateTypeRequestCount
	BatchUpdateTypeCount // if you add a new type, you need to add a new map and a new lock
)

var batchUpdateStores []map[int]int
var batchUpdateLocks []sync.Mutex

func init() {
	for i := 0; i < BatchUpdateTypeCount; i++ {
		batchUpdateStores = append(batchUpdateStores, make(map[int]int))
		batchUpdateLocks = append(batchUpdateLocks, sync.Mutex{})
	}
}

func InitBatchUpdater() {
	gopool.Go(func() {
		for {
			time.Sleep(time.Duration(common.BatchUpdateInterval) * time.Second)
			batchUpdate()
		}
	})
}

func addNewRecord(type_ int, id int, value int) {
	batchUpdateLocks[type_].Lock()
	defer batchUpdateLocks[type_].Unlock()
	if _, ok := batchUpdateStores[type_][id]; !ok {
		batchUpdateStores[type_][id] = value
	} else {
		batchUpdateStores[type_][id] += value
	}
}

// batchUpdateMaxStoreSize 单个批量更新类型允许积压的最大键数。
// 超过该上限说明数据库持续写入失败，此时继续积压会导致内存膨胀，
// 因此记录醒目错误并放弃该批增量（宁可丢计数也不拖垮进程）。
const batchUpdateMaxStoreSize = 100000

// batchUpdate 将进程内累积的增量刷入数据库。
//
// 重要：余额类增量（用户余额、令牌子额度）在写库失败时必须回填到 store 等待下一轮重试。
// 旧实现在写库前就清空了 store 且只打印日志，导致一次瞬时数据库故障就永久丢失
// 已发生的扣费（审计报告 M10）。
func batchUpdate() {
	// check if there's any data to update
	hasData := false
	for i := 0; i < BatchUpdateTypeCount; i++ {
		batchUpdateLocks[i].Lock()
		if len(batchUpdateStores[i]) > 0 {
			hasData = true
			batchUpdateLocks[i].Unlock()
			break
		}
		batchUpdateLocks[i].Unlock()
	}

	if !hasData {
		return
	}

	common.SysLog("batch update started")
	for i := 0; i < BatchUpdateTypeCount; i++ {
		batchUpdateLocks[i].Lock()
		store := batchUpdateStores[i]
		batchUpdateStores[i] = make(map[int]int)
		batchUpdateLocks[i].Unlock()

		if len(store) == 0 {
			continue
		}

		okCount := 0
		failed := make(map[int]int)
		for key, value := range store {
			var err error
			switch i {
			case BatchUpdateTypeUserQuota:
				err = increaseUserQuota(key, value)
			case BatchUpdateTypeTokenQuota:
				err = increaseTokenQuota(key, value)
			case BatchUpdateTypeUsedQuota:
				// 统计类计数非资金数据，保持尽力而为，不重试。
				updateUserUsedQuota(key, value)
			case BatchUpdateTypeRequestCount:
				updateUserRequestCount(key, value)
			case BatchUpdateTypeChannelUsedQuota:
				updateChannelUsedQuota(key, value)
			}
			if err != nil {
				common.SysLog(fmt.Sprintf("failed to batch update type %d for key %d: %s", i, key, err.Error()))
				failed[key] = value
				continue
			}
			okCount++
		}

		if len(failed) == 0 {
			continue
		}

		// 余额类增量重新并回 store，下一轮重试，避免"一次失败即永久丢账"。
		batchUpdateLocks[i].Lock()
		if len(batchUpdateStores[i])+len(failed) > batchUpdateMaxStoreSize {
			batchUpdateLocks[i].Unlock()
			common.SysError(fmt.Sprintf(
				"batch update type %d backlog exceeded %d keys after repeated failures; dropping %d deltas to protect memory. Please check database health immediately.",
				i, batchUpdateMaxStoreSize, len(failed)))
			continue
		}
		for key, value := range failed {
			batchUpdateStores[i][key] += value
		}
		batchUpdateLocks[i].Unlock()
		common.SysError(fmt.Sprintf("batch update type %d: %d succeeded, %d failed and will be retried", i, okCount, len(failed)))
	}
	common.SysLog("batch update finished")
}

func RecordExist(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return false, err
}

func shouldUpdateRedis(fromDB bool, err error) bool {
	return common.RedisEnabled && fromDB && err == nil
}
