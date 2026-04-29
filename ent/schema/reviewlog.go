package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// ReviewLog holds the schema definition for the ReviewLog entity.
// Maps to mr_reviewer_app.review_logs.
type ReviewLog struct {
	ent.Schema
}

// Fields of the ReviewLog.
func (ReviewLog) Fields() []ent.Field {
	return []ent.Field{
		field.Int("mr_id"),
		field.String("project_id"),
		field.String("comment"),
		field.Time("reviewed_at").
			Default(time.Now).
			Immutable(),
	}
}

// Edges of the ReviewLog.
func (ReviewLog) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).
			Ref("review_logs").
			Unique().
			Required(),
	}
}
