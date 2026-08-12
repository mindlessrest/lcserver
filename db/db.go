package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type Database struct {
	mu sync.RWMutex
	db *sql.DB
}

type UserProfile struct {
	UUID              string  `json:"uuid"`
	Username          string  `json:"username"`
	Rank              string  `json:"rank"`
	PlusColor         uint32  `json:"plus_color"`
	LogoColor         uint32  `json:"logo_color"`
	LogoAlwaysShow    bool    `json:"logo_always_show"`
	ArtistTools       bool    `json:"artist_tools"`
	TesterTools       bool    `json:"tester_tools"`
	EquippedCosmetics []int32 `json:"equipped_cosmetics"`
	EquippedEmotes    []int32 `json:"equipped_emotes"`
	EquippedBadge     int32   `json:"equipped_badge"`
	EquippedSprays    []int32 `json:"equipped_sprays"`
	OwnedCosmetics    []int32 `json:"owned_cosmetics"`
	OwnedEmotes       []int32 `json:"owned_emotes"`
	OwnedBadges       []int32 `json:"owned_badges"`
	OwnedSprays       []int32 `json:"owned_sprays"`
	// Visibility prefs
	VisClothCloak     bool  `json:"vis_cloth_cloak"`
	VisHatsOverHelmet bool  `json:"vis_hats_over_helmet"`
	VisHatsOverSkin   bool  `json:"vis_hats_over_skin"`
	VisOverChestplate bool  `json:"vis_over_chestplate"`
	VisOverLeggings   bool  `json:"vis_over_leggings"`
	VisOverBoots      bool  `json:"vis_over_boots"`
	VisFlipShoulder   bool  `json:"vis_flip_shoulder"`
	CreatedAt         int64 `json:"created_at"`
	LastSeen          int64 `json:"last_seen"`
}

type FriendEntry struct {
	FriendUUID string
	State      string
	Pinned     bool
	CreatedAt  int64
}

type DirectMessage struct {
	ID            string
	SenderUUID    string
	RecipientUUID string
	Contents      []byte
	SentAt        int64
}

type CosmeticOutfit struct {
	ID        string
	Data      []byte
	UpdatedAt int64
}

func Open(path string) (*Database, error) {
	sdb, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	sdb.SetMaxOpenConns(1)
	sdb.SetMaxIdleConns(1)
	for _, pragma := range []string{
		`PRAGMA busy_timeout=5000`,
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=FULL`,
		`PRAGMA foreign_keys=ON`,
	} {
		if _, err := sdb.Exec(pragma); err != nil {
			_ = sdb.Close()
			return nil, fmt.Errorf("sqlite configure: %w", err)
		}
	}

	d := &Database{db: sdb}
	if err := d.migrate(); err != nil {
		_ = sdb.Close()
		return nil, err
	}
	return d, nil
}

func (d *Database) Close() error {
	return d.db.Close()
}

func (d *Database) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS users (
			uuid TEXT PRIMARY KEY,
			username TEXT NOT NULL DEFAULT '',
			rank TEXT NOT NULL DEFAULT 'ADMIN',
			plus_color INTEGER NOT NULL DEFAULT 0,
			logo_color INTEGER NOT NULL DEFAULT 0,
			logo_always_show INTEGER NOT NULL DEFAULT 1,
			artist_tools INTEGER NOT NULL DEFAULT 1,
			tester_tools INTEGER NOT NULL DEFAULT 1,
			equipped_cosmetics TEXT NOT NULL DEFAULT '[]',
			equipped_emotes TEXT NOT NULL DEFAULT '[]',
			equipped_badge INTEGER NOT NULL DEFAULT 0,
			equipped_sprays TEXT NOT NULL DEFAULT '[]',
			owned_cosmetics TEXT NOT NULL DEFAULT '[]',
			owned_emotes TEXT NOT NULL DEFAULT '[]',
			owned_badges TEXT NOT NULL DEFAULT '[]',
			owned_sprays TEXT NOT NULL DEFAULT '[]',
			vis_cloth_cloak INTEGER NOT NULL DEFAULT 0,
			vis_hats_over_helmet INTEGER NOT NULL DEFAULT 1,
			vis_hats_over_skin INTEGER NOT NULL DEFAULT 1,
			vis_over_chestplate INTEGER NOT NULL DEFAULT 1,
			vis_over_leggings INTEGER NOT NULL DEFAULT 1,
			vis_over_boots INTEGER NOT NULL DEFAULT 1,
			vis_flip_shoulder INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL DEFAULT 0,
			last_seen INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_users_username ON users(username)`,
		`CREATE TABLE IF NOT EXISTS user_settings (uuid TEXT NOT NULL, scope TEXT NOT NULL, setting_id TEXT NOT NULL, value BLOB NOT NULL, updated_at INTEGER NOT NULL, PRIMARY KEY(uuid, scope, setting_id))`,
		`CREATE TABLE IF NOT EXISTS friendships (owner_uuid TEXT NOT NULL, friend_uuid TEXT NOT NULL, state TEXT NOT NULL, pinned INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL, PRIMARY KEY(owner_uuid, friend_uuid))`,
		`CREATE TABLE IF NOT EXISTS parties (party_id TEXT PRIMARY KEY, owner_uuid TEXT NOT NULL, name TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS party_members (party_id TEXT NOT NULL, member_uuid TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'MEMBER', joined_at INTEGER NOT NULL, PRIMARY KEY(party_id, member_uuid))`,
		`CREATE TABLE IF NOT EXISTS direct_messages (id TEXT PRIMARY KEY, sender_uuid TEXT NOT NULL, recipient_uuid TEXT NOT NULL, contents BLOB NOT NULL, sent_at INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_direct_messages_pair ON direct_messages(sender_uuid, recipient_uuid, sent_at)`,
		`CREATE TABLE IF NOT EXISTS cosmetic_outfits (owner_uuid TEXT NOT NULL, outfit_uuid TEXT NOT NULL, outfit BLOB NOT NULL, updated_at INTEGER NOT NULL, PRIMARY KEY(owner_uuid, outfit_uuid))`,
		`CREATE TABLE IF NOT EXISTS cosmetic_outfit_trees (owner_uuid TEXT PRIMARY KEY, outfit_tree BLOB NOT NULL, updated_at INTEGER NOT NULL)`,
	}

	for _, q := range queries {
		if _, err := d.db.Exec(q); err != nil {
			return fmt.Errorf("migration: %w", err)
		}
	}
	return nil
}

