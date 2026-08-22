package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ScenarioVersion holds the schema definition for the ScenarioVersion entity.
type ScenarioVersion struct {
	ent.Schema
}

// Fields of the ScenarioVersion.
func (ScenarioVersion) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			Unique().
			NotEmpty().
			Immutable(),
		field.String("scenario_id").
			NotEmpty(),
		field.Int("version").
			Default(1),
		field.Text("dsl_yaml").
			NotEmpty(),
		field.JSON("inputs_schema", map[string]any{}).
			Optional(),
		field.JSON("target_names", []string{}).
			Optional(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
	}
}

// Edges of the ScenarioVersion.
func (ScenarioVersion) Edges() []ent.Edge {
	return nil
}

// Indexes of the ScenarioVersion.
func (ScenarioVersion) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("scenario_id", "version").Unique(),
	}
}
