package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Target holds the schema definition for the Target entity.
type Target struct {
	ent.Schema
}

// Fields of the Target.
func (Target) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			Unique().
			NotEmpty().
			Immutable(),
		field.String("name").
			Unique().
			NotEmpty(),
		field.String("description").
			Optional().
			Default(""),
		field.String("host").
			NotEmpty(),
		field.Int("port").
			Default(161),
		field.String("status").
			Default("ONLINE"),
		field.JSON("labels", map[string]string{}).
			Optional(),
		field.String("credential_id").
			NotEmpty(),
		field.JSON("polling_config", map[string]any{}).
			Optional(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the Target.
func (Target) Edges() []ent.Edge {
	return nil
}

// Indexes of the Target.
func (Target) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name").Unique(),
		index.Fields("status"),
	}
}
