package infraintegration

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/pluto-han/taskflow/internal/db/sqlc"
	domainintegration "github.com/pluto-han/taskflow/internal/domain/integration"
)

func binding(row db.DiscordBinding, enabled bool) domainintegration.Binding {
	b := domainintegration.Binding{Enabled: enabled, Version: row.Version, Bound: row.ChannelID.Valid}
	if row.ChannelID.Valid {
		b.GuildID = &row.GuildID.String
		b.ChannelID = &row.ChannelID.String
		b.ChannelName = &row.ChannelName.String
		b.BoundAt = &row.BoundAt.Time
	}
	return b
}

func stamp(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }
