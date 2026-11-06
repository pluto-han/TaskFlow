-- name: EnsureDiscordBinding :exec
INSERT INTO discord_bindings(owner_id) VALUES ($1) ON CONFLICT(owner_id) DO NOTHING;

-- name: GetDiscordBinding :one
SELECT * FROM discord_bindings WHERE owner_id=$1;

-- name: LockDiscordBinding :one
SELECT * FROM discord_bindings WHERE owner_id=$1 FOR UPDATE;

-- name: LockDiscordCode :one
SELECT * FROM discord_bindings WHERE code_hash=$1 FOR UPDATE;

-- name: IssueDiscordCode :exec
UPDATE discord_bindings SET code_hash=$2,code_expires_at=$3,rate_started_at=$4,rate_count=$5 WHERE owner_id=$1;

-- name: ConsumeDiscordCode :exec
UPDATE discord_bindings SET guild_id=$2,channel_id=$3,channel_name=$4,discord_user_id=$5,bound_at=$6,
version=version+1,code_hash=NULL,code_expires_at=NULL WHERE owner_id=$1;

-- name: UnbindDiscord :exec
UPDATE discord_bindings SET guild_id=NULL,channel_id=NULL,channel_name=NULL,discord_user_id=NULL,bound_at=NULL,
version=version+1,code_hash=NULL,code_expires_at=NULL WHERE owner_id=$1;

-- name: FindDiscordInteraction :one
SELECT interaction_id FROM discord_interactions WHERE interaction_id=$1;

-- name: SaveDiscordInteraction :execrows
INSERT INTO discord_interactions(interaction_id,owner_id) VALUES($1,$2) ON CONFLICT DO NOTHING;
