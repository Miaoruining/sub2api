package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

type plazaStyleRepo struct {
	SettingRepository
	style string
}

func (r plazaStyleRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := map[string]string{SettingKeyModelPlazaEnabled: "true", SettingKeyModelPlazaStyle: r.style}
	result := map[string]string{}
	for _, key := range keys {
		result[key] = values[key]
	}
	return result, nil
}
func TestModelPlazaRuntimeStyleDefaultsAndSwitches(t *testing.T) {
	for _, tc := range []struct{ stored, want string }{{"", "cards"}, {"cards", "cards"}, {"sub2api", "sub2api"}, {"invalid", "cards"}} {
		svc := &SettingService{settingRepo: plazaStyleRepo{style: tc.stored}}
		runtime := svc.GetModelPlazaRuntime(context.Background())
		require.True(t, runtime.Enabled)
		require.Equal(t, tc.want, runtime.Style)
	}
}
