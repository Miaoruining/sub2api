package service

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// 仅用于美元拼单；与普通中转的渠道价、倍率和价格同步配置隔离。
// 更新时核对官方 Standard 表及模型长上下文规则，同时递增 version。
//
//go:embed pool_pricing.json
var poolPricingJSON []byte

type PoolStandardPricingContextKey struct{}
type PoolModelPrice struct {
	Model                string   `json:"model"`
	Input                float64  `json:"input"`
	CacheRead            *float64 `json:"cache_read"`
	CacheWrite           *float64 `json:"cache_write"`
	Output               float64  `json:"output"`
	LongContextThreshold int      `json:"long_context_threshold"`
}
type PoolPriceCatalog struct {
	Version   string           `json:"version"`
	SourceURL string           `json:"source_url"`
	Tier      string           `json:"tier"`
	Models    []PoolModelPrice `json:"models"`
}

// 返回副本，价格页不能修改实际计费快照。
func PoolPricingCatalog() PoolPriceCatalog {
	var out PoolPriceCatalog
	if err := json.Unmarshal(poolPricingJSON, &out); err != nil {
		panic(err)
	}
	return out
}

var poolOfficialPrices = PoolPricingCatalog()

func poolModelPrice(model string) (PoolModelPrice, error) {
	model = strings.ToLower(strings.TrimSpace(model))
	for _, p := range poolOfficialPrices.Models {
		if model == p.Model {
			return p, nil
		}
	}
	// 仅接纳明确的日期快照；禁止把未知/更贵的模型通过模糊匹配计成廉价型号。
	if len(model) > 11 {
		suffix := model[len(model)-10:]
		if _, err := time.Parse("2006-01-02", suffix); err == nil && model[len(model)-11] == '-' {
			base := model[:len(model)-11]
			for _, p := range poolOfficialPrices.Models {
				if base == p.Model {
					return p, nil
				}
			}
		}
	}
	return PoolModelPrice{}, fmt.Errorf("%w: 拼团 Standard 价表尚未收录模型 %s", ErrPoolConfig, model)
}

func (s *BillingService) calculatePoolStandardCost(model string, tokens UsageTokens) (*CostBreakdown, error) {
	p, err := poolModelPrice(model)
	if err != nil {
		return nil, err
	}
	// 未列独立缓存价时按普通输入计量，页面明确展示这一规则。
	read, write := p.Input, p.Input
	if p.CacheRead != nil {
		read = *p.CacheRead
	}
	if p.CacheWrite != nil {
		write = *p.CacheWrite
	}
	pricing := &ModelPricing{InputPricePerToken: p.Input / 1e6, OutputPricePerToken: p.Output / 1e6,
		CacheReadPricePerToken: read / 1e6, CacheCreationPricePerToken: write / 1e6,
		LongContextInputThreshold: p.LongContextThreshold, LongContextInputMultiplier: 2, LongContextOutputMultiplier: 1.5}
	cost := s.computeTokenBreakdown(pricing, tokens, 1, "", true)
	cost.BillingMode = string(BillingModeToken)
	return cost, nil
}

// 结算以实际转发模型为准，避开渠道自定义计费模型与定价覆盖。
func (s *OpenAIGatewayService) poolStandardUsageCost(ctx context.Context, result *OpenAIForwardResult, tokens UsageTokens) (*CostBreakdown, error) {
	model := strings.TrimSpace(result.UpstreamModel)
	if model == "" {
		model = strings.TrimSpace(result.Model)
	}
	if model == "" {
		model, _ = ctx.Value(PoolStandardPricingContextKey{}).(string)
	}
	return s.billingService.calculatePoolStandardCost(model, tokens)
}
