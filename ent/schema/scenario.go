package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Scenario holds the schema definition for the Scenario entity.
type Scenario struct {
	ent.Schema
}

// Fields of the Scenario.
func (Scenario) Fields() []ent.Field {
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
		field.Int("current_version").
			Default(1),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the Scenario.
func (Scenario) Edges() []ent.Edge {
	return nil
}

// Indexes of the Scenario.
func (Scenario) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name").Unique(),
	}
}
