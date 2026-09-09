package handler

import (
	"context"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type PoolOrderHandler struct {
	Repo           service.PoolRepository
	auth           service.APIKeyAuthCacheInvalidator
	billing        *service.BillingCacheService
	subscriptions  *service.SubscriptionService
	dynamicRefresh *service.PoolDynamicRefreshService
}

func NewPoolOrderHandler(repo service.PoolRepository, auth service.APIKeyAuthCacheInvalidator, billing *service.BillingCacheService, subscriptions *service.SubscriptionService) *PoolOrderHandler {
	return &PoolOrderHandler{Repo: repo, auth: auth, billing: billing, subscriptions: subscriptions}
}
func ProvidePoolOrderHandler(repo service.PoolRepository, auth service.APIKeyAuthCacheInvalidator, billing *service.BillingCacheService, subscriptions *service.SubscriptionService, dynamic *service.PoolDynamicRefreshService) *PoolOrderHandler {
	h := NewPoolOrderHandler(repo, auth, billing, subscriptions)
	h.dynamicRefresh = dynamic
	return h
}

func (h *PoolOrderHandler) DynamicUsage(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "请先登录")
		return
	}
	repo, ok := h.Repo.(service.PoolDynamicRepository)
	if !ok {
		response.Success(c, []service.PoolDynamicUsage{})
		return
	}
	out, err := repo.DynamicUsage(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func (h *PoolOrderHandler) Pricing(c *gin.Context) {
	response.Success(c, service.PoolPricingCatalog())
}

func (h *PoolOrderHandler) List(c *gin.Context)      { h.list(c, false) }
func (h *PoolOrderHandler) AdminList(c *gin.Context) { h.list(c, true) }
func (h *PoolOrderHandler) list(c *gin.Context, admin bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "请先登录")
		return
	}
	if admin && !poolAdmin(c) {
		return
	}
	out, err := h.Repo.List(c.Request.Context(), subject.UserID, admin)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}
func poolAdmin(c *gin.Context) bool {
	role, ok := middleware.GetUserRoleFromContext(c)
	if !ok || role != service.RoleAdmin {
		response.Forbidden(c, "需要管理员权限")
		return false
	}
	return true
}
func (h *PoolOrderHandler) Create(c *gin.Context) {
	if !poolAdmin(c) {
		return
	}
	var req service.PoolCreate
	if c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "拼单参数无效")
		return
	}
	id, err := h.Repo.Create(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"id": id})
}
func (h *PoolOrderHandler) Join(c *gin.Context)    { h.mutate(c, "join") }
func (h *PoolOrderHandler) Leave(c *gin.Context)   { h.mutate(c, "leave") }
func (h *PoolOrderHandler) Deliver(c *gin.Context) { h.mutate(c, "deliver") }
func (h *PoolOrderHandler) Cancel(c *gin.Context)  { h.mutate(c, "cancel") }
func (h *PoolOrderHandler) mutate(c *gin.Context, action string) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "请先登录")
		return
	}
	if (action == "cancel" || action == "deliver") && !poolAdmin(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "拼单编号无效")
		return
	}
	var affected []int64
	switch action {
	case "deliver":
		var req struct {
			ResourceID int64 `json:"resource_id"`
		}
		if c.ShouldBindJSON(&req) != nil || req.ResourceID <= 0 {
			response.BadRequest(c, "请选择拼单账号")
			return
		}
		// Refresh only dynamic orders; the repository remains authoritative about
		// freshness and ownership, including when a concurrent probe holds the lease.
		orders, listErr := h.Repo.List(c.Request.Context(), subject.UserID, true)
		if listErr != nil {
			response.ErrorFrom(c, listErr)
			return
		}
		for _, order := range orders {
			if order.ID != id || (order.QuotaMode != "dynamic" && order.QuotaMode != "dynamic_shadow") {
				continue
			}
			if h.dynamicRefresh != nil {
				resources, resourceErr := h.Repo.Resources(c.Request.Context())
				if resourceErr != nil {
					response.ErrorFrom(c, resourceErr)
					return
				}
				for _, resource := range resources {
					if resource.ID == req.ResourceID {
						_ = h.dynamicRefresh.Refresh(c.Request.Context(), resource.AccountID)
						break
					}
				}
			}
			break
		}
		affected, err = h.Repo.Deliver(c.Request.Context(), id, req.ResourceID)
	case "join":
		affected, err = h.Repo.Join(c.Request.Context(), id, subject.UserID)
	case "leave":
		affected, err = h.Repo.Leave(c.Request.Context(), id, subject.UserID)
	case "cancel":
		affected, err = h.Repo.Cancel(c.Request.Context(), id)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 5*time.Second)
	defer cancel()
	orders, _ := h.Repo.List(ctx, subject.UserID, true)
	var gid int64
	for _, o := range orders {
		if o.ID == id {
			gid = o.GroupID
			break
		}
	}
	for _, uid := range affected {
		if h.auth != nil {
			h.auth.InvalidateAuthCacheByUserID(ctx, uid)
		}
		if h.billing != nil {
			_ = h.billing.InvalidateUserBalance(ctx, uid)
		}
		if gid > 0 && h.subscriptions != nil {
			h.subscriptions.InvalidateSubCacheSync(uid, gid)
		}
	}
	response.Success(c, gin.H{"saved": true})
}

