package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// JobStep holds the schema definition for the JobStep entity.
type JobStep struct {
	ent.Schema
}

// Fields of the JobStep.
func (JobStep) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			Unique().
			NotEmpty().
			Immutable(),
		field.String("job_id").
			NotEmpty(),
		field.String("step_id").
			NotEmpty(),
		field.Int("step_order").
			Default(0),
		field.String("step_type").
			NotEmpty(),
		field.String("status").
			Default("PENDING"),
		field.JSON("result_output", map[string]any{}).
			Optional(),
		field.String("error").
			Optional().
			Default(""),
		field.Time("executed_at").
			Default(time.Now),
	}
}

// Edges of the JobStep.
func (JobStep) Edges() []ent.Edge {
	return nil
}

// Indexes of the JobStep.
func (JobStep) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("job_id", "step_order"),
	}
}
