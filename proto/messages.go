package proto

// Per-player cosmetics state (maintained from Login responses)
type PlayerCosmetics struct {
	Cosmetics []int32
	Emotes    []int32
	Badges    []int32
	Sprays    []int32
	// Equipment (equipped IDs)
	EquippedCosmetics []int32
	EquippedEmotes    []int32
	EquippedBadge     int32
	EquippedSprays    []int32
	// Player stuff
	RankName    string
	PlusColor   uint32
	LogoColor   uint32
	LogoShow    bool
	ArtistTools bool
	TesterTools bool
	// Visibility
	VisClothCloak     bool
	VisHatsOverHelmet bool
	VisHatsOverSkin   bool
	VisOverChestplate bool
	VisOverLeggings   bool
	VisOverBoots      bool
	VisFlipShoulder   bool
}

func DefaultPlayerCosmetics() *PlayerCosmetics {
	return &PlayerCosmetics{
		Cosmetics:         allCosmeticIDs(),
		Emotes:            allEmoteIDs(),
		Badges:            allBadgeIDs(),
		Sprays:            allSprayIDs(),
		EquippedCosmetics: []int32{},
		EquippedEmotes:    []int32{},
		EquippedBadge:     0,
		EquippedSprays:    []int32{},
		RankName:          "ADMIN",
		PlusColor:         0x00FFAA00,
		LogoColor:         0xFFAA0000,
		LogoShow:          true,
		ArtistTools:       true,
		TesterTools:       true,
		VisClothCloak:     false,
		VisHatsOverHelmet: true,
		VisHatsOverSkin:   true,
		VisOverChestplate: true,
		VisOverLeggings:   true,
		VisOverBoots:      true,
		VisFlipShoulder:   false,
	}
}

func allCosmeticIDs() []int32 {
	ids := make([]int32, 0, 500)
	for i := int32(1); i <= 500; i++ {
		ids = append(ids, i)
	}
	return ids
}

func allEmoteIDs() []int32 {
	ids := make([]int32, 0, 200)
	for i := int32(1); i <= 200; i++ {
		ids = append(ids, i)
	}
	return ids
}

func allBadgeIDs() []int32 {
	ids := make([]int32, 0, 100)
	for i := int32(1); i <= 100; i++ {
		ids = append(ids, i)
	}
	return ids
}

func allSprayIDs() []int32 {
	ids := make([]int32, 0, 100)
	for i := int32(1); i <= 100; i++ {
		ids = append(ids, i)
	}
	return ids
}

// EncodeCosmeticLoginResponse builds the LoginResponse protobuf for cosmetic v1
func EncodeCosmeticLoginResponse(p *PlayerCosmetics) []byte {
	e := NewEncoder()

	// Settings message (field 1)
	e.SubMessage(1, func(enc *NestedEncoder) {
		enc.Bool(1, p.VisClothCloak)
		enc.Bool(2, p.VisHatsOverHelmet)
		enc.Bool(3, p.VisHatsOverSkin)
		enc.Bool(4, p.VisOverChestplate)
		enc.Bool(5, p.VisOverLeggings)
		enc.Bool(6, p.VisOverBoots)
		enc.Bool(7, p.VisFlipShoulder)
	})

	// Omitted: has_all_cosmetics_flag unlocks the local catalog safely.

	// available_lunar_plus_colors (field 3, repeated Color)
	plusColors := []uint32{0x00FFAA00, 0x00FFD700, 0x00FF6B6B, 0x0000FFAA, 0x00AA00FF, 0x00FF00FF}
	for _, c := range plusColors {
		EncodeColor(3, c, e)
	}

	// logo_color (field 4)
	EncodeColor(4, p.LogoColor, e)

	// logo_always_show (field 5)
	e.Bool(5, p.LogoShow)

	// lunar_plus_free_cosmetic_ids (field 6)
	// Omitted: fabricated free IDs can reference entries absent from the catalog.

	// has_all_cosmetics_flag (field 8)
	e.Bool(8, true)

	// rank_name (field 9)
	e.String(9, p.RankName)

	// artist_tools (field 10)
	e.Bool(10, p.ArtistTools)

	// cosmetic_ownership_visibility (field 11, enum: 0=unspecified, 1=everyone, 2=friends, 3=noone)
	e.Enum(11, 1)

	// tester_tools (field 12)
	e.Bool(12, p.TesterTools)

	return e.Bytes()
}

