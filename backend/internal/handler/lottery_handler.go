package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type LotteryHandler struct{ lottery *service.LotteryService }

func NewLotteryHandler(lottery *service.LotteryService) *LotteryHandler {
	return &LotteryHandler{lottery: lottery}
}

func (h *LotteryHandler) Status(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	out, err := h.lottery.Status(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}
func (h *LotteryHandler) Draw(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req struct {
		ActivityDate string `json:"activity_date" binding:"required"`
		RequestID    string `json:"request_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid activity date")
		return
	}
	out, err := h.lottery.Draw(c.Request.Context(), subject.UserID, req.ActivityDate, req.RequestID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

// 以下方法仅在 AdminAuthMiddleware 保护的 /admin 路由注册。
func (h *LotteryHandler) AdminStatus(c *gin.Context) {
	if role, ok := middleware.GetUserRoleFromContext(c); !ok || role != service.RoleAdmin {
		response.Forbidden(c, "Administrator access required")
		return
	}
	out, err := h.lottery.AdminStatus(c.Request.Context(), c.Query("date"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}
func (h *LotteryHandler) UpdateConfig(c *gin.Context) {
	if role, ok := middleware.GetUserRoleFromContext(c); !ok || role != service.RoleAdmin {
		response.Forbidden(c, "Administrator access required")
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req service.LotteryConfigUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid lottery configuration")
		return
	}
	if err := h.lottery.UpdateConfig(c.Request.Context(), subject.UserID, req); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"saved": true})
}
