package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// StateTransitionLog holds the schema definition for the StateTransitionLog entity.
type StateTransitionLog struct {
	ent.Schema
}

// Fields of the StateTransitionLog.
func (StateTransitionLog) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			Unique().
			NotEmpty().
			Immutable(),
		field.String("target").
			NotEmpty(),
		field.String("state_key").
			NotEmpty(),
		field.String("old_value").
			Default(""),
		field.String("new_value").
			Default(""),
		field.String("trigger").
			NotEmpty(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
	}
}

// Edges of the StateTransitionLog.
func (StateTransitionLog) Edges() []ent.Edge {
	return nil
}

// Indexes of the StateTransitionLog.
func (StateTransitionLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("target", "created_at"),
		index.Fields("state_key"),
	}
}