const DefaultCosmeticOutfitID = "00000000-0000-0000-0000-000000000001"

func EncodeCosmeticOutfit(id, name string, cosmetics []int32, badge int32) []byte {
	outfit := NewEncoder()
	EncodeUUID(1, id, outfit)
	outfit.String(2, name)
	for _, cid := range cosmetics {
		outfit.Message(3, EncodeEquippedCosmetic(cid))
	}
	EncodeTimestamp(5, 1717000000, 0, outfit)
	EncodeTimestamp(6, 1717000000, 0, outfit)
	if badge != 0 {
		outfit.Int32(8, badge)
	}
	return outfit.Bytes()
}

func EncodeOutfitTree(defaultOutfitID string) []byte {
	tree := NewEncoder()
	EncodeUUID(2, defaultOutfitID, tree)
	return tree.Bytes()
}

// EncodeCosmeticV2LoginResponse builds the LoginResponse for cosmetic v2.
// Legacy profiles are represented by one deterministic default outfit.
func EncodeCosmeticV2LoginResponse(p *PlayerCosmetics) []byte {
	return EncodeCosmeticV2LoginResponseWithOutfits(p, nil, nil)
}

// EncodeCosmeticV2LoginResponseWithOutfits restores Lunar's complete outfit
// list and selection tree, instead of only restoring the flattened equipment.
func EncodeCosmeticV2LoginResponseWithOutfits(p *PlayerCosmetics, outfits [][]byte, tree []byte) []byte {
	e := NewEncoder()
	EncodeColor(1, p.LogoColor, e)
	e.Bool(2, p.LogoShow)
	EncodeColor(3, p.PlusColor, e)
	for _, c := range []uint32{0x00FFAA00, 0x00FFD700, 0x00FF6B6B, 0x0000FFAA, 0x00AA00FF, 0x00FF00FF} {
		EncodeColor(4, c, e)
	}
	e.Bool(7, true)
	e.String(8, p.RankName)
	if len(outfits) == 0 {
		outfits = [][]byte{EncodeCosmeticOutfit(DefaultCosmeticOutfitID, "Default Outfit", p.EquippedCosmetics, p.EquippedBadge)}
	}
	for _, outfit := range outfits {
		e.Message(10, outfit)
	}
	if len(tree) == 0 {
		tree = EncodeOutfitTree(DefaultCosmeticOutfitID)
	}
	e.Message(11, tree)
	e.Bool(12, p.ArtistTools)
	e.Enum(13, 1)
	e.Bool(14, p.TesterTools)
	return e.Bytes()
	/* legacy incorrect v1-shaped encoder retained below as unreachable history

	// v2 has superset of v1 fields plus: plus_color, outfits, outfit_tree, favorite_cosmetic_ids

	// Settings
	e.SubMessage(1, func(enc *NestedEncoder) {
		enc.Bool(1, p.VisClothCloak)
		enc.Bool(2, p.VisHatsOverHelmet)
		enc.Bool(3, p.VisHatsOverSkin)
		enc.Bool(4, p.VisOverChestplate)
		enc.Bool(5, p.VisOverLeggings)
		enc.Bool(6, p.VisOverBoots)
		enc.Bool(7, p.VisFlipShoulder)
	})

	e.RepeatedInt32(2, p.Cosmetics)

	plusColorsV2 := []uint32{0x00FFAA00, 0x00FFD700, 0x00FF6B6B, 0x0000FFAA, 0x00AA00FF, 0x00FF00FF}
	for _, c := range plusColorsV2 {
		EncodeColor(3, c, e)
	}

	EncodeColor(4, p.LogoColor, e)
	e.Bool(5, p.LogoShow)
	e.RepeatedInt32(6, p.Cosmetics)

	for _, cid := range p.Cosmetics {
		e.Message(7, EncodeOwnedCosmetic(cid))
	}

	e.Bool(8, true)
	e.String(9, p.RankName)
	e.Bool(10, p.ArtistTools)
	e.Enum(11, 1)
	e.Bool(12, p.TesterTools)

	// plus_color (field 13? or higher — v2 extends v1)
	EncodeColor(13, p.PlusColor, e)

	return e.Bytes() */
}

