package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// User holds the schema definition for the User entity.
// Maps to mr_reviewer_app.users.
type User struct {
	ent.Schema
}

// Fields of the User.
func (User) Fields() []ent.Field {
	return []ent.Field{
		field.String("username").
			Unique().
			NotEmpty(),
		field.String("password").
			Optional().
			Nillable(),
		field.Int("gitlab_user_id").
			Optional().
			Nillable(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
	}
}

// Edges of the User.
func (User) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("token", UserToken.Type).
			Unique().
			StorageKey(edge.Column("user_id")),
		edge.To("review_logs", ReviewLog.Type).
			StorageKey(edge.Column("user_id")),
	}
}
