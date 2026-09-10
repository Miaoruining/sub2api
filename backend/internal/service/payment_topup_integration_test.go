//go:build integration

package service_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/subscriptionquotagrant"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/lib/pq"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

var (
	topupIntegrationDB     *sql.DB
	topupIntegrationClient *dbent.Client
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	container, err := tcpostgres.Run(
		ctx,
		"postgres:18.1-alpine3.23",
		tcpostgres.WithDatabase("sub2api_test"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		log.Printf("failed to start postgres container: %v", err)
		os.Exit(1)
	}
	terminate := func() {
		if err := container.Terminate(ctx); err != nil {
			log.Printf("failed to terminate postgres container: %v", err)
		}
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable", "TimeZone=UTC")
	if err != nil {
		log.Printf("failed to get postgres connection string: %v", err)
		terminate()
		os.Exit(1)
	}
	topupIntegrationDB, err = sql.Open("postgres", dsn)
	if err != nil {
		log.Printf("failed to open postgres: %v", err)
		terminate()
		os.Exit(1)
	}
	topupIntegrationDB.SetMaxOpenConns(4)
	topupIntegrationDB.SetMaxIdleConns(4)
	if err := topupIntegrationDB.PingContext(ctx); err != nil {
		log.Printf("failed to ping postgres: %v", err)
		_ = topupIntegrationDB.Close()
		terminate()
		os.Exit(1)
	}
	if err := repository.ApplyMigrations(ctx, topupIntegrationDB); err != nil {
		log.Printf("failed to apply migrations: %v", err)
		_ = topupIntegrationDB.Close()
		terminate()
		os.Exit(1)
	}

	drv := entsql.OpenDB(dialect.Postgres, topupIntegrationDB)
	topupIntegrationClient = dbent.NewClient(dbent.Driver(drv))

	code := m.Run()
	_ = topupIntegrationClient.Close()
	_ = topupIntegrationDB.Close()
	terminate()
	os.Exit(code)
}

type topupIntegrationFixture struct {
	user     *dbent.User
	group    *dbent.Group
	sub      *dbent.UserSubscription
	provider *dbent.PaymentProviderInstance
	order    *dbent.PaymentOrder
	cycle    time.Time
}

func newTopupIntegrationFixture(t *testing.T, orderStatus string) *topupIntegrationFixture {
	t.Helper()
	ctx := context.Background()
	unique := strconv.FormatInt(time.Now().UnixNano(), 10)
	name := "topup-" + unique

	user, err := topupIntegrationClient.User.Create().
		SetEmail("topup-" + name + "-" + unique + "@example.com").
		SetPasswordHash("integration-test-hash").
		SetUsername("topup-" + name).
		Save(ctx)
	require.NoError(t, err)

	group, err := topupIntegrationClient.Group.Create().
		SetName("topup-" + name + "-" + unique).
		SetPlatform(service.PlatformOpenAI).
		SetSubscriptionType(service.SubscriptionTypeSubscription).
		SetMonthlyLimitUsd(100).
		Save(ctx)
	require.NoError(t, err)

	cycle := time.Now().UTC().Truncate(time.Microsecond)
	sub, err := topupIntegrationClient.UserSubscription.Create().
		SetUserID(user.ID).
		SetGroupID(group.ID).
		SetStartsAt(cycle).
		SetExpiresAt(cycle.AddDate(0, 0, 30)).
		SetStatus(service.SubscriptionStatusActive).
		Save(ctx)
	require.NoError(t, err)

	provider, err := topupIntegrationClient.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeStripe).
		SetName("topup-integration-provider-" + unique).
		SetConfig("{}").
		SetEnabled(true).
		SetRefundEnabled(true).
		SetAllowUserRefund(true).
		Save(ctx)
	require.NoError(t, err)

	order, err := topupIntegrationClient.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetRechargeCode("PAY-" + unique).
		SetOutTradeNo("sub2_topup_" + unique).
		SetPaymentType(payment.TypeStripe).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeSubscriptionTopup).
		SetStatus(orderStatus).
		SetExpiresAt(time.Now().UTC().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("integration-test").
		SetSubscriptionGroupID(group.ID).
		SetSubscriptionDays(30).
		SetSubscriptionID(sub.ID).
		SetSubscriptionCycleStart(cycle).
		SetQuotaUsd(11).
		SetSubscriptionAllowActiveRenewal(false).
		SetProviderInstanceID(strconv.FormatInt(int64(provider.ID), 10)).
		SetProviderKey(payment.TypeStripe).
		Save(ctx)
	require.NoError(t, err)

	return &topupIntegrationFixture{user: user, group: group, sub: sub, provider: provider, order: order, cycle: cycle}
}

func newTopupPaymentService() *service.PaymentService {
	return service.NewPaymentService(topupIntegrationClient, nil, nil, nil, nil, nil, nil, nil, nil)
}

func createTopupOrderForIntegration(t *testing.T, f *topupIntegrationFixture, orderStatus string) *dbent.PaymentOrder {
	t.Helper()
	ctx := context.Background()
	unique := strconv.FormatInt(time.Now().UnixNano(), 10)
	order, err := topupIntegrationClient.PaymentOrder.Create().
		SetUserID(f.user.ID).
		SetUserEmail(f.user.Email).
		SetUserName(f.user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetRechargeCode("PAY-" + unique).
		SetOutTradeNo("sub2_topup_" + unique).
		SetPaymentType(payment.TypeStripe).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeSubscriptionTopup).
		SetStatus(orderStatus).
		SetExpiresAt(time.Now().UTC().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("integration-test").
		SetSubscriptionGroupID(f.group.ID).
		SetSubscriptionDays(30).
		SetSubscriptionID(f.sub.ID).
		SetSubscriptionCycleStart(f.cycle).
		SetQuotaUsd(11).
		SetSubscriptionAllowActiveRenewal(false).
		SetProviderInstanceID(strconv.FormatInt(int64(f.provider.ID), 10)).
		SetProviderKey(payment.TypeStripe).
		Save(ctx)
	require.NoError(t, err)
	return order
}

func TestSubscriptionTopupIntegrationFulfillsGrantWithoutChangingSubscription(t *testing.T) {
	ctx := context.Background()
	f := newTopupIntegrationFixture(t, service.OrderStatusPaid)
	before, err := topupIntegrationClient.UserSubscription.Get(ctx, f.sub.ID)
	require.NoError(t, err)

	require.NoError(t, newTopupPaymentService().ExecuteSubscriptionFulfillment(ctx, f.order.ID))

	after, err := topupIntegrationClient.UserSubscription.Get(ctx, f.sub.ID)
	require.NoError(t, err)
	require.WithinDuration(t, before.StartsAt, after.StartsAt, time.Microsecond)
	require.WithinDuration(t, before.ExpiresAt, after.ExpiresAt, time.Microsecond)
	require.Equal(t, before.DailyUsageUsd, after.DailyUsageUsd)
	require.Equal(t, before.WeeklyUsageUsd, after.WeeklyUsageUsd)
	require.Equal(t, before.MonthlyUsageUsd, after.MonthlyUsageUsd)

	grant, err := topupIntegrationClient.SubscriptionQuotaGrant.Query().
		Where(subscriptionquotagrant.PaymentOrderIDEQ(f.order.ID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, f.sub.ID, grant.SubscriptionID)
	require.WithinDuration(t, f.cycle, grant.CycleStart, time.Microsecond)
	require.Equal(t, 11.0, grant.GrantedUsd)
	require.Equal(t, 0.0, grant.UsedUsd)
	require.Equal(t, "active", grant.Status)

	order, err := topupIntegrationClient.PaymentOrder.Get(ctx, f.order.ID)
	require.NoError(t, err)
	require.Equal(t, service.OrderStatusCompleted, order.Status)
}

func TestSubscriptionTopupIntegrationConcurrentDuplicateCallbacksCreateOneGrant(t *testing.T) {
	ctx := context.Background()
	f := newTopupIntegrationFixture(t, service.OrderStatusPending)
	n := &payment.PaymentNotification{
		TradeNo: f.order.OutTradeNo + "-trade",
		OrderID: f.order.OutTradeNo,
		Amount:  10,
		Status:  payment.NotificationStatusSuccess,
	}
	svc := newTopupPaymentService()

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- svc.HandlePaymentNotification(ctx, n, payment.TypeStripe)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		// A concurrent loser may observe the short-lived RECHARGING lease and
		// return a retryable conflict; it must not create a second grant.
		if err != nil {
			t.Logf("concurrent duplicate callback returned retryable result: %v", err)
		}
	}

	for i := 0; i < 3; i++ {
		order, err := topupIntegrationClient.PaymentOrder.Get(ctx, f.order.ID)
		require.NoError(t, err)
		if order.Status == service.OrderStatusCompleted {
			break
		}
		_ = newTopupPaymentService().HandlePaymentNotification(ctx, n, payment.TypeStripe)
		time.Sleep(20 * time.Millisecond)
	}
	order, err := topupIntegrationClient.PaymentOrder.Get(ctx, f.order.ID)
	require.NoError(t, err)
	require.Equal(t, service.OrderStatusCompleted, order.Status)
	count, err := topupIntegrationClient.SubscriptionQuotaGrant.Query().
		Where(subscriptionquotagrant.PaymentOrderIDEQ(f.order.ID)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestSubscriptionTopupIntegrationQuotaReadIncludesAmbiguousAndNewGrant(t *testing.T) {
	ctx := context.Background()
	f := newTopupIntegrationFixture(t, service.OrderStatusPaid)
	_, err := topupIntegrationClient.Group.UpdateOneID(f.group.ID).
		SetMonthlyLimitUsd(55).
		Save(ctx)
	require.NoError(t, err)
	_, err = topupIntegrationClient.UserSubscription.UpdateOneID(f.sub.ID).
		SetMonthlyUsageUsd(55).
		Save(ctx)
	require.NoError(t, err)

	require.NoError(t, newTopupPaymentService().ExecuteSubscriptionFulfillment(ctx, f.order.ID))
	firstGrant, err := topupIntegrationClient.SubscriptionQuotaGrant.Query().
		Where(subscriptionquotagrant.PaymentOrderIDEQ(f.order.ID)).Only(ctx)
	require.NoError(t, err)

	apiKey, err := topupIntegrationClient.APIKey.Create().
		SetUserID(f.user.ID).
		SetGroupID(f.group.ID).
		SetKey("sk-topup-" + strconv.FormatInt(time.Now().UnixNano(), 10)).
		SetName("topup-usage").
		Save(ctx)
	require.NoError(t, err)
	subscriptionID := f.sub.ID
	cycleStart := f.cycle
	usageRepo := repository.NewUsageBillingRepository(topupIntegrationClient, topupIntegrationDB)
	result, err := usageRepo.Apply(ctx, &service.UsageBillingCommand{
		RequestID:              "topup-ambiguous-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		APIKeyID:               apiKey.ID,
		UserID:                 f.user.ID,
		SubscriptionID:         &subscriptionID,
		SubscriptionCycleStart: &cycleStart,
		SubscriptionCost:       12,
	})
	require.NoError(t, err)
	require.True(t, result.Applied)

	firstGrant, err = topupIntegrationClient.SubscriptionQuotaGrant.Get(ctx, firstGrant.ID)
	require.NoError(t, err)
	require.Equal(t, service.SubscriptionQuotaGrantStatusAmbiguous, firstGrant.Status)
	require.InDelta(t, 11, firstGrant.UsedUsd, 0.000001)

	secondOrder := createTopupOrderForIntegration(t, f, service.OrderStatusPaid)
	require.NoError(t, newTopupPaymentService().ExecuteSubscriptionFulfillment(ctx, secondOrder.ID))
	oldCycleOrder := createTopupOrderForIntegration(t, f, service.OrderStatusCompleted)
	_, err = topupIntegrationClient.SubscriptionQuotaGrant.Create().
		SetSubscriptionID(f.sub.ID).
		SetPaymentOrderID(oldCycleOrder.ID).
		SetCycleStart(f.cycle.AddDate(0, 0, -30)).
		SetGrantedUsd(99).
		SetUsedUsd(0).
		SetStatus(service.SubscriptionQuotaGrantStatusActive).
		Save(ctx)
	require.NoError(t, err)

	loaded, err := repository.NewUserSubscriptionRepository(topupIntegrationClient).GetByID(ctx, f.sub.ID)
	require.NoError(t, err)
	require.InDelta(t, 67, loaded.MonthlyUsageUSD, 0.000001)
	require.InDelta(t, 22, loaded.TopupGrantedUSD, 0.000001)
	require.InDelta(t, 11, loaded.TopupUsedUSD, 0.000001)
	require.InDelta(t, 10, 55+loaded.TopupGrantedUSD-loaded.MonthlyUsageUSD, 0.000001)
}

func TestSubscriptionTopupIntegrationCrossCycleApplyPersistsFailureIdempotently(t *testing.T) {
	ctx := context.Background()
	f := newTopupIntegrationFixture(t, service.OrderStatusPaid)
	_, err := topupIntegrationClient.User.UpdateOneID(f.user.ID).
		SetBalance(100).
		Save(ctx)
	require.NoError(t, err)

	apiKey, err := topupIntegrationClient.APIKey.Create().
		SetUserID(f.user.ID).
		SetGroupID(f.group.ID).
		SetKey("sk-topup-failure-" + strconv.FormatInt(time.Now().UnixNano(), 10)).
		SetName("topup-cross-cycle-failure").
		Save(ctx)
	require.NoError(t, err)

	beforeSubscription, err := topupIntegrationClient.UserSubscription.Get(ctx, f.sub.ID)
	require.NoError(t, err)
	beforeUser, err := topupIntegrationClient.User.Get(ctx, f.user.ID)
	require.NoError(t, err)

	subscriptionID := f.sub.ID
	oldCycle := f.cycle.AddDate(0, 0, -30)
	requestID := "topup-cross-cycle-failure-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	command := &service.UsageBillingCommand{
		RequestID:              requestID,
		APIKeyID:               apiKey.ID,
		UserID:                 f.user.ID,
		SubscriptionID:         &subscriptionID,
		SubscriptionCycleStart: &oldCycle,
		SubscriptionCost:       7.25,
		BalanceCost:            3.5,
	}
	usageRepo := repository.NewUsageBillingRepository(topupIntegrationClient, topupIntegrationDB)
	for i := 0; i < 2; i++ {
		_, err = usageRepo.Apply(ctx, command)
		require.ErrorIs(t, err, service.ErrSubscriptionCycleMismatch)
	}

	afterSubscription, err := topupIntegrationClient.UserSubscription.Get(ctx, f.sub.ID)
	require.NoError(t, err)
	require.InDelta(t, beforeSubscription.DailyUsageUsd, afterSubscription.DailyUsageUsd, 0.000001)
	require.InDelta(t, beforeSubscription.WeeklyUsageUsd, afterSubscription.WeeklyUsageUsd, 0.000001)
	require.InDelta(t, beforeSubscription.MonthlyUsageUsd, afterSubscription.MonthlyUsageUsd, 0.000001)
	afterUser, err := topupIntegrationClient.User.Get(ctx, f.user.ID)
	require.NoError(t, err)
	require.InDelta(t, beforeUser.Balance, afterUser.Balance, 0.000001)

	var requestIDFromDB string
	var apiKeyIDFromDB, subscriptionIDFromDB int64
	var cycleSnapshot time.Time
	var commandJSON []byte
	var reason string
	err = topupIntegrationDB.QueryRowContext(ctx, `
		SELECT request_id, api_key_id, subscription_id, cycle_snapshot, command, reason
		FROM subscription_billing_failures
		WHERE request_id = $1 AND api_key_id = $2
	`, requestID, apiKey.ID).Scan(
		&requestIDFromDB, &apiKeyIDFromDB, &subscriptionIDFromDB, &cycleSnapshot, &commandJSON, &reason,
	)
	require.NoError(t, err)
	require.Equal(t, requestID, requestIDFromDB)
	require.Equal(t, int64(apiKey.ID), apiKeyIDFromDB)
	require.Equal(t, f.sub.ID, subscriptionIDFromDB)
	require.WithinDuration(t, oldCycle, cycleSnapshot, time.Microsecond)
	require.Contains(t, reason, "subscription cycle")

	var persistedCommand map[string]any
	require.NoError(t, json.Unmarshal(commandJSON, &persistedCommand))
	require.Equal(t, 7.25, persistedCommand["SubscriptionCost"])
	require.Equal(t, 3.5, persistedCommand["BalanceCost"])
	require.Equal(t, oldCycle.UTC().Format(time.RFC3339Nano), persistedCommand["SubscriptionCycleStart"])

	var failureCount int
	err = topupIntegrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM subscription_billing_failures
		WHERE request_id = $1 AND api_key_id = $2
	`, requestID, apiKey.ID).Scan(&failureCount)
	require.NoError(t, err)
	require.Equal(t, 1, failureCount)
}

func newSubscriptionAwarePaymentService() (*service.PaymentService, func()) {
	groupRepo := repository.NewGroupRepository(topupIntegrationClient, topupIntegrationDB)
	userSubRepo := repository.NewUserSubscriptionRepository(topupIntegrationClient)
	subscriptionSvc := service.NewSubscriptionService(groupRepo, userSubRepo, nil, topupIntegrationClient, nil)
	paymentSvc := service.NewPaymentService(topupIntegrationClient, nil, nil, nil, subscriptionSvc, nil, nil, groupRepo, nil)
	return paymentSvc, subscriptionSvc.Stop
}

func createBaseOrderForIntegration(t *testing.T, f *topupIntegrationFixture, orderStatus string) *dbent.PaymentOrder {
	t.Helper()
	ctx := context.Background()
	unique := strconv.FormatInt(time.Now().UnixNano(), 10)
	order, err := topupIntegrationClient.PaymentOrder.Create().
		SetUserID(f.user.ID).
		SetUserEmail(f.user.Email).
		SetUserName(f.user.Username).
		SetAmount(55).
		SetPayAmount(55).
		SetRechargeCode("PAY-" + unique).
		SetOutTradeNo("sub2_base_" + unique).
		SetPaymentType(payment.TypeStripe).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeSubscription).
		SetStatus(orderStatus).
		SetExpiresAt(time.Now().UTC().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("integration-test").
		SetSubscriptionGroupID(f.group.ID).
		SetSubscriptionDays(30).
		SetSubscriptionAllowActiveRenewal(false).
		SetProviderInstanceID(strconv.FormatInt(int64(f.provider.ID), 10)).
		SetProviderKey(payment.TypeStripe).
		Save(ctx)
	require.NoError(t, err)
	return order
}

func TestSubscriptionBaseIntegrationConcurrentNonRenewalDoesNotExtend(t *testing.T) {
	ctx := context.Background()
	f := newTopupIntegrationFixture(t, service.OrderStatusPending)
	err := topupIntegrationClient.UserSubscription.DeleteOneID(f.sub.ID).Exec(ctx)
	require.NoError(t, err)

	first := createBaseOrderForIntegration(t, f, service.OrderStatusPaid)
	second := createBaseOrderForIntegration(t, f, service.OrderStatusPaid)
	paymentSvc, stop := newSubscriptionAwarePaymentService()
	defer stop()

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, order := range []*dbent.PaymentOrder{first, second} {
		wg.Add(1)
		go func(orderID int64) {
			defer wg.Done()
			<-start
			errs <- paymentSvc.ExecuteSubscriptionFulfillment(ctx, orderID)
		}(order.ID)
	}
	close(start)
	wg.Wait()
	close(errs)

	completed := 0
	refundRequested := 0
	for _, order := range []*dbent.PaymentOrder{first, second} {
		updated, getErr := topupIntegrationClient.PaymentOrder.Get(ctx, order.ID)
		require.NoError(t, getErr)
		switch updated.Status {
		case service.OrderStatusCompleted:
			completed++
		case service.OrderStatusRefundRequested:
			refundRequested++
		default:
			t.Fatalf("unexpected base order status %q", updated.Status)
		}
	}
	for err := range errs {
		if err != nil {
			t.Logf("concurrent base fulfillment returned expected conflict/refund result: %v", err)
		}
	}
	require.Equal(t, 1, completed)
	require.Equal(t, 1, refundRequested)

	subscription, err := topupIntegrationClient.UserSubscription.Query().
		Where(usersubscription.UserIDEQ(f.user.ID), usersubscription.GroupIDEQ(f.group.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.WithinDuration(t, subscription.StartsAt.AddDate(0, 0, 30), subscription.ExpiresAt, time.Microsecond)
}

func TestSubscriptionTopupIntegrationCrossCycleCallbackRequiresRefundAndNoGrant(t *testing.T) {
	ctx := context.Background()
	f := newTopupIntegrationFixture(t, service.OrderStatusPending)
	newCycle := f.cycle.AddDate(0, 0, 30)
	_, err := topupIntegrationClient.UserSubscription.UpdateOneID(f.sub.ID).
		SetStartsAt(newCycle).
		SetExpiresAt(newCycle.AddDate(0, 0, 30)).
		SetStatus(service.SubscriptionStatusActive).
		Save(ctx)
	require.NoError(t, err)

	n := &payment.PaymentNotification{TradeNo: "cross-cycle-trade", OrderID: f.order.OutTradeNo, Amount: 10, Status: payment.NotificationStatusSuccess}
	require.Error(t, newTopupPaymentService().HandlePaymentNotification(ctx, n, payment.TypeStripe))

	order, err := topupIntegrationClient.PaymentOrder.Get(ctx, f.order.ID)
	require.NoError(t, err)
	require.Equal(t, service.OrderStatusRefundRequested, order.Status)
	count, err := topupIntegrationClient.SubscriptionQuotaGrant.Query().
		Where(subscriptionquotagrant.PaymentOrderIDEQ(f.order.ID)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, count)
}

func TestSubscriptionTopupIntegrationExpiredCallbackRequiresRefundAndNoGrant(t *testing.T) {
	ctx := context.Background()
	f := newTopupIntegrationFixture(t, service.OrderStatusPending)
	_, err := topupIntegrationClient.UserSubscription.UpdateOneID(f.sub.ID).
		SetExpiresAt(time.Now().UTC().Add(-time.Hour)).
		Save(ctx)
	require.NoError(t, err)

	n := &payment.PaymentNotification{TradeNo: "expired-cycle-trade", OrderID: f.order.OutTradeNo, Amount: 10, Status: payment.NotificationStatusSuccess}
	require.Error(t, newTopupPaymentService().HandlePaymentNotification(ctx, n, payment.TypeStripe))

	order, err := topupIntegrationClient.PaymentOrder.Get(ctx, f.order.ID)
	require.NoError(t, err)
	require.Equal(t, service.OrderStatusRefundRequested, order.Status)
	count, err := topupIntegrationClient.SubscriptionQuotaGrant.Query().
		Where(subscriptionquotagrant.PaymentOrderIDEQ(f.order.ID)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, count)
}

func TestSubscriptionTopupIntegrationRefundPreparationAllowsUnusedGrant(t *testing.T) {
	ctx := context.Background()
	f := newTopupIntegrationFixture(t, service.OrderStatusCompleted)
	_, err := topupIntegrationClient.SubscriptionQuotaGrant.Create().
		SetSubscriptionID(f.sub.ID).
		SetPaymentOrderID(f.order.ID).
		SetCycleStart(f.cycle).
		SetGrantedUsd(11).
		SetUsedUsd(0).
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)

	plan, early, err := newTopupPaymentService().PrepareRefund(ctx, f.order.ID, f.order.Amount, "integration unused grant", false, false)
	require.NoError(t, err)
	require.Nil(t, early)
	require.NotNil(t, plan)
	require.Equal(t, payment.DeductionTypeSubscriptionTopup, plan.DeductionType)
	require.Equal(t, f.order.ID, plan.OrderID)
}

func TestSubscriptionTopupIntegrationRefundPreparationBlocksUsedGrant(t *testing.T) {
	ctx := context.Background()
	f := newTopupIntegrationFixture(t, service.OrderStatusCompleted)
	grant, err := topupIntegrationClient.SubscriptionQuotaGrant.Create().
		SetSubscriptionID(f.sub.ID).
		SetPaymentOrderID(f.order.ID).
		SetCycleStart(f.cycle).
		SetGrantedUsd(11).
		SetUsedUsd(1).
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)

	plan, early, err := newTopupPaymentService().PrepareRefund(ctx, f.order.ID, f.order.Amount, "integration used grant", false, false)
	require.NoError(t, err)
	require.Nil(t, plan)
	require.NotNil(t, early)
	require.True(t, early.RequireForce)

	order, err := topupIntegrationClient.PaymentOrder.Get(ctx, f.order.ID)
	require.NoError(t, err)
	require.Equal(t, service.OrderStatusRefundRequested, order.Status)
	refetched, err := topupIntegrationClient.SubscriptionQuotaGrant.Get(ctx, grant.ID)
	require.NoError(t, err)
	require.Equal(t, 1.0, refetched.UsedUsd)
}

func TestSubscriptionTopupIntegrationMissingGrantManualRefundPath(t *testing.T) {
	ctx := context.Background()
	f := newTopupIntegrationFixture(t, service.OrderStatusCompleted)

	plan, early, err := newTopupPaymentService().PrepareRefund(ctx, f.order.ID, f.order.Amount, "integration missing grant", false, false)
	require.NoError(t, err)
	require.Nil(t, plan)
	require.NotNil(t, early)
	require.True(t, early.RequireForce)

	order, err := topupIntegrationClient.PaymentOrder.Get(ctx, f.order.ID)
	require.NoError(t, err)
	require.Equal(t, service.OrderStatusRefundRequested, order.Status)

	plan, early, err = newTopupPaymentService().PrepareRefund(ctx, f.order.ID, f.order.Amount, "integration missing grant force", true, false)
	require.NoError(t, err)
	require.Nil(t, early)
	require.NotNil(t, plan)
	require.Equal(t, payment.DeductionTypeNone, plan.DeductionType)
	require.Equal(t, int64(0), plan.TopupGrantID)
}
