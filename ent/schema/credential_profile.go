package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// CredentialProfile holds the schema definition for the CredentialProfile entity.
type CredentialProfile struct {
	ent.Schema
}

// Fields of the CredentialProfile.
func (CredentialProfile) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			Unique().
			NotEmpty().
			Immutable(),
		field.String("name").
			Unique().
			NotEmpty(),
		field.String("version").
			Default("v2c"),
		field.String("sec_level").
			Default("noAuthNoPriv"),
		field.String("community").
			Optional().
			Default(""),
		field.String("username").
			Optional().
			Default(""),
		field.String("auth_protocol").
			Optional().
			Default(""),
		field.String("auth_passphrase").
			Optional().
			Sensitive().
			Default(""),
		field.String("priv_protocol").
			Optional().
			Default(""),
		field.String("priv_passphrase").
			Optional().
			Sensitive().
			Default(""),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the CredentialProfile.
func (CredentialProfile) Edges() []ent.Edge {
	return nil
}

// Indexes of the CredentialProfile.
func (CredentialProfile) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name").Unique(),
	}
}