func (h *PoolOrderHandler) Resources(c *gin.Context) {
	if !poolAdmin(c) {
		return
	}
	out, err := h.Repo.Resources(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}
func (h *PoolOrderHandler) CreateResource(c *gin.Context) {
	if !poolAdmin(c) {
		return
	}
	var req service.PoolResourceCreate
	if c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "账号参数无效")
		return
	}
	id, err := h.Repo.CreateResource(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"id": id})
}
func (h *PoolOrderHandler) SetResourceStatus(c *gin.Context) {
	if !poolAdmin(c) {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "账号参数无效")
		return
	}
	if err = h.Repo.SetResourceStatus(c.Request.Context(), id, req.Status); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"saved": true})
}

func (h *PoolOrderHandler) Notifications(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "请先登录")
		return
	}
	role, _ := middleware.GetUserRoleFromContext(c)
	if c.Request.Method == "POST" {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			response.BadRequest(c, "通知编号无效")
			return
		}
		if err = h.Repo.ReadNotification(c.Request.Context(), id, subject.UserID, role == service.RoleAdmin); err != nil {
			response.ErrorFrom(c, err)
			return
		}
		response.Success(c, gin.H{"saved": true})
		return
	}
	out, err := h.Repo.Notifications(c.Request.Context(), subject.UserID, role == service.RoleAdmin)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func (h *PoolOrderHandler) Products(c *gin.Context)      { h.products(c, false) }
func (h *PoolOrderHandler) AdminProducts(c *gin.Context) { h.products(c, true) }
func (h *PoolOrderHandler) products(c *gin.Context, admin bool) {
	if admin && !poolAdmin(c) {
		return
	}
	out, err := h.Repo.Products(c.Request.Context(), admin)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}
func (h *PoolOrderHandler) SaveProduct(c *gin.Context) {
	if !poolAdmin(c) {
		return
	}
	var p service.PoolProduct
	if c.ShouldBindJSON(&p) != nil {
		response.BadRequest(c, "商品参数无效")
		return
	}
	var id int64
	var err error
	if c.Param("id") != "" {
		id, err = strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			response.BadRequest(c, "商品编号无效")
			return
		}
	}
	id, err = h.Repo.SaveProduct(c.Request.Context(), id, p)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"id": id})
}
func (h *PoolOrderHandler) PurchaseProduct(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "请先登录")
		return
	}
	var req struct {
		RequestID string `json:"request_id"`
		Version   int64  `json:"version"`
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 || c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "商品参数无效")
		return
	}
	oid, err := h.Repo.PurchaseProduct(c.Request.Context(), id, subject.UserID, req.RequestID, req.Version)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 5*time.Second)
	defer cancel()
	if h.billing != nil {
		_ = h.billing.InvalidateUserBalance(ctx, subject.UserID)
	}
	if h.auth != nil {
		h.auth.InvalidateAuthCacheByUserID(ctx, subject.UserID)
	}
	response.Success(c, gin.H{"order_id": oid})
}

func (h *PoolOrderHandler) CreditHolds(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "请先登录")
		return
	}
	rows, err := h.Repo.CreditHolds(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, rows)
}
