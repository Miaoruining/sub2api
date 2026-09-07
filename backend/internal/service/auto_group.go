package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
)

type autoGroupAttemptKey struct{}

// AutoGroupAttempt 只通过服务端 context 传递，客户端不能指定目标分组。
// 每次尝试使用独立 context 和 APIKey 副本，避免异步扣费读到下一组的数据。
type AutoGroupAttempt struct {
	APIKeyID  int64
	GroupID   int64
	Platform  string
	retryable atomic.Bool
}

func WithAutoGroupAttempt(ctx context.Context, attempt *AutoGroupAttempt) context.Context {
	return context.WithValue(ctx, autoGroupAttemptKey{}, attempt)
}

func AutoGroupAttemptFromContext(ctx context.Context) *AutoGroupAttempt {
	attempt, _ := ctx.Value(autoGroupAttemptKey{}).(*AutoGroupAttempt)
	return attempt
}

func MarkAutoGroupRetryable(ctx context.Context) {
	if attempt := AutoGroupAttemptFromContext(ctx); attempt != nil {
		attempt.retryable.Store(true)
	}
}

// 仅允许可重试的容量、服务端或账号凭证故障触发跨组；普通参数错误不重放。
func MarkAutoGroupFailover(ctx context.Context, failure *UpstreamFailoverError, streamStarted bool) {
	if streamStarted || failure == nil || !failure.ShouldRetryNextAccount() {
		return
	}
	if failure.StatusCode == 429 || failure.StatusCode >= 500 || (failure.IsCredentialFailure() && failure.Scope == GatewayFailureScopeAccount) {
		MarkAutoGroupRetryable(ctx)
	}
}

func (a *AutoGroupAttempt) Retryable() bool { return a != nil && a.retryable.Load() }

// BindAutoGroupAttempt 在原有鉴权、订阅、余额及分组限流检查之前绑定实际分组。
// 不保存 GroupID，不改变原密钥及鉴权缓存。
func (s *APIKeyService) BindAutoGroupAttempt(ctx context.Context, key *APIKey) (*APIKey, error) {
	attempt := AutoGroupAttemptFromContext(ctx)
	if attempt == nil {
		return key, nil
	}
	if key == nil || !key.AutoGroup || key.ID != attempt.APIKeyID {
		return nil, ErrGroupNotAllowed
	}
	group, err := s.groupRepo.GetByID(ctx, attempt.GroupID)
	if err != nil || group == nil || !group.IsActive() || (attempt.Platform != "" && group.Platform != attempt.Platform) {
		return nil, ErrGroupNotAllowed
	}
	user, err := s.userRepo.GetByID(ctx, key.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrGroupNotAllowed
	}
	if !s.canUserBindGroup(ctx, user, group) {
		return nil, ErrGroupNotAllowed
	}
	copy := *key
	userCopy := *user
	copy.User = &userCopy
	copy.Group = group
	copy.GroupID = &group.ID
	copy.User.UserGroupRPMOverride = nil
	if s.userGroupRateRepo != nil {
		copy.User.UserGroupRPMOverride, err = s.userGroupRateRepo.GetRPMOverrideByUserAndGroup(ctx, key.UserID, group.ID)
		if err != nil {
			return nil, err
		}
	}
	return &copy, nil
}

type AutoGroupModels struct {
	Group  Group
	Models []string
}

// AutoGroupCatalog 是自动路由和自动密钥模型列表的共同来源。
// 不把价格目录或框架默认模型当作已接入模型；空映射需管理员配置分组模型清单。
func (s *GatewayService) AutoGroupCatalog(ctx context.Context, groups []Group) ([]AutoGroupModels, error) {
	result := make([]AutoGroupModels, 0, len(groups))
	for _, group := range groups {
		if !group.IsActive() || group.Platform == PlatformComposite {
			continue
		}
		accounts, err := s.accountRepo.ListSchedulableByGroupID(ctx, group.ID)
		if err != nil {
			return nil, fmt.Errorf("load auto group models: %w", err)
		}
		models := autoGroupRegisteredModels(group, accounts)
		allowed := models[:0]
		for _, name := range models {
			if !s.checkChannelPricingRestriction(ctx, &group.ID, name) {
				allowed = append(allowed, name)
			}
		}
		if len(allowed) > 0 {
			result = append(result, AutoGroupModels{Group: group, Models: allowed})
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Group.SortOrder != result[j].Group.SortOrder {
			return result[i].Group.SortOrder < result[j].Group.SortOrder
		}
		return result[i].Group.ID < result[j].Group.ID
	})
	return result, nil
}

func autoGroupRegisteredModels(group Group, accounts []Account) []string {
	registered := map[string]bool{}
	for _, account := range accounts {
		if account.Platform != group.Platform {
			continue
		}
		if group.CustomModelsListEnabled() {
			for _, name := range group.ModelsListConfig.Models {
				if account.IsModelSupported(name) {
					registered[name] = true
				}
			}
		} else {
			// 只读显式配置，不枚举 GetModelMapping 自动补入的平台默认模型。
			mapping, _ := account.Credentials["model_mapping"].(map[string]any)
			for name, target := range mapping {
				if value, ok := target.(string); ok && value != "" {
					registered[name] = true
				}
			}
		}
	}
	models := make([]string, 0, len(registered))
	for name := range registered {
		if name == "" || strings.ContainsAny(name, "*?") {
			continue
		}
		// 不把其他模型家族混入本平台；无法判断的自定义名称允许显式登记。
		if platform, known := DetectModelPlatform(name); known && platform != group.Platform {
			continue
		}
		models = append(models, name)
	}
	sort.Strings(models)
	return models
}

// BuildAutoGroupCodexManifest 按模型汇总其真实候选组账号，再取能力交集。
// 不读取虚拟 group_id=0 的账号，也不因某个组支持图片就向所有路由宣告支持。
func (s *GatewayService) BuildAutoGroupCodexManifest(ctx context.Context, catalog []AutoGroupModels, ids []string) ([]byte, error) {
	byModel := make(map[string][]Account)
	for _, entry := range catalog {
		if entry.Group.Platform != PlatformOpenAI {
			continue
		}
		_, accounts, err := loadCodexGroupCatalogAccounts(ctx, s.accountRepo, entry.Group.ID)
		if err != nil {
			return nil, err
		}
		for _, id := range FilterCodexModelIDsForGroup(entry.Models, &entry.Group) {
			byModel[id] = append(byModel[id], accounts...)
		}
	}
	models := make([]json.RawMessage, 0, len(ids))
	for _, id := range ids {
		accounts, ok := byModel[id]
		if !ok {
			continue
		}
		body, err := buildCodexModelsManifestForAccounts(PlatformOpenAI, []string{id}, accounts, nil, true)
		if err != nil {
			return nil, err
		}
		var envelope struct {
			Models []json.RawMessage `json:"models"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			return nil, err
		}
		models = append(models, envelope.Models...)
	}
	return json.Marshal(struct {
		Models []json.RawMessage `json:"models"`
	}{Models: models})
}

// 同一个公开模型在多个不同平台出现时拒绝猜测，避免 GPT/Claude 串组。
func AutoGroupCandidates(catalog []AutoGroupModels, model string) ([]Group, error) {
	var groups []Group
	platform := ""
	for _, entry := range catalog {
		for _, name := range entry.Models {
			if name != model {
				continue
			}
			if platform != "" && platform != entry.Group.Platform {
				return nil, fmt.Errorf("模型 %s 的平台归属不唯一", model)
			}
			platform = entry.Group.Platform
			groups = append(groups, entry.Group)
			break
		}
	}
	return groups, nil
}
