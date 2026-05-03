package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// UserToken holds the schema definition for the UserToken entity.
// Maps to mr_reviewer_app.user_tokens (1-to-1 with User).
type UserToken struct {
	ent.Schema
}

// Fields of the UserToken.
func (UserToken) Fields() []ent.Field {
	return []ent.Field{
		field.String("token"),
		field.String("web_url").
			Optional().
			Nillable(),
		field.Int64("project_id").
			Optional().
			Nillable(),
		field.Int64("last_event_id").
			Default(0),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the UserToken.
func (UserToken) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).
			Ref("token").
			Unique().
			Required(),
	}
}
