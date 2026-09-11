package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompositeRouteSourceGroupMigration(t *testing.T) {
	content, err := FS.ReadFile("247_composite_route_source_groups.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS source_group_id BIGINT NULL")
	require.Contains(t, sql, "FOREIGN KEY (source_group_id) REFERENCES groups(id) ON DELETE RESTRICT")
	require.Contains(t, sql, "source_group_id IS NULL OR source_group_id <> group_id")
	require.Contains(t, sql, "idx_composite_model_routes_source_group")
}
