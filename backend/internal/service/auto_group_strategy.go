package service

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"
)

func ValidRoutingStrategy(value string) bool {
	return value == "" || value == "smart" || value == "price" || value == "stable"
}
func NormalizeRoutingStrategy(value string) string {
	if value == "price" || value == "stable" {
		return value
	}
	return "smart"
}

// 只保留本站自动路由的近期观测，不扫描用户日志，不存请求正文。进程重启后为未知，
// 不能把无样本当作 100% 成功。多实例各自学习；账号层熔断仍使用原有调度器。
type autoRouteMetricKey struct {
	group int64
	model string
}
type autoRouteMetric struct {
	at, failure     time.Time
	errors, latency float64
	samples         int
}
type autoRouteMetrics struct {
	mu     sync.Mutex
	values map[autoRouteMetricKey]autoRouteMetric
}

func (s *GatewayService) ObserveAutoRoute(group int64, model string, failed bool, latency time.Duration) {
	s.autoRouteMetrics.record(autoRouteMetricKey{group, model}, failed, latency, time.Now())
}
func (m *autoRouteMetrics) record(key autoRouteMetricKey, failed bool, latency time.Duration, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.values == nil {
		m.values = make(map[autoRouteMetricKey]autoRouteMetric)
	}
	v := m.values[key]
	if now.Sub(v.at) > 10*time.Minute {
		v = autoRouteMetric{}
	}
	if len(m.values) >= 512 {
		for k, value := range m.values {
			if now.Sub(value.at) > 10*time.Minute {
				delete(m.values, k)
			}
		}
		if _, ok := m.values[key]; !ok && len(m.values) >= 512 {
			return
		}
	}
	errorValue := 0.0
	if failed {
		errorValue = 1
		v.failure = now
	}
	if v.samples == 0 {
		v.errors = errorValue
	} else {
		v.errors = .8*v.errors + .2*errorValue
	}
	if !failed && latency > 0 {
		if v.latency == 0 {
			v.latency = latency.Seconds()
		} else {
			v.latency = .8*v.latency + .2*latency.Seconds()
		}
	}
	v.samples++
	v.at = now
	m.values[key] = v
}

type autoRouteRank struct {
	group                Group
	price, risk, latency float64
	cooling              bool
}

// Token 价格是统一的 1k 输入 + 1k 输出参考；按次取默认档，不伪称实际账单。
// 真正结算仍走原有计费链（缓存、阶梯、分时和用户套餐均不在这里改写）。
func (s *GatewayService) RankAutoGroupCandidates(ctx context.Context, groups []Group, model string, userID int64, strategy string) []Group {
	now := time.Now()
	ranks := make([]autoRouteRank, 0, len(groups))
	for _, g := range groups {
		rank := autoRouteRank{group: g, price: math.Inf(1), risk: .1, latency: 5}
		if s.resolver != nil {
			price := s.resolver.Resolve(ctx, PricingInput{Model: model, GroupID: &g.ID, Group: &g})
			rate := s.getUserGroupRateMultiplier(ctx, userID, g.ID, g.RateMultiplier)
			if price != nil && price.Mode == BillingModeToken && price.BasePricing != nil {
				value := (price.BasePricing.InputPricePerToken + price.BasePricing.OutputPricePerToken) * 1000 * rate * g.PeakMultiplierAt(now)
				if price.channelPricing != nil {
					value *= price.channelPricing.TimePricing.MultiplierAt(now)
				}
				if value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
					rank.price = value
				}
			}
			if price != nil && (price.Mode == BillingModePerRequest || price.Mode == BillingModeImage) && price.channelPricing != nil && price.channelPricing.PerRequestPrice != nil {
				if price.Mode == BillingModeImage && g.ImageRateIndependent {
					rate = g.ImageRateMultiplier
				}
				value := price.DefaultPerRequestPrice * rate * price.channelPricing.TimePricing.MultiplierAt(now)
				if value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
					rank.price = value
				}
			}
		}
		s.autoRouteMetrics.mu.Lock()
		metric := s.autoRouteMetrics.values[autoRouteMetricKey{g.ID, model}]
		s.autoRouteMetrics.mu.Unlock()
		if metric.samples > 0 && now.Sub(metric.at) <= 10*time.Minute {
			// 少量观测向未知先验收缩，避免单次成功被当作稳定线路。
			weight := math.Min(float64(metric.samples)/10, 1)
			rank.risk = weight*metric.errors + (1-weight)*.1
			if metric.latency > 0 {
				rank.latency = metric.latency
			}
			rank.cooling = !metric.failure.IsZero() && now.Sub(metric.failure) < time.Minute
		}
		ranks = append(ranks, rank)
	}
	sortAutoRouteRanks(ranks, NormalizeRoutingStrategy(strategy))
	result := make([]Group, 0, len(ranks))
	for _, rank := range ranks {
		result = append(result, rank.group)
	}
	return result
}

func sortAutoRouteRanks(ranks []autoRouteRank, strategy string) {
	maxPrice := 0.0
	for _, r := range ranks {
		if !math.IsInf(r.price, 0) && r.price > maxPrice {
			maxPrice = r.price
		}
	}
	score := func(r autoRouteRank) float64 {
		price := 1.0
		if !math.IsInf(r.price, 0) {
			price = 0
			if maxPrice > 0 {
				price = r.price / maxPrice
			}
		}
		return .6*r.risk + .3*price + .1*math.Min(r.latency/30, 1)
	}
	sort.SliceStable(ranks, func(i, j int) bool {
		a, b := ranks[i], ranks[j]
		// 近期故障线路降级而不是永久删除，全部故障时仍允许恢复探测。
		if a.cooling != b.cooling {
			return !a.cooling
		}
		switch strategy {
		case "price":
			if a.price != b.price {
				return a.price < b.price
			}
			if a.risk != b.risk {
				return a.risk < b.risk
			}
		case "stable":
			if a.risk != b.risk {
				return a.risk < b.risk
			}
			if a.latency != b.latency {
				return a.latency < b.latency
			}
			if a.price != b.price {
				return a.price < b.price
			}
		default:
			if score(a) != score(b) {
				return score(a) < score(b)
			}
		}
		return a.group.ID < b.group.ID
	})
}
