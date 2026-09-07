//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAutoGroupSuccessfulAttemptBillsSelectedChannelAndUserRate(t *testing.T) {
	ctx := context.Background()
	user := &User{ID: 7, Status: StatusActive, Balance: 10}
	group := &Group{ID: 1, Status: StatusActive, Platform: PlatformOpenAI, RateMultiplier: 1}
	keys := &APIKeyService{userRepo: autoGroupUserRepo{user: user}, groupRepo: autoGroupGroupRepo{group: group}}
	key := &APIKey{ID: 4, UserID: 7, User: user, AutoGroup: true, Quota: 100}
	first, err := keys.BindAutoGroupAttempt(WithAutoGroupAttempt(ctx, &AutoGroupAttempt{APIKeyID: 4, GroupID: 1}), key)
	require.NoError(t, err)
	// 模拟 A 组无容量，B 组成功；不为失败的调度尝试生成成功用量。
	secondGroup := &Group{ID: 2, Status: StatusActive, Platform: PlatformOpenAI, RateMultiplier: 3}
	keys.groupRepo = autoGroupGroupRepo{group: secondGroup}
	selected, err := keys.BindAutoGroupAttempt(WithAutoGroupAttempt(ctx, &AutoGroupAttempt{APIKeyID: 4, GroupID: 2}), key)
	require.NoError(t, err)

	channels := newTestChannelServiceWithCache(t, &channelCache{
		pricingByGroupModel: map[channelModelKey]*ChannelModelPricing{
			{groupID: 1, model: "gpt-test"}: {BillingMode: BillingModePerRequest, PerRequestPrice: testPtrFloat64(0.1)},
			{groupID: 2, model: "gpt-test"}: {BillingMode: BillingModePerRequest, PerRequestPrice: testPtrFloat64(0.2)},
		},
		channelByGroupID: map[int64]*Channel{1: {ID: 1, Status: StatusActive}, 2: {ID: 2, Status: StatusActive}},
		groupPlatform:    map[int64]string{1: "", 2: ""},
	})
	logs := &openAIRecordUsageLogRepoStub{inserted: true}
	ledger := &openAIRecordUsageBillingRepoStub{}
	userRate := 0.5
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, &openAIUserGroupRateRepoStub{rate: &userRate})
	svc.resolver = NewModelPricingResolver(channels, svc.billingService)
	err = svc.RecordUsage(ctx, &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{RequestID: "auto-group-billing-test", Model: "gpt-test", Usage: OpenAIUsage{InputTokens: 100, OutputTokens: 10}, Duration: time.Second},
		APIKey: selected, User: selected.User, Account: &Account{ID: 3, Type: AccountTypeAPIKey},
		APIKeyService: &openAIRecordUsageAPIKeyQuotaStub{},
	})
	require.NoError(t, err)
	require.Equal(t, 1, ledger.calls)
	require.Equal(t, 1, logs.calls)
	require.Equal(t, int64(2), *logs.lastLog.GroupID)
	require.InDelta(t, 0.2, logs.lastLog.TotalCost, 1e-9)
	require.Equal(t, userRate, logs.lastLog.RateMultiplier)
	require.InDelta(t, 0.1, ledger.lastCmd.BalanceCost, 1e-9)
	require.InDelta(t, 0.1, ledger.lastCmd.APIKeyQuotaCost, 1e-9)
	require.Equal(t, int64(1), *first.GroupID)
	require.Nil(t, key.GroupID)
}
