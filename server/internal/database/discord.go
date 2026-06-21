package database

import "time"

type DiscordBotStatus struct {
	BotUserTag     string `json:"botUserTag"`
	GuildID        string `json:"guildId"`
	GuildName      string `json:"guildName"`
	CommandCount   int    `json:"commandCount"`
	WelcomeEnabled bool   `json:"welcomeEnabled"`
	LastSeenAt     int64  `json:"lastSeenAt"`
}

func (d *DB) UpsertDiscordBotStatus(status DiscordBotStatus) error {
	if status.LastSeenAt == 0 {
		status.LastSeenAt = time.Now().Unix()
	}
	_, err := d.db.Exec(`
		INSERT INTO discord_bot_status (
			id, bot_user_tag, guild_id, guild_name, command_count, welcome_enabled, last_seen_at
		) VALUES ('local', $1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET
			bot_user_tag = EXCLUDED.bot_user_tag,
			guild_id = EXCLUDED.guild_id,
			guild_name = EXCLUDED.guild_name,
			command_count = EXCLUDED.command_count,
			welcome_enabled = EXCLUDED.welcome_enabled,
			last_seen_at = EXCLUDED.last_seen_at
	`, status.BotUserTag, status.GuildID, status.GuildName, status.CommandCount, status.WelcomeEnabled, status.LastSeenAt)
	return err
}

func (d *DB) GetDiscordBotStatus() (*DiscordBotStatus, error) {
	var status DiscordBotStatus
	err := d.db.QueryRow(`
		SELECT bot_user_tag, guild_id, guild_name, command_count, welcome_enabled, last_seen_at
		FROM discord_bot_status WHERE id = 'local'
	`).Scan(&status.BotUserTag, &status.GuildID, &status.GuildName, &status.CommandCount, &status.WelcomeEnabled, &status.LastSeenAt)
	if err != nil {
		return nil, err
	}
	return &status, nil
}
