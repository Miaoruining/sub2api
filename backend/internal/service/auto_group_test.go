package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAutoGroupRegisteredModelsDoesNotInventModelsOrMixPlatforms(t *testing.T) {
	group := Group{ID: 1, Platform: PlatformOpenAI}
	accounts := []Account{{Platform: PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-enabled": "gpt-enabled", "gpt-*": "gpt-enabled", "claude-wrong": "claude-wrong"}}}, {Platform: PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"claude-only": "claude-only"}}}}
	require.Equal(t, []string{"gpt-enabled"}, autoGroupRegisteredModels(group, accounts))
	require.Empty(t, autoGroupRegisteredModels(group, []Account{{Platform: PlatformOpenAI}}))
	group.ModelsListConfig = GroupModelsListConfig{Enabled: true, Models: []string{"gpt-enabled", "gpt-disabled"}}
	accounts[0].Credentials = map[string]any{"model_mapping": map[string]any{"gpt-enabled": "gpt-enabled"}}
	require.Equal(t, []string{"gpt-enabled"}, autoGroupRegisteredModels(group, accounts))
	require.Empty(t, autoGroupRegisteredModels(group, nil))
	require.Empty(t, autoGroupRegisteredModels(Group{Platform: PlatformGrok}, []Account{{Platform: PlatformGrok}}), "默认平台模型不能自动出现在目录")
}

func TestAutoGroupCandidatesKeepsOrderAndRejectsAmbiguousAliases(t *testing.T) {
	catalog := []AutoGroupModels{
		{Group: Group{ID: 2, Platform: PlatformOpenAI}, Models: []string{"gpt-test", "alias"}},
		{Group: Group{ID: 1, Platform: PlatformOpenAI}, Models: []string{"gpt-test"}},
		{Group: Group{ID: 3, Platform: PlatformAnthropic}, Models: []string{"claude-test", "alias"}},
	}
	groups, err := AutoGroupCandidates(catalog, "gpt-test")
	require.NoError(t, err)
	require.Equal(t, []Group{catalog[0].Group, catalog[1].Group}, groups)
	groups, err = AutoGroupCandidates(catalog, "claude-test")
	require.NoError(t, err)
	require.Equal(t, []Group{catalog[2].Group}, groups)
	_, err = AutoGroupCandidates(catalog, "alias")
	require.Error(t, err)
}

type autoGroupUserRepo struct {
	UserRepository
	user *User
}

func (r autoGroupUserRepo) GetByID(context.Context, int64) (*User, error) {
	user := *r.user
	return &user, nil
}

type autoGroupGroupRepo struct {
	GroupRepository
	group *Group
}

func (r autoGroupGroupRepo) GetByID(context.Context, int64) (*Group, error) { return r.group, nil }

func TestAutoGroupBindingRechecksPermissionWithoutMutatingCachedKey(t *testing.T) {
	user := &User{ID: 7, Status: StatusActive, AllowedGroups: []int64{2}, RestrictPublicGroups: true}
	group := &Group{ID: 2, Status: StatusActive, Platform: PlatformOpenAI, RateMultiplier: 3}
	svc := &APIKeyService{userRepo: autoGroupUserRepo{user: user}, groupRepo: autoGroupGroupRepo{group: group}}
	key := &APIKey{ID: 4, UserID: 7, AutoGroup: true, User: user}
	ctx := WithAutoGroupAttempt(context.Background(), &AutoGroupAttempt{APIKeyID: 4, GroupID: 2})
	bound, err := svc.BindAutoGroupAttempt(ctx, key)
	require.NoError(t, err)
	require.Equal(t, int64(2), *bound.GroupID)
	require.Equal(t, 3.0, bound.Group.RateMultiplier)
	require.Nil(t, key.GroupID)
	require.Nil(t, key.Group)
	wrongPlatform := WithAutoGroupAttempt(context.Background(), &AutoGroupAttempt{APIKeyID: 4, GroupID: 2, Platform: PlatformAnthropic})
	_, err = svc.BindAutoGroupAttempt(wrongPlatform, key)
	require.ErrorIs(t, err, ErrGroupNotAllowed)
	user.AllowedGroups = nil
	_, err = svc.BindAutoGroupAttempt(ctx, key)
	require.ErrorIs(t, err, ErrGroupNotAllowed)
	key.AutoGroup = false
	_, err = svc.BindAutoGroupAttempt(ctx, key)
	require.ErrorIs(t, err, ErrGroupNotAllowed)
}

func TestAutoGroupSnapshotRoundTripAndLegacyDefault(t *testing.T) {
	svc := &APIKeyService{}
	key := &APIKey{ID: 4, UserID: 7, AutoGroup: true, User: &User{ID: 7}}
	data, err := json.Marshal(svc.snapshotFromAPIKey(context.Background(), key))
	require.NoError(t, err)
	var snapshot APIKeyAuthSnapshot
	require.NoError(t, json.Unmarshal(data, &snapshot))
	restored := svc.snapshotToAPIKey("test-key", &snapshot)
	require.True(t, restored.AutoGroup)
	require.Nil(t, restored.GroupID)
	var legacy APIKeyAuthSnapshot
	require.NoError(t, json.Unmarshal([]byte(`{"api_key_id":4,"user_id":7}`), &legacy))
	require.False(t, svc.snapshotToAPIKey("legacy", &legacy).AutoGroup)
}
