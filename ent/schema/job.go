package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Job holds the schema definition for the Job entity.
type Job struct {
	ent.Schema
}

// Fields of the Job.
func (Job) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			Unique().
			NotEmpty().
			Immutable(),
		field.String("scenario_id").
			NotEmpty(),
		field.Int("scenario_version").
			Default(1),
		field.String("status").
			Default("QUEUED"),
		field.JSON("dynamic_inputs", map[string]any{}).
			Optional(),
		field.JSON("locked_targets", []string{}).
			Optional(),
		field.String("triggered_by").
			Optional().
			Default("admin"),
		field.Time("started_at").
			Optional().
			Nillable(),
		field.Time("finished_at").
			Optional().
			Nillable(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
	}
}

// Edges of the Job.
func (Job) Edges() []ent.Edge {
	return nil
}

// Indexes of the Job.
func (Job) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("scenario_id"),
		index.Fields("status"),
	}
}
