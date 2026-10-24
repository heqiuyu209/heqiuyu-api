package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/i18n"
	"github.com/heqiuyu/heqiuyu-api/model"
	"github.com/heqiuyu/heqiuyu-api/service"
	"github.com/heqiuyu/heqiuyu-api/setting/system_setting"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type OidcResponse struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

type OidcUser struct {
	OpenID            string `json:"sub"`
	Email             string `json:"email"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
	Picture           string `json:"picture"`
}

func getOidcUserInfoByCode(code string) (*OidcUser, error) {
	if code == "" {
		return nil, errors.New("无效的参数")
	}

	values := url.Values{}
	values.Set("client_id", system_setting.GetOIDCSettings().ClientId)
	values.Set("client_secret", system_setting.GetOIDCSettings().ClientSecret)
	values.Set("code", code)
	values.Set("grant_type", "authorization_code")
	values.Set("redirect_uri", fmt.Sprintf("%s/oauth/oidc", system_setting.ServerAddress))
	formData := values.Encode()
	req, err := http.NewRequest("POST", system_setting.GetOIDCSettings().TokenEndpoint, strings.NewReader(formData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	client := http.Client{
		Timeout: 5 * time.Second,
	}
	res, err := client.Do(req)
	if err != nil {
		common.SysLog(err.Error())
		return nil, errors.New("无法连接至 OIDC 服务器，请稍后重试！")
	}
	defer res.Body.Close()
	var oidcResponse OidcResponse
	err = json.NewDecoder(res.Body).Decode(&oidcResponse)
	if err != nil {
		return nil, err
	}

	if oidcResponse.AccessToken == "" {
		common.SysLog("OIDC 获取 Token 失败，请检查设置！")
		return nil, errors.New("OIDC 获取 Token 失败，请检查设置！")
	}

	req, err = http.NewRequest("GET", system_setting.GetOIDCSettings().UserInfoEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+oidcResponse.AccessToken)
	res2, err := client.Do(req)
	if err != nil {
		common.SysLog(err.Error())
		return nil, errors.New("无法连接至 OIDC 服务器，请稍后重试！")
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		common.SysLog("OIDC 获取用户信息失败！请检查设置！")
		return nil, errors.New("OIDC 获取用户信息失败！请检查设置！")
	}

	var oidcUser OidcUser
	err = json.NewDecoder(res2.Body).Decode(&oidcUser)
	if err != nil {
		return nil, err
	}
	if oidcUser.OpenID == "" || oidcUser.Email == "" {
		common.SysLog("OIDC 获取用户信息为空！请检查设置！")
		return nil, errors.New("OIDC 获取用户信息为空！请检查设置！")
	}
	return &oidcUser, nil
}

func OidcAuth(c *gin.Context) {
	session := sessions.Default(c)
	state := c.Query("state")
	if state == "" || session.Get("oauth_state") == nil || state != session.Get("oauth_state").(string) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "state is empty or not same",
		})
		return
	}
	if session.Get("id") != nil {
		OidcBind(c)
		return
	}
	if !system_setting.GetOIDCSettings().Enabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "管理员未开启通过 OIDC 登录以及注册",
		})
		return
	}
	code := c.Query("code")
	oidcUser, err := getOidcUserInfoByCode(code)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	user := model.User{
		OidcId: oidcUser.OpenID,
	}
	if model.IsOidcIdAlreadyTaken(user.OidcId) {
		err := user.FillUserByOidcId()
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	} else {
		if common.RegisterEnabled {
			// 仅限邀请码注册：先校验邀请码（首登建号必须携带有效邀请码；防御性同步统一 OAuth 流程）。
			inviterId := 0
			if inviteCodeRequired() {
				validatedInviterId, inviteErr := validateInviteCode(sessionInviteCode(session))
				if inviteErr != nil {
					respondInviteCodeError(c, inviteErr)
					return
				}
				inviterId = validatedInviterId
			}
			// 注册防护：OAuth 首登建号同样受注册防护限流约束（计划 R2 §2）。
			if decision := service.CheckRegistrationAttempt(c.ClientIP()); !decision.Allowed {
				common.ApiErrorI18n(c, i18n.MsgUserRegistrationRateLimited)
				return
			}
			// 邮箱统一 lower+trim，与注册/发码/DB 唯一索引的大小写不敏感策略一致。
			user.Email = strings.ToLower(strings.TrimSpace(oidcUser.Email))
			if oidcUser.PreferredUsername != "" {
				user.Username = oidcUser.PreferredUsername
			} else {
				user.Username = "oidc_" + strconv.Itoa(model.GetMaxUserId()+1)
			}
			if oidcUser.Name != "" {
				user.DisplayName = oidcUser.Name
			} else {
				user.DisplayName = "OIDC User"
			}
			// 写入邀请人归因：0 也显式设置；Insert 的 inviterId 参数只用于奖励/日志。
			user.InviterId = inviterId
			err := user.Insert(inviterId)
			if err != nil {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": err.Error(),
				})
				return
			}
			service.RecordRegistrationSuccess(c.ClientIP())
		} else {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "管理员关闭了新用户注册",
			})
			return
		}
	}

	if user.Status != common.UserStatusEnabled {
		c.JSON(http.StatusOK, gin.H{
			"message": "用户已被封禁",
			"success": false,
		})
		return
	}
	beginLogin(c, &user)
}

func OidcBind(c *gin.Context) {
	if !system_setting.GetOIDCSettings().Enabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "管理员未开启通过 OIDC 登录以及注册",
		})
		return
	}
	code := c.Query("code")
	oidcUser, err := getOidcUserInfoByCode(code)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	user := model.User{
		OidcId: oidcUser.OpenID,
	}
	if model.IsOidcIdAlreadyTaken(user.OidcId) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "该 OIDC 账户已被绑定",
		})
		return
	}
	session := sessions.Default(c)
	id := session.Get("id")
	// id := c.GetInt("id")  // critical bug!
	user.Id = id.(int)
	err = user.FillUserById()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	user.OidcId = oidcUser.OpenID
	err = user.UpdateOAuthBindings()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "bind",
	})
	return
}
