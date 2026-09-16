package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/constant"
	"github.com/heqiuyu/heqiuyu-api/dto"

	"github.com/gin-gonic/gin"

	"github.com/bytedance/gopkg/util/gopool"
)

// UserBase struct remains the same as it represents the cached data structure
type UserBase struct {
	Id       int    `json:"id"`
	Group    string `json:"group"`
	Email    string `json:"email"`
	Quota    int    `json:"quota"`
	Status   int    `json:"status"`
	Username string `json:"username"`
	Setting  string `json:"setting"`
	// Role 必须随缓存一起保存：鉴权中间件每次请求都用它判定权限，
	// 不再信任会话 cookie 里登录时写入的角色。
	Role int `json:"role"`
}

func (user *UserBase) WriteContext(c *gin.Context) {
	common.SetContextKey(c, constant.ContextKeyUserGroup, user.Group)
	common.SetContextKey(c, constant.ContextKeyUserQuota, user.Quota)
	common.SetContextKey(c, constant.ContextKeyUserStatus, user.Status)
	common.SetContextKey(c, constant.ContextKeyUserEmail, user.Email)
	common.SetContextKey(c, constant.ContextKeyUserName, user.Username)
	common.SetContextKey(c, constant.ContextKeyUserSetting, user.GetSetting())
}

func (user *UserBase) GetSetting() dto.UserSetting {
	setting := dto.UserSetting{}
	if user.Setting != "" {
		err := common.Unmarshal([]byte(user.Setting), &setting)
		if err != nil {
			common.SysLog("failed to unmarshal setting: " + err.Error())
		}
	}
	return setting
}

// getUserCacheKey returns the key for user cache
func getUserCacheKey(userId int) string {
	return fmt.Sprintf("user:%d", userId)
}

// invalidateUserCache clears user cache
func invalidateUserCache(userId int) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisDelKey(getUserCacheKey(userId))
}

// InvalidateUserCache is the exported version of invalidateUserCache.
// 供 controller 等上层包在用户状态变更（如禁用、删除、角色变更）后主动清理缓存。
func InvalidateUserCache(userId int) error {
	return invalidateUserCache(userId)
}

// updateUserCache updates all user cache fields using hash
func updateUserCache(user User) error {
	if !common.RedisEnabled {
		return nil
	}

	return common.RedisHSetObj(
		getUserCacheKey(user.Id),
		user.ToBaseUser(),
		time.Duration(common.RedisKeyCacheSeconds())*time.Second,
	)
}

// GetUserCache gets complete user cache from hash
func GetUserCache(userId int) (userCache *UserBase, err error) {
	var user *User
	var fromDB bool
	defer func() {
		// Update Redis cache asynchronously on successful DB read
		if shouldUpdateRedis(fromDB, err) && user != nil {
			gopool.Go(func() {
				if err := updateUserCache(*user); err != nil {
					common.SysLog("failed to update user status cache: " + err.Error())
				}
			})
		}
	}()

	// Try getting from Redis first
	userCache, err = cacheGetUserBase(userId)
	if err == nil && userCache != nil {
		// 旧版本写入的 hash 没有 Role 字段，此时 Role 解析为 0。
		// 若直接采信会把所有用户当成游客，因此发现 Role==0 时回退读库并刷新缓存。
		if userCache.Role != 0 {
			return userCache, nil
		}
		err = fmt.Errorf("cached user %d has no role field, reloading from database", userId)
	}

	// If Redis fails, get from DB
	fromDB = true
	user, err = GetUserById(userId, false)
	if err != nil {
		return nil, err // Return nil and error if DB lookup fails
	}

	// Create cache object from user data
	userCache = &UserBase{
		Id:       user.Id,
		Group:    user.Group,
		Quota:    user.Quota,
		Status:   user.Status,
		Username: user.Username,
		Setting:  user.Setting,
		Email:    user.Email,
		Role:     user.Role,
	}

	return userCache, nil
}

// GetUserAuthz 一次返回鉴权所需的全部权威字段。
//
// 这是唯一的身份权威来源：会话 cookie 只提供用户 ID，角色/状态/分组一律以
// 数据库（经缓存）为准，从而让封禁、降权、分组变更对已登录会话立即生效。
// Redis 未启用时退化为一次 SELECT。
func GetUserAuthz(userId int) (status int, role int, group string, username string, err error) {
	if userId <= 0 {
		return 0, 0, "", "", errors.New("userId 无效")
	}
	userCache, err := GetUserCache(userId)
	if err != nil {
		return 0, 0, "", "", err
	}
	if userCache == nil {
		return 0, 0, "", "", errors.New("user not found")
	}
	return userCache.Status, userCache.Role, userCache.Group, userCache.Username, nil
}

func cacheGetUserBase(userId int) (*UserBase, error) {
	if !common.RedisEnabled {
		return nil, fmt.Errorf("redis is not enabled")
	}
	var userCache UserBase
	// Try getting from Redis first
	err := common.RedisHGetObj(getUserCacheKey(userId), &userCache)
	if err != nil {
		return nil, err
	}
	return &userCache, nil
}

// Add atomic quota operations using hash fields
func cacheIncrUserQuota(userId int, delta int64) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHIncrBy(getUserCacheKey(userId), "Quota", delta)
}

func cacheDecrUserQuota(userId int, delta int64) error {
	return cacheIncrUserQuota(userId, -delta)
}

// Helper functions to get individual fields if needed
func getUserGroupCache(userId int) (string, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return "", err
	}
	return cache.Group, nil
}

func getUserQuotaCache(userId int) (int, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return 0, err
	}
	return cache.Quota, nil
}

func getUserStatusCache(userId int) (int, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return 0, err
	}
	return cache.Status, nil
}

func getUserNameCache(userId int) (string, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return "", err
	}
	return cache.Username, nil
}

func getUserSettingCache(userId int) (dto.UserSetting, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return dto.UserSetting{}, err
	}
	return cache.GetSetting(), nil
}

// New functions for individual field updates
func updateUserStatusCache(userId int, status bool) error {
	if !common.RedisEnabled {
		return nil
	}
	statusInt := common.UserStatusEnabled
	if !status {
		statusInt = common.UserStatusDisabled
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Status", fmt.Sprintf("%d", statusInt))
}

func updateUserQuotaCache(userId int, quota int) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Quota", fmt.Sprintf("%d", quota))
}

func updateUserGroupCache(userId int, group string) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Group", group)
}

func UpdateUserGroupCache(userId int, group string) error {
	return updateUserGroupCache(userId, group)
}

func updateUserNameCache(userId int, username string) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Username", username)
}

func updateUserSettingCache(userId int, setting string) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Setting", setting)
}

// GetUserLanguage returns the user's language preference from cache
// Uses the existing GetUserCache mechanism for efficiency
func GetUserLanguage(userId int) string {
	userCache, err := GetUserCache(userId)
	if err != nil {
		return ""
	}
	return userCache.GetSetting().Language
}