// EncodeEmoteLoginResponse builds the LoginResponse for emote v1
func EncodeEmoteLoginResponse(p *PlayerCosmetics) []byte {
	e := NewEncoder()

	// equipped_emote_ids (field 2, repeated int32)
	e.RepeatedInt32(2, p.EquippedEmotes)

	// lunar_plus_free_emote_id (field 3, int32)
	e.Int32(3, 0)

	// has_all_emotes_flag (field 5, bool)
	e.Bool(5, true)

	// equipped_emotes (field 8, repeated EquippedEmote)
	for _, eid := range p.EquippedEmotes {
		e.Message(8, EncodeEquippedCosmetic(eid))
	}

	// lunar_plus_free_emote_ids (field 9, repeated int32)
	// Omitted: has_all_emotes_flag is sufficient.

	// Also send field 4 (lunar_plus_free_emote_id int32) if needed
	e.Int32(4, 0)

	return e.Bytes()
}

// EncodeBadgeLoginResponse builds the LoginResponse for badge v1
func EncodeBadgeLoginResponse(p *PlayerCosmetics) []byte {
	e := NewEncoder()

	// equipped_badge_id (field 2, int32)
	e.Int32(2, p.EquippedBadge)

	// has_all_badges_flag (field 3, bool)
	e.Bool(3, true)

	return e.Bytes()
}

// EncodeSprayLoginResponse builds the LoginResponse for spray v1
func EncodeSprayLoginResponse(p *PlayerCosmetics) []byte {
	e := NewEncoder()

	// equipped_sprays (field 2, repeated EquippedSpray)
	for _, sid := range p.EquippedSprays {
		e.Message(2, EncodeEquippedCosmetic(sid))
	}

	// lunar_plus_free_spray_id (field 3, int32)
	e.Int32(3, 0)

	// has_all_sprays_flag (field 4, bool)
	e.Bool(4, true)

	// max_active_sprays (field 5, int32)
	e.Int32(5, 16)

	// lunar_plus_free_spray_ids (field 6, repeated int32)
	// Omitted: has_all_sprays_flag is sufficient.

	return e.Bytes()
}

// EncodeUseEmoteResponse builds UseEmoteResponse
func EncodeUseEmoteResponse(emoteID, metadata int32) []byte {
	e := NewEncoder()
	e.Enum(1, 1)
	e.Int32(2, emoteID)
	e.Int32(3, metadata)
	return e.Bytes()
}

// EncodeUseSprayResponse builds UseSprayResponse
func EncodeUseSprayResponse(sprayID int32) []byte {
	e := NewEncoder()
	e.Enum(1, 1)
	e.Int32(2, sprayID)
	return e.Bytes()
}

// EncodeStopEmoteResponse builds StopEmoteResponse
func EncodeStopEmoteResponse() []byte {
	return NewEncoder().Bytes()
}

// EncodeRemoveSprayResponse builds RemoveSprayResponse
func EncodeRemoveSprayResponse(sprayID int32) []byte {
	e := NewEncoder()
	e.Int32(1, sprayID)
	return e.Bytes()
}

// EncodeEmptyResponse returns empty bytes for RPCs with no response data
func EncodeEmptyResponse() []byte {
	return NewEncoder().Bytes()
}

// EncodeEquipBadgeResponse builds EquipBadgeResponse
func EncodeEquipBadgeResponse(badgeID int32) []byte {
	e := NewEncoder()
	e.Int32(1, badgeID)
	return e.Bytes()
}

// EncodeUpdateEquippedEmotesResponse builds response for UpdateEquippedEmotes
func EncodeUpdateEquippedEmotesResponse() []byte {
	return NewEncoder().Bytes()
}

// EncodeUpdateEquippedSpraysResponse builds response for UpdateEquippedSprays
func EncodeUpdateEquippedSpraysResponse() []byte {
	return NewEncoder().Bytes()
}

// --- Push messages (server → client, broadcast) ---

// EncodePlayerCosmeticsPushV2 builds a PlayerCosmeticsPushV2 message
func EncodePlayerCosmeticsPushV2(uuidStr string, p *PlayerCosmetics) []byte {
	e := NewEncoder()
	EncodeUUID(1, uuidStr, e)
	EncodeColor(2, p.LogoColor, e)
	e.Bool(3, p.LogoShow)
	EncodeColor(4, p.PlusColor, e)
	for _, cid := range p.EquippedCosmetics {
		e.Message(6, EncodeEquippedCosmetic(cid))
	}
	e.Int32(7, p.EquippedBadge)
	EncodeUUID(8, "00000000-0000-0000-0000-000000000001", e)
	return e.Bytes()
}

// EncodePlayerCosmeticsPush builds the legacy cosmetic.v1 subscription item.
func EncodePlayerCosmeticsPush(uuidStr string, p *PlayerCosmetics) []byte {
	settings := NewEncoder()
	settings.PackedInt32(1, p.EquippedCosmetics)
	settings.Bool(2, p.VisClothCloak)
	EncodeColor(3, p.PlusColor, settings)
	settings.Bool(4, p.VisHatsOverHelmet)
	settings.Bool(5, p.VisHatsOverSkin)
	settings.Bool(6, p.VisOverChestplate)
	settings.Bool(7, p.VisOverLeggings)
	settings.Bool(8, p.VisOverBoots)
	settings.Bool(10, p.VisFlipShoulder)
	for _, cid := range p.EquippedCosmetics {
		settings.Message(11, EncodeEquippedCosmetic(cid))
	}

	e := NewEncoder()
	EncodeUUID(1, uuidStr, e)
	e.Message(2, settings.Bytes())
	EncodeColor(3, p.LogoColor, e)
	e.Bool(4, p.LogoShow)
	e.Int32(5, p.EquippedBadge)
	EncodeUUID(6, "00000000-0000-0000-0000-000000000001", e)
	return e.Bytes()
}

func EncodeUUIDAndUsername(uuidStr, username string) []byte {
	e := NewEncoder()
	EncodeUUID(1, uuidStr, e)
	e.String(2, username)
	return e.Bytes()
}

func EncodeSendFriendRequestResponse(status int32, uuidStr, username string, logoColor, plusColor uint32, rank string, badgeID int32) []byte {
	e := NewEncoder()
	e.Enum(1, status)
	if uuidStr != "" {
		e.Message(2, EncodeUUIDAndUsername(uuidStr, username))
		EncodeColor(3, logoColor, e)
		EncodeColor(4, plusColor, e)
		e.String(5, rank)
		e.Int32(6, badgeID)
	}
	return e.Bytes()
}

func EncodeFriendRequestReceivedPush(uuidStr, username string, logoColor, plusColor uint32, rank string, badgeID int32) []byte {
	e := NewEncoder()
	e.Message(1, EncodeUUIDAndUsername(uuidStr, username))
	EncodeColor(3, logoColor, e)
	EncodeColor(4, plusColor, e)
	e.String(5, rank)
	e.Int32(6, badgeID)
	return e.Bytes()
}

func EncodeFriendRequestAcceptedPush(uuidStr, username string) []byte {
	e := NewEncoder()
	EncodeUUID(1, uuidStr, e)
	e.Message(2, EncodeUUIDAndUsername(uuidStr, username))
	return e.Bytes()
}

func EncodeFriendRequest(uuidStr, username string, sentAt int64, logoColor, plusColor uint32, rank string, badgeID int32) []byte {
	e := NewEncoder()
	e.Message(1, EncodeUUIDAndUsername(uuidStr, username))
	EncodeTimestamp(2, sentAt, 0, e)
	EncodeColor(3, logoColor, e)
	EncodeColor(4, plusColor, e)
	e.String(5, rank)
	e.Int32(6, badgeID)
	return e.Bytes()
}

func EncodeOnlineFriend(uuidStr, username string, status int32, justCameOnline bool, logoColor, plusColor uint32, rank string, badgeID int32, friendsSince int64) []byte {
	e := NewEncoder()
	e.Message(1, EncodeUUIDAndUsername(uuidStr, username))
	e.Enum(2, status)
	e.Bool(5, justCameOnline)
	EncodeColor(7, logoColor, e)
	EncodeColor(9, plusColor, e)
	EncodeTimestamp(11, friendsSince, 0, e)
	e.String(13, rank)
	e.Int32(15, badgeID)
	return e.Bytes()
}

func EncodeOfflineFriend(uuidStr, username string, lastSeen, friendsSince int64, logoColor, plusColor uint32, rank string, badgeID int32) []byte {
	e := NewEncoder()
	e.Message(1, EncodeUUIDAndUsername(uuidStr, username))
	EncodeTimestamp(2, lastSeen, 0, e)
	EncodeColor(3, logoColor, e)
	EncodeColor(4, plusColor, e)
	EncodeTimestamp(6, friendsSince, 0, e)
	e.String(8, rank)
	e.Int32(10, badgeID)
	return e.Bytes()
}

