package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SubscriptionQuotaGrant records one paid quota package attached to a
// subscription cycle. The payment order is unique so webhook replays cannot
// grant the package twice.
type SubscriptionQuotaGrant struct {
	ent.Schema
}

func (SubscriptionQuotaGrant) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "subscription_quota_grants"}}
}

func (SubscriptionQuotaGrant) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("subscription_id"),
		field.Int64("payment_order_id").Unique(),
		field.Time("cycle_start").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Float("granted_usd").SchemaType(map[string]string{dialect.Postgres: "decimal(20, 8)"}),
		field.Float("used_usd").SchemaType(map[string]string{dialect.Postgres: "decimal(20, 8)"}).Default(0),
		field.String("status").MaxLen(32).Default("active"),
		field.Time("refunded_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (SubscriptionQuotaGrant) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("subscription_id", "cycle_start", "status"),
		index.Fields("payment_order_id"),
	}
}
