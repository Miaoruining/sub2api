package service

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"time"
)

// 请求前按无缓存输入与最大输出估价；最终扣额以网关实际上报用量计费为准。
func (s *OpenAIGatewayService) EstimatePoolCredit(ctx context.Context, key *APIKey, body []byte, tokens int64) (float64, error) {
	var req struct {
		Model           string `json:"model"`
		ServiceTier     string `json:"service_tier"`
		ReasoningEffort string `json:"reasoning_effort"`
		Reasoning       struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
		MaxTokens     int64 `json:"max_tokens"`
		MaxOutput     int64 `json:"max_output_tokens"`
		MaxCompletion int64 `json:"max_completion_tokens"`
	}
	if json.Unmarshal(body, &req) != nil || strings.TrimSpace(req.Model) == "" {
		return 0, ErrPoolConfig
	}
	if identified, _ := s.hasIdentifiedOpenAIResponsePricing(ctx, req.Model, key); !identified {
		return 0, ErrPoolConfig
	}
	output := req.MaxTokens
	if req.MaxOutput > output {
		output = req.MaxOutput
	}
	if req.MaxCompletion > output {
		output = req.MaxCompletion
	}
	if output <= 0 || tokens <= output {
		return 0, ErrPoolConfig
	}
	effort := req.ReasoningEffort
	if req.Reasoning.Effort != "" {
		effort = req.Reasoning.Effort
	}
	cost, err := s.calculateOpenAIRecordUsageTokenCost(ctx, key, req.Model, 1, time.Now(), UsageTokens{InputTokens: int(tokens - output), OutputTokens: int(output)}, req.ServiceTier, effort, nil)
	if err != nil {
		return 0, err
	}
	if cost == nil || math.IsNaN(cost.TotalCost) || math.IsInf(cost.TotalCost, 0) || cost.TotalCost <= 0 {
		return 0, ErrPoolConfig
	}
	return math.Max(0.00000001, QuantizeUsageBillingAmount(cost.TotalCost)), nil
}
