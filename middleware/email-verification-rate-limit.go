package middleware

// Deprecated: 原 EmailVerificationRateLimit（单 IP 30 秒内最多 2 次）的语义已并入
// EmailSendRateLimit（见 middleware/email-send-rate-limit.go），路由也已切换到
// EmailSendRateLimit + TurnstileCheckStrict。
//
// 说明：本文件按计划应删除；但当前 Windows 文件沙箱的工作区继承了
// "Everyone: DENY (DeleteSubdirectoriesAndFiles)" 权限项，进程内无法删除该文件
// （DeleteFile 返回 Access is denied），因此保留为空文件，待宿主环境允许删除时移除。