func (d *Database) SaveDirectMessage(message *DirectMessage) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.db.Exec(`INSERT INTO direct_messages(id,sender_uuid,recipient_uuid,contents,sent_at) VALUES(?,?,?,?,?)`, message.ID, message.SenderUUID, message.RecipientUUID, message.Contents, message.SentAt)
	return err
}

func (d *Database) DirectMessages(a, b string, limit int) ([]DirectMessage, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	rows, err := d.db.Query(`SELECT id,sender_uuid,recipient_uuid,contents,sent_at FROM (SELECT id,sender_uuid,recipient_uuid,contents,sent_at FROM direct_messages WHERE (sender_uuid=? AND recipient_uuid=?) OR (sender_uuid=? AND recipient_uuid=?) ORDER BY sent_at DESC LIMIT ?) ORDER BY sent_at`, a, b, b, a, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var messages []DirectMessage
	for rows.Next() {
		var message DirectMessage
		if err := rows.Scan(&message.ID, &message.SenderUUID, &message.RecipientUUID, &message.Contents, &message.SentAt); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

func (d *Database) PutSetting(uuid, scope, id string, value []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.db.Exec(`INSERT INTO user_settings(uuid,scope,setting_id,value,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(uuid,scope,setting_id) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, uuid, scope, id, value, time.Now().Unix())
	return err
}

func (d *Database) SetFriendState(owner, friend, state string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if state == "" {
		_, err := d.db.Exec(`DELETE FROM friendships WHERE owner_uuid=? AND friend_uuid=?`, owner, friend)
		return err
	}
	_, err := d.db.Exec(`INSERT INTO friendships(owner_uuid,friend_uuid,state,created_at) VALUES(?,?,?,?) ON CONFLICT(owner_uuid,friend_uuid) DO UPDATE SET state=excluded.state`, owner, friend, state, time.Now().Unix())
	return err
}

// SetFriendship changes both sides of a relationship in one durable commit.
func (d *Database) SetFriendship(a, b, aState, bState string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	set := func(owner, friend, state string) error {
		if state == "" {
			_, err := tx.Exec(`DELETE FROM friendships WHERE owner_uuid=? AND friend_uuid=?`, owner, friend)
			return err
		}
		_, err := tx.Exec(`INSERT INTO friendships(owner_uuid,friend_uuid,state,created_at) VALUES(?,?,?,?) ON CONFLICT(owner_uuid,friend_uuid) DO UPDATE SET state=excluded.state`, owner, friend, state, time.Now().Unix())
		return err
	}
	if err := set(a, b, aState); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := set(b, a, bState); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (d *Database) SaveCosmeticOutfit(owner, outfitID string, outfit []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.db.Exec(`INSERT INTO cosmetic_outfits(owner_uuid,outfit_uuid,outfit,updated_at) VALUES(?,?,?,?) ON CONFLICT(owner_uuid,outfit_uuid) DO UPDATE SET outfit=excluded.outfit,updated_at=excluded.updated_at`, owner, outfitID, outfit, time.Now().Unix())
	return err
}

func (d *Database) CosmeticOutfit(owner, outfitID string) ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var data []byte
	err := d.db.QueryRow(`SELECT outfit FROM cosmetic_outfits WHERE owner_uuid=? AND outfit_uuid=?`, owner, outfitID).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return data, err
}

func (d *Database) CosmeticOutfits(owner string) ([]CosmeticOutfit, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	rows, err := d.db.Query(`SELECT outfit_uuid,outfit,updated_at FROM cosmetic_outfits WHERE owner_uuid=? ORDER BY updated_at,outfit_uuid`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var outfits []CosmeticOutfit
	for rows.Next() {
		var outfit CosmeticOutfit
		if err := rows.Scan(&outfit.ID, &outfit.Data, &outfit.UpdatedAt); err != nil {
			return nil, err
		}
		outfits = append(outfits, outfit)
	}
	return outfits, rows.Err()
}

func (d *Database) DeleteCosmeticOutfit(owner, outfitID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.db.Exec(`DELETE FROM cosmetic_outfits WHERE owner_uuid=? AND outfit_uuid=?`, owner, outfitID)
	return err
}

func (d *Database) SaveCosmeticOutfitTree(owner string, tree []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.db.Exec(`INSERT INTO cosmetic_outfit_trees(owner_uuid,outfit_tree,updated_at) VALUES(?,?,?) ON CONFLICT(owner_uuid) DO UPDATE SET outfit_tree=excluded.outfit_tree,updated_at=excluded.updated_at`, owner, tree, time.Now().Unix())
	return err
}

func (d *Database) CosmeticOutfitTree(owner string) ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var data []byte
	err := d.db.QueryRow(`SELECT outfit_tree FROM cosmetic_outfit_trees WHERE owner_uuid=?`, owner).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return data, err
}

func (d *Database) FriendCount(uuid, state string) (int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var n int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM friendships WHERE owner_uuid=? AND state=?`, uuid, state).Scan(&n)
	return n, err
}

func (d *Database) SetFriendPinned(owner, friend string, pinned bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.db.Exec(`UPDATE friendships SET pinned=? WHERE owner_uuid=? AND friend_uuid=?`, pinned, owner, friend)
	return err
}

func (d *Database) FriendEntries(owner string) ([]FriendEntry, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	rows, err := d.db.Query(`SELECT friend_uuid,state,pinned,created_at FROM friendships WHERE owner_uuid=? ORDER BY created_at`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []FriendEntry
	for rows.Next() {
		var entry FriendEntry
		if err := rows.Scan(&entry.FriendUUID, &entry.State, &entry.Pinned, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (d *Database) GetProfileByUsername(username string) (*UserProfile, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return scanProfile(d.db.QueryRow(`SELECT uuid,username,rank,plus_color,logo_color,logo_always_show,artist_tools,tester_tools,equipped_cosmetics,equipped_emotes,equipped_badge,equipped_sprays,owned_cosmetics,owned_emotes,owned_badges,owned_sprays,vis_cloth_cloak,vis_hats_over_helmet,vis_hats_over_skin,vis_over_chestplate,vis_over_leggings,vis_over_boots,vis_flip_shoulder,created_at,last_seen FROM users WHERE username=? COLLATE NOCASE`, username))
}

func (d *Database) RekeyProfile(oldUUID, newUUID string) error {
	if oldUUID == "" || newUUID == "" || oldUUID == newUUID {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	rollback := func(cause error) error { _ = tx.Rollback(); return cause }
	statements := []struct {
		query string
		args  []any
	}{
		{`UPDATE users SET uuid=? WHERE uuid=?`, []any{newUUID, oldUUID}},
		{`UPDATE friendships SET owner_uuid=? WHERE owner_uuid=?`, []any{newUUID, oldUUID}},
		{`UPDATE friendships SET friend_uuid=? WHERE friend_uuid=?`, []any{newUUID, oldUUID}},
		{`UPDATE direct_messages SET sender_uuid=? WHERE sender_uuid=?`, []any{newUUID, oldUUID}},
		{`UPDATE direct_messages SET recipient_uuid=? WHERE recipient_uuid=?`, []any{newUUID, oldUUID}},
		{`UPDATE parties SET owner_uuid=? WHERE owner_uuid=?`, []any{newUUID, oldUUID}},
		{`UPDATE party_members SET member_uuid=? WHERE member_uuid=?`, []any{newUUID, oldUUID}},
		{`UPDATE user_settings SET uuid=? WHERE uuid=?`, []any{newUUID, oldUUID}},
		{`UPDATE cosmetic_outfits SET owner_uuid=? WHERE owner_uuid=?`, []any{newUUID, oldUUID}},
		{`UPDATE cosmetic_outfit_trees SET owner_uuid=? WHERE owner_uuid=?`, []any{newUUID, oldUUID}},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement.query, statement.args...); err != nil {
			return rollback(err)
		}
	}
	return tx.Commit()
}

func (d *Database) GetProfile(uuid string) (*UserProfile, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	row := d.db.QueryRow(`SELECT uuid, username, rank, plus_color, logo_color, logo_always_show,
		artist_tools, tester_tools, equipped_cosmetics, equipped_emotes, equipped_badge, equipped_sprays,
		owned_cosmetics, owned_emotes, owned_badges, owned_sprays,
		vis_cloth_cloak, vis_hats_over_helmet, vis_hats_over_skin, vis_over_chestplate,
		vis_over_leggings, vis_over_boots, vis_flip_shoulder, created_at, last_seen
		FROM users WHERE uuid = ?`, uuid)

	return scanProfile(row)
}

func scanProfile(row *sql.Row) (*UserProfile, error) {
	p := &UserProfile{}
	var eqCos, eqEmote, eqSpray, ownCos, ownEmote, ownBadge, ownSpray string

	err := row.Scan(&p.UUID, &p.Username, &p.Rank, &p.PlusColor, &p.LogoColor,
		&p.LogoAlwaysShow, &p.ArtistTools, &p.TesterTools,
		&eqCos, &eqEmote, &p.EquippedBadge, &eqSpray,
		&ownCos, &ownEmote, &ownBadge, &ownSpray,
		&p.VisClothCloak, &p.VisHatsOverHelmet, &p.VisHatsOverSkin, &p.VisOverChestplate,
		&p.VisOverLeggings, &p.VisOverBoots, &p.VisFlipShoulder,
		&p.CreatedAt, &p.LastSeen)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	json.Unmarshal([]byte(eqCos), &p.EquippedCosmetics)
	json.Unmarshal([]byte(eqEmote), &p.EquippedEmotes)
	json.Unmarshal([]byte(eqSpray), &p.EquippedSprays)
	json.Unmarshal([]byte(ownCos), &p.OwnedCosmetics)
	json.Unmarshal([]byte(ownEmote), &p.OwnedEmotes)
	json.Unmarshal([]byte(ownBadge), &p.OwnedBadges)
	json.Unmarshal([]byte(ownSpray), &p.OwnedSprays)

	return p, nil
}

func (d *Database) UpsertProfile(p *UserProfile) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now().Unix()
	if p.CreatedAt == 0 {
		p.CreatedAt = now
	}
	p.LastSeen = now

	eqCos, _ := json.Marshal(p.EquippedCosmetics)
	eqEmote, _ := json.Marshal(p.EquippedEmotes)
	eqSpray, _ := json.Marshal(p.EquippedSprays)
	ownCos, _ := json.Marshal(p.OwnedCosmetics)
	ownEmote, _ := json.Marshal(p.OwnedEmotes)
	ownBadge, _ := json.Marshal(p.OwnedBadges)
	ownSpray, _ := json.Marshal(p.OwnedSprays)

	_, err := d.db.Exec(`INSERT INTO users (uuid, username, rank, plus_color, logo_color, logo_always_show,
		artist_tools, tester_tools, equipped_cosmetics, equipped_emotes, equipped_badge, equipped_sprays,
		owned_cosmetics, owned_emotes, owned_badges, owned_sprays,
		vis_cloth_cloak, vis_hats_over_helmet, vis_hats_over_skin, vis_over_chestplate,
		vis_over_leggings, vis_over_boots, vis_flip_shoulder, created_at, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(uuid) DO UPDATE SET
			username = excluded.username,
			rank = excluded.rank,
			plus_color = excluded.plus_color,
			logo_color = excluded.logo_color,
			logo_always_show = excluded.logo_always_show,
			artist_tools = excluded.artist_tools,
			tester_tools = excluded.tester_tools,
			equipped_cosmetics = excluded.equipped_cosmetics,
			equipped_emotes = excluded.equipped_emotes,
			equipped_badge = excluded.equipped_badge,
			equipped_sprays = excluded.equipped_sprays,
			owned_cosmetics = excluded.owned_cosmetics,
			owned_emotes = excluded.owned_emotes,
			owned_badges = excluded.owned_badges,
			owned_sprays = excluded.owned_sprays,
			vis_cloth_cloak = excluded.vis_cloth_cloak,
			vis_hats_over_helmet = excluded.vis_hats_over_helmet,
			vis_hats_over_skin = excluded.vis_hats_over_skin,
			vis_over_chestplate = excluded.vis_over_chestplate,
			vis_over_leggings = excluded.vis_over_leggings,
			vis_over_boots = excluded.vis_over_boots,
			vis_flip_shoulder = excluded.vis_flip_shoulder,
			last_seen = excluded.last_seen`,
		p.UUID, p.Username, p.Rank, p.PlusColor, p.LogoColor, p.LogoAlwaysShow,
		p.ArtistTools, p.TesterTools, string(eqCos), string(eqEmote), p.EquippedBadge,
		string(eqSpray), string(ownCos), string(ownEmote), string(ownBadge), string(ownSpray),
		p.VisClothCloak, p.VisHatsOverHelmet, p.VisHatsOverSkin, p.VisOverChestplate,
		p.VisOverLeggings, p.VisOverBoots, p.VisFlipShoulder, p.CreatedAt, p.LastSeen)

	return err
}

func (d *Database) AllProfiles() ([]*UserProfile, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`SELECT uuid, username, rank, plus_color, logo_color, logo_always_show,
		artist_tools, tester_tools, equipped_cosmetics, equipped_emotes, equipped_badge, equipped_sprays,
		owned_cosmetics, owned_emotes, owned_badges, owned_sprays,
		vis_cloth_cloak, vis_hats_over_helmet, vis_hats_over_skin, vis_over_chestplate,
		vis_over_leggings, vis_over_boots, vis_flip_shoulder, created_at, last_seen
		FROM users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	profiles := make([]*UserProfile, 0)
	for rows.Next() {
		p := &UserProfile{}
		var eqCos, eqEmote, eqSpray, ownCos, ownEmote, ownBadge, ownSpray string

		if err := rows.Scan(&p.UUID, &p.Username, &p.Rank, &p.PlusColor, &p.LogoColor,
			&p.LogoAlwaysShow, &p.ArtistTools, &p.TesterTools,
			&eqCos, &eqEmote, &p.EquippedBadge, &eqSpray,
			&ownCos, &ownEmote, &ownBadge, &ownSpray,
			&p.VisClothCloak, &p.VisHatsOverHelmet, &p.VisHatsOverSkin, &p.VisOverChestplate,
			&p.VisOverLeggings, &p.VisOverBoots, &p.VisFlipShoulder,
			&p.CreatedAt, &p.LastSeen); err != nil {
			return nil, err
		}

		json.Unmarshal([]byte(eqCos), &p.EquippedCosmetics)
		json.Unmarshal([]byte(eqEmote), &p.EquippedEmotes)
		json.Unmarshal([]byte(eqSpray), &p.EquippedSprays)
		json.Unmarshal([]byte(ownCos), &p.OwnedCosmetics)
		json.Unmarshal([]byte(ownEmote), &p.OwnedEmotes)
		json.Unmarshal([]byte(ownBadge), &p.OwnedBadges)
		json.Unmarshal([]byte(ownSpray), &p.OwnedSprays)

		profiles = append(profiles, p)
	}
	return profiles, nil
}