func EncodeAcceptFriendRequestResponse(status int32, offlineFriend []byte) []byte {
	e := NewEncoder()
	e.Enum(1, status)
	if len(offlineFriend) != 0 {
		e.Message(2, offlineFriend)
	}
	return e.Bytes()
}

type TabLogo struct {
	UUID      string
	LogoColor uint32
	PlusColor uint32
	BadgeID   int32
}

func EncodeLoadTabLogosResponse(logos []TabLogo) []byte {
	e := NewEncoder()
	for _, logo := range logos {
		item := NewEncoder()
		EncodeUUID(1, logo.UUID, item)
		EncodeColor(2, logo.LogoColor, item)
		EncodeColor(3, logo.PlusColor, item)
		item.Int32(4, logo.BadgeID)
		e.Message(1, item.Bytes())
	}
	return e.Bytes()
}

func EncodeOutfitMutationResponse(outfit []byte) []byte {
	e := NewEncoder()
	e.Enum(1, 1)
	if len(outfit) != 0 {
		e.Message(2, outfit)
	}
	return e.Bytes()
}

// EncodeHostedWorldLoginResponse disables unsupported hosting while keeping
// Lunar's multiplayer refresh loop on a safe interval. Omitting field 5 makes
// the client sleep for 0ms and issue unbounded ListHostedWorlds RPCs.
func EncodeHostedWorldLoginResponse(refreshSeconds int32) []byte {
	if refreshSeconds < 5 {
		refreshSeconds = 30
	}
	e := NewEncoder()
	e.Bool(1, false)
	e.Bool(2, false)
	e.Int32(3, 12)
	e.Int32(5, refreshSeconds)
	return e.Bytes()
}

func EncodeConversationReference(friendUUID string) []byte {
	e := NewEncoder()
	// Lunar's conversation UI only accepts the conversation_reference oneof
	// (field 2). friend_uuid (field 1) is legacy and is rejected before send.
	EncodeUUID(2, friendUUID, e)
	return e.Bytes()
}

func EncodeConversationMessage(id string, sentAt int64, senderUUID, senderName string, contents []byte) []byte {
	e := NewEncoder()
	EncodeUUID(1, id, e)
	EncodeTimestamp(2, sentAt, 0, e)
	sender := NewEncoder()
	sender.Message(1, EncodeUUIDAndUsername(senderUUID, senderName))
	e.Message(3, sender.Bytes())
	e.Message(4, contents)
	e.Enum(5, 1)
	return e.Bytes()
}

func EncodeConversationMessagePush(friendUUID string, message []byte) []byte {
	e := NewEncoder()
	e.Message(1, EncodeConversationReference(friendUUID))
	e.Message(2, message)
	return e.Bytes()
}

func EncodeConversationStub(friendUUID string) []byte {
	e := NewEncoder()
	e.Message(1, EncodeConversationReference(friendUUID))
	return e.Bytes()
}

func EncodeConversationParticipant(uuidStr, username string, logoColor, plusColor uint32, rank string, badgeID int32) []byte {
	e := NewEncoder()
	e.Message(1, EncodeUUIDAndUsername(uuidStr, username))
	EncodeColor(2, logoColor, e)
	EncodeColor(3, plusColor, e)
	e.String(4, rank)
	e.Int32(6, badgeID)
	return e.Bytes()
}

// EncodeUseEmotePush builds UseEmotePush for broadcasting
func EncodeUseEmotePush(uuidStr string, emoteID, metadata int32, soundtrackURL string, jamID int32) []byte {
	e := NewEncoder()

	// player_uuid (field 1)
	EncodeUUID(1, uuidStr, e)

	// emote_id (field 2)
	e.Int32(2, emoteID)
	e.Int32(3, metadata)
	e.String(4, soundtrackURL)
	e.Int32(5, jamID)

	return e.Bytes()
}

// EncodeStopEmotePush builds StopEmotePush
func EncodeStopEmotePush(uuidStr string) []byte {
	e := NewEncoder()
	EncodeUUID(1, uuidStr, e)
	return e.Bytes()
}

// EncodeUseSprayPush builds UseSprayPush
func EncodeUseSprayPush(uuidStr string, sprayID int32) []byte {
	e := NewEncoder()
	EncodeUUID(1, uuidStr, e)
	e.Int32(2, sprayID)
	return e.Bytes()
}

