package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AuditLog holds the schema definition for the AuditLog entity.
type AuditLog struct {
	ent.Schema
}

// Fields of the AuditLog.
func (AuditLog) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			Unique().
			NotEmpty().
			Immutable(),
		field.String("action").
			NotEmpty(),
		field.String("user_id").
			Default("system"),
		field.String("role").
			Default("system"),
		field.String("ip").
			Default("127.0.0.1"),
		field.String("target_id").
			Optional().
			Default(""),
		field.String("scenario_id").
			Optional().
			Default(""),
		field.JSON("diff", map[string]any{}).
			Optional(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
	}
}

// Edges of the AuditLog.
func (AuditLog) Edges() []ent.Edge {
	return nil
}

// Indexes of the AuditLog.
func (AuditLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("action", "created_at"),
		index.Fields("user_id"),
	}
}
