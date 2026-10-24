package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/i18n"
	"github.com/heqiuyu/heqiuyu-api/model"
	"github.com/heqiuyu/heqiuyu-api/service"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type GitHubOAuthResponse struct {
	AccessToken string `json:"access_token"`
	Scope       string `json:"scope"`
	TokenType   string `json:"token_type"`
}

type GitHubUser struct {
	Login string `json:"login"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func getGitHubUserInfoByCode(code string) (*GitHubUser, error) {
	if code == "" {
		return nil, errors.New("无效的参数")
	}
	values := map[string]string{"client_id": common.GitHubClientId, "client_secret": common.GitHubClientSecret, "code": code}
	jsonData, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", "https://github.com/login/oauth/access_token", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := http.Client{
		Timeout: 20 * time.Second,
	}
	res, err := client.Do(req)
	if err != nil {
		common.SysLog(err.Error())
		return nil, errors.New("无法连接至 GitHub 服务器，请稍后重试！")
	}
	defer res.Body.Close()
	var oAuthResponse GitHubOAuthResponse
	err = json.NewDecoder(res.Body).Decode(&oAuthResponse)
	if err != nil {
		return nil, err
	}
	req, err = http.NewRequest("GET", "https://api.github.com/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", oAuthResponse.AccessToken))
	res2, err := client.Do(req)
	if err != nil {
		common.SysLog(err.Error())
		return nil, errors.New("无法连接至 GitHub 服务器，请稍后重试！")
	}
	defer res2.Body.Close()
	var githubUser GitHubUser
	err = json.NewDecoder(res2.Body).Decode(&githubUser)
	if err != nil {
		return nil, err
	}
	if githubUser.Login == "" {
		return nil, errors.New("返回值非法，用户字段为空，请稍后重试！")
	}
	return &githubUser, nil
}

func GitHubOAuth(c *gin.Context) {
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
		GitHubBind(c)
		return
	}

	if !common.GitHubOAuthEnabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "管理员未开启通过 GitHub 登录以及注册",
		})
		return
	}
	code := c.Query("code")
	githubUser, err := getGitHubUserInfoByCode(code)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	user := model.User{
		GitHubId: githubUser.Login,
	}
	// IsGitHubIdAlreadyTaken is unscoped
	if model.IsGitHubIdAlreadyTaken(user.GitHubId) {
		// FillUserByGitHubId is scoped
		err := user.FillUserByGitHubId()
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
		// if user.Id == 0 , user has been deleted
		if user.Id == 0 {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "用户已注销",
			})
			return
		}
	} else {
		if common.RegisterEnabled {
			// 仅限邀请码注册：先校验邀请码（首登建号必须携带有效邀请码）；
			// 关闭时保留既有 aff 推荐逻辑。
			inviteRequired := inviteCodeRequired()
			inviterId := 0
			if inviteRequired {
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
			user.Username = "github_" + strconv.Itoa(model.GetMaxUserId()+1)
			if githubUser.Name != "" {
				user.DisplayName = githubUser.Name
			} else {
				user.DisplayName = "GitHub User"
			}
			// 邮箱统一 lower+trim，与注册/发码/DB 唯一索引的大小写不敏感策略一致。
			user.Email = strings.ToLower(strings.TrimSpace(githubUser.Email))
			user.Role = common.RoleCommonUser
			user.Status = common.UserStatusEnabled
			if !inviteRequired {
				if affCode := session.Get("aff"); affCode != nil {
					inviterId, _ = model.GetUserIdByAffCode(affCode.(string))
				}
			}
			// 写入邀请人归因：0 也显式设置；Insert 的 inviterId 参数只用于奖励/日志。
			user.InviterId = inviterId

			if err := user.Insert(inviterId); err != nil {
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

func GitHubBind(c *gin.Context) {
	if !common.GitHubOAuthEnabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "管理员未开启通过 GitHub 登录以及注册",
		})
		return
	}
	code := c.Query("code")
	githubUser, err := getGitHubUserInfoByCode(code)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	user := model.User{
		GitHubId: githubUser.Login,
	}
	if model.IsGitHubIdAlreadyTaken(user.GitHubId) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "该 GitHub 账户已被绑定",
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
	user.GitHubId = githubUser.Login
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