// EncodeRemoveSprayPush builds RemoveSprayPush
func EncodeRemoveSprayPush(uuidStr string, sprayID int32) []byte {
	e := NewEncoder()
	EncodeUUID(1, uuidStr, e)
	e.Int32(2, sprayID)
	return e.Bytes()
}

// --- Framing messages ---

// ServerboundWebSocketMessage is the main protocol wrapper from client
type ServerboundMessage struct {
	RequestID []byte
	Service   string
	Method    string
	Input     []byte
}

// ParseServerboundMessage decodes the main protocol message from client
func ParseServerboundMessage(data []byte) (*ServerboundMessage, error) {
	d := NewDecoder(data)
	msg := &ServerboundMessage{}

	for d.Remaining() > 0 {
		fn, wt, err := d.ReadTag()
		if err != nil {
			return nil, err
		}
		switch fn {
		case 1: // request_id (bytes)
			if wt == LengthDelimited {
				msg.RequestID, err = d.ReadBytes()
				if err != nil {
					return nil, err
				}
			} else {
				d.Skip(wt)
			}
		case 2: // service (string)
			msg.Service, err = d.ReadString()
			if err != nil {
				return nil, err
			}
		case 3: // method (string)
			msg.Method, err = d.ReadString()
			if err != nil {
				return nil, err
			}
		case 4: // input (bytes)
			msg.Input, err = d.ReadBytes()
			if err != nil {
				return nil, err
			}
		default:
			d.Skip(wt)
		}
	}
	return msg, nil
}

// EncodeClientboundMessage builds the main protocol response wrapper
func EncodeClientboundMessage(requestID []byte, output []byte) []byte {
	e := NewEncoder()

	// rpc_response (field 1, WebSocketRpcResponse)
	e.SubMessage(1, func(enc *NestedEncoder) {
		enc.WriteBytes(1, requestID)
		enc.WriteBytes(2, output)
	})

	return e.Bytes()
}

// EncodePush wraps a push notification in Any and sends as push
func EncodePush(typeURL string, pushData []byte) []byte {
	e := NewEncoder()

	// push_notification (field 2, google.protobuf.Any)
	e.SubMessage(2, func(enc *NestedEncoder) {
		enc.String(1, typeURL)
		enc.WriteBytes(2, pushData)
	})

	return e.Bytes()
}

// --- Authenticator messages ---

// HelloMessage wraps the hello from client
type HelloMessage struct {
	// Usually empty or has player info
	Raw []byte
}

// AuthServerboundMessage: client→server in auth phase
type AuthServerboundMessage struct {
	Hello              *HelloMessage
	EncryptionFail     bool
	EncryptionResponse []byte
}

// ParseAuthServerboundMessage decodes the authenticator wrapper
func ParseAuthServerboundMessage(data []byte) (*AuthServerboundMessage, error) {
	d := NewDecoder(data)
	msg := &AuthServerboundMessage{}

	for d.Remaining() > 0 {
		fn, wt, err := d.ReadTag()
		if err != nil {
			return nil, err
		}
		switch fn {
		case 1: // hello
			raw, err := d.ReadMessage()
			if err != nil {
				return nil, err
			}
			msg.Hello = &HelloMessage{Raw: raw}
		case 2: // encryption_response
			raw, err := d.ReadMessage()
			if err != nil {
				return nil, err
			}
			msg.EncryptionResponse = raw
		case 3: // encryption_fail
			msg.EncryptionFail = true
			d.Skip(wt)
		default:
			d.Skip(wt)
		}
	}
	return msg, nil
}

// EncodeAuthSuccess builds the AuthSuccessMessage + wrapper
func EncodeAuthSuccess(jwt string) []byte {
	e := NewEncoder()
	// auth_success (field 2)
	e.SubMessage(2, func(enc *NestedEncoder) {
		enc.String(1, jwt)
	})
	return e.Bytes()
}

// EncodeEncryptionRequest builds the EncryptionRequest message (won't be needed since we bypass)
func EncodeEncryptionRequest(publicKey []byte) []byte {
	e := NewEncoder()
	e.SubMessage(1, func(enc *NestedEncoder) {
		enc.WriteBytes(1, publicKey)
	})
	return e.Bytes()
}
