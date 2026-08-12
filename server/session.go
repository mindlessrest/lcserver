package server

import (
	"encoding/json"
	"fmt"
	"lcserver/db"
	"lcserver/proto"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Session struct {
	mu                sync.Mutex
	conn              *websocket.Conn
	uuid              string
	username          string
	authed            bool
	authMode          bool
	handshakeComplete bool
	hub               *Hub
	db                *db.Database
	profile           *db.UserProfile
	// Runtime state, overrides DB profile
	cosmetics *proto.PlayerCosmetics
	writeCh   chan []byte
	closeCh   chan struct{}
}

func newSession(conn *websocket.Conn, hub *Hub, database *db.Database, authMode bool) *Session {
	return &Session{
		conn:     conn,
		hub:      hub,
		db:       database,
		authMode: authMode,
		writeCh:  make(chan []byte, 512),
		closeCh:  make(chan struct{}),
	}
}

func (s *Session) Run() {
	defer s.cleanup()

	go s.writeLoop()

	for {
		messageType, data, err := s.conn.ReadMessage()
		if err != nil {
			return
		}

		if messageType != websocket.BinaryMessage {
			log.Printf("WARN protocol: expected binary frame, got type=%d", messageType)
			s.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseUnsupportedData, "binary protobuf required"), time.Now().Add(time.Second))
			return
		}
		s.mu.Lock()
		if s.authMode {
			s.handleAuth(data)
		} else if !s.handshakeComplete {
			s.handleGameHandshake(data)
		} else {
			s.handleProtocolMessage(data)
		}
		s.mu.Unlock()
	}
}

func (s *Session) writeLoop() {
	defer s.conn.Close()
	for {
		select {
		case msg, ok := <-s.writeCh:
			if !ok {
				return
			}
			// Write raw binary to WebSocket
			_ = s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			w, err := s.conn.NextWriter(websocket.BinaryMessage)
			if err != nil {
				return
			}
			if _, err := w.Write(msg); err != nil {
				w.Close()
				return
			}
			if err := w.Close(); err != nil {
				return
			}
		case <-s.closeCh:
			return
		}
	}
}

func (s *Session) cleanup() {
	if s.profile != nil && s.handshakeComplete && s.hub.isCurrent(s) {
		s.broadcastFriendStatus(0, false)
	}
	s.hub.unregisterSession(s)
	select {
	case <-s.closeCh:
	default:
		close(s.closeCh)
	}
}

func (s *Session) sendRaw(data []byte) {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case s.writeCh <- data:
	case <-s.closeCh:
	case <-timer.C:
		log.Printf("WARN websocket writer stalled user=%s uuid=%s; closing connection", s.username, s.uuid)
		_ = s.conn.Close()
	}
}

// handleAuth processes authenticator protocol messages (first message after connect)
// Returns true if auth was handled, false if the message wasn't an auth message
func (s *Session) handleAuth(data []byte) bool {
	authMsg, err := proto.ParseAuthServerboundMessage(data)
	if err != nil {
		log.Printf("ERROR auth parse: %v", err)
		return false
	}

	if authMsg.Hello != nil {
		s.extractAuthIdentity(authMsg.Hello.Raw)

		// Load/create profile
		profile, err := s.db.GetProfile(s.uuid)
		if err != nil {
			log.Printf("ERROR db profile load [%s]: %v", s.uuid, err)
		}
		if profile == nil {
			profile = s.migrateLegacyProfile()
		}
		if profile == nil {
			profile = s.defaultProfile()
			s.db.UpsertProfile(profile)
		}
		s.profile = profile

		// Build runtime cosmetics state from profile
		s.cosmetics = profileToCosmetics(profile)

		// Send AuthSuccess
		s.sendRaw(proto.EncodeAuthSuccess("lcserver:" + s.uuid))
		s.authed = true

		log.Printf("AUTH user=%s uuid=%s rank=%s", s.username, s.uuid, profile.Rank)
		return true
	}

	if authMsg.EncryptionFail {
		log.Printf("WARN auth encryption failed")
		s.conn.Close()
		return true
	}

	// Not a recognized auth message
	return false
}

// extractIdentity tries to get UUID and username from the raw Hello message bytes
func (s *Session) extractAuthIdentity(helloData []byte) {
	d := proto.NewDecoder(helloData)
	for d.Remaining() > 0 {
		fn, wt, err := d.ReadTag()
		if err != nil {
			break
		}
		if fn == 1 && wt == proto.LengthDelimited {
			identity, _ := d.ReadMessage()
			s.extractUUIDAndUsername(identity)
		} else {
			d.Skip(wt)
		}
	}
	s.ensureIdentity(helloData)
}

func (s *Session) extractUUIDAndUsername(data []byte) {
	d := proto.NewDecoder(data)
	for d.Remaining() > 0 {
		fn, wt, err := d.ReadTag()
		if err != nil {
			break
		}
		switch fn {
		case 1:
			raw, e := d.ReadMessage()
			if e == nil {
				s.uuid = parseUUIDFromBytes(raw)
			}
		case 2:
			s.username, _ = d.ReadString()
		default:
			d.Skip(wt)
		}
	}
}

func (s *Session) ensureIdentity(seed []byte) {

	if s.uuid == "" {
		s.uuid = fmt.Sprintf("00000000-0000-0000-0000-%012x", hashBytes(seed)&0xffffffffffff)
	}
	if s.username == "" {
		s.username = s.uuid[:8]
	}
}

// handleGameHandshake consumes the raw Handshake frame sent immediately after
// the RFC 6455 upgrade. Handshake.identity.player is fields 1.1.
func (s *Session) handleGameHandshake(data []byte) {
	d := proto.NewDecoder(data)
	for d.Remaining() > 0 {
		fn, wt, err := d.ReadTag()
		if err != nil {
			log.Printf("ERROR handshake decode: %v", err)
			return
		}
		if fn == 1 && wt == proto.LengthDelimited {
			identity, err := d.ReadMessage()
			if err != nil {
				return
			}
			id := proto.NewDecoder(identity)
			for id.Remaining() > 0 {
				f, w, e := id.ReadTag()
				if e != nil {
					break
				}
				if f == 1 && w == proto.LengthDelimited {
					player, _ := id.ReadMessage()
					s.extractUUIDAndUsername(player)
				} else {
					id.Skip(w)
				}
			}
		} else {
			d.Skip(wt)
		}
	}
	s.ensureIdentity(data)
	profile, err := s.db.GetProfile(s.uuid)
	if err != nil {
		log.Printf("ERROR db profile load [%s]: %v", s.uuid, err)
	}
	if profile == nil {
		profile = s.migrateLegacyProfile()
	}
	if profile == nil {
		profile = s.defaultProfile()
		if err := s.db.UpsertProfile(profile); err != nil {
			log.Printf("ERROR db profile create [%s]: %v", s.uuid, err)
		}
	}
	s.profile, s.cosmetics, s.authed, s.handshakeComplete = profile, profileToCosmetics(profile), true, true
	s.hub.registerSession(s)
	s.broadcastFriendStatus(1, true)
	log.Printf("AUTH game user=%s uuid=%s", s.username, s.uuid)
}

func (s *Session) migrateLegacyProfile() *db.UserProfile {
	if s.username == "" {
		return nil
	}
	profile, err := s.db.GetProfileByUsername(s.username)
	if err != nil || profile == nil || profile.UUID == s.uuid {
		return profile
	}
	if err := s.db.RekeyProfile(profile.UUID, s.uuid); err != nil {
		log.Printf("ERROR profile identity migration user=%s: %v", s.username, err)
		return nil
	}
	profile.UUID = s.uuid
	return profile
}

func parseUUIDFromBytes(data []byte) string {
	d := proto.NewDecoder(data)
	var high, low uint64
	for d.Remaining() > 0 {
		fn, wt, err := d.ReadTag()
		if err != nil {
			break
		}
		read := func() uint64 {
			if wt == proto.Fixed64 {
				value, _ := d.ReadFixed64()
				return value
			}
			if wt == proto.Varint {
				value, _ := d.ReadUint64()
				return value
			}
			_ = d.Skip(wt)
			return 0
		}
		switch fn {
		case 1:
			high = read()
		case 2:
			low = read()
		default:
			d.Skip(wt)
		}
	}
	return fmtUUID(high, low)
}

func fmtUUID(high, low uint64) string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uint32(high>>32), uint16(high>>16), uint16(high),
		uint16(low>>48), low&0x0000ffffffffffff)
}

func (s *Session) defaultProfile() *db.UserProfile {
	allCos := make([]int32, 0)
	for i := int32(1); i <= 500; i++ {
		allCos = append(allCos, i)
	}
	allEmote := make([]int32, 0)
	for i := int32(1); i <= 200; i++ {
		allEmote = append(allEmote, i)
	}
	return &db.UserProfile{
		UUID:              s.uuid,
		Username:          s.username,
		Rank:              "ADMIN",
		PlusColor:         0x00FFAA00,
		LogoColor:         0xFFAA0000,
		LogoAlwaysShow:    true,
		ArtistTools:       true,
		TesterTools:       true,
		EquippedCosmetics: []int32{},
		EquippedEmotes:    []int32{},
		EquippedBadge:     0,
		EquippedSprays:    []int32{},
		OwnedCosmetics:    allCos,
		OwnedEmotes:       allEmote,
		OwnedBadges:       allCos[:100],
		OwnedSprays:       allCos[:100],
		VisClothCloak:     false,
		VisHatsOverHelmet: true,
		VisHatsOverSkin:   true,
		VisOverChestplate: true,
		VisOverLeggings:   true,
		VisOverBoots:      true,
		VisFlipShoulder:   false,
	}
}

func profileToCosmetics(p *db.UserProfile) *proto.PlayerCosmetics {
	return &proto.PlayerCosmetics{
		Cosmetics:         p.OwnedCosmetics,
		Emotes:            p.OwnedEmotes,
		Badges:            p.OwnedBadges,
		Sprays:            p.OwnedSprays,
		EquippedCosmetics: p.EquippedCosmetics,
		EquippedEmotes:    p.EquippedEmotes,
		EquippedBadge:     p.EquippedBadge,
		EquippedSprays:    p.EquippedSprays,
		RankName:          p.Rank,
		PlusColor:         p.PlusColor,
		LogoColor:         p.LogoColor,
		LogoShow:          p.LogoAlwaysShow,
		ArtistTools:       p.ArtistTools,
		TesterTools:       p.TesterTools,
		VisClothCloak:     p.VisClothCloak,
		VisHatsOverHelmet: p.VisHatsOverHelmet,
		VisHatsOverSkin:   p.VisHatsOverSkin,
		VisOverChestplate: p.VisOverChestplate,
		VisOverLeggings:   p.VisOverLeggings,
		VisOverBoots:      p.VisOverBoots,
		VisFlipShoulder:   p.VisFlipShoulder,
	}
}

func cosmeticsToProfile(c *proto.PlayerCosmetics, p *db.UserProfile) {
	p.OwnedCosmetics = c.Cosmetics
	p.OwnedEmotes = c.Emotes
	p.OwnedBadges = c.Badges
	p.OwnedSprays = c.Sprays
	p.EquippedCosmetics = c.EquippedCosmetics
	p.EquippedEmotes = c.EquippedEmotes
	p.EquippedBadge = c.EquippedBadge
	p.EquippedSprays = c.EquippedSprays
	p.Rank = c.RankName
	p.PlusColor = c.PlusColor
	p.LogoColor = c.LogoColor
	p.LogoAlwaysShow = c.LogoShow
	p.ArtistTools = c.ArtistTools
	p.TesterTools = c.TesterTools
	p.VisClothCloak = c.VisClothCloak
	p.VisHatsOverHelmet = c.VisHatsOverHelmet
	p.VisHatsOverSkin = c.VisHatsOverSkin
	p.VisOverChestplate = c.VisOverChestplate
	p.VisOverLeggings = c.VisOverLeggings
	p.VisOverBoots = c.VisOverBoots
	p.VisFlipShoulder = c.VisFlipShoulder
}

func (s *Session) saveProfileLocked() {
	if s.profile == nil || s.cosmetics == nil {
		return
	}
	cosmeticsToProfile(s.cosmetics, s.profile)
	if err := s.db.UpsertProfile(s.profile); err != nil {
		log.Printf("ERROR profile persist user=%s uuid=%s: %v", s.username, s.uuid, err)
	}
}

func (s *Session) handleProtocolMessage(data []byte) {
	if !s.authed {
		log.Printf("WARN rejected RPC before handshake")
		return
	}

	msg, err := proto.ParseServerboundMessage(data)
	if err != nil {
		log.Printf("ERROR message parse [%s]: %v", s.uuid, err)
		return
	}
	if len(msg.RequestID) == 0 || msg.Service == "" || msg.Method == "" {
		log.Printf("WARN invalid RPC envelope [%s]: request_id=%d service=%q method=%q", s.uuid, len(msg.RequestID), msg.Service, msg.Method)
		return
	}
	s.handleRPC(msg.RequestID, msg.Service, msg.Method, msg.Input)
}

func shortServiceName(full string) string {
	if idx := strings.LastIndexByte(full, '.'); idx >= 0 {
		return full[idx+1:]
	}
	return full
}

func (s *Session) handleRPC(reqID []byte, service, method string, input []byte) {
	switch {
	case strings.Contains(service, "CosmeticService"):
		s.handleCosmetic(reqID, service, method, input)
	case strings.Contains(service, "EmoteService"):
		s.handleEmote(reqID, method, input)
	case strings.Contains(service, "BadgeService"):
		s.handleBadge(reqID, method, input)
	case strings.Contains(service, "SprayService"):
		s.handleSpray(reqID, method, input)
	case strings.Contains(service, "FriendService"):
		s.handleFriend(reqID, method, input)
	case strings.Contains(service, "SubscriptionService"):
		s.handleSubscription(reqID, method, input)
	case strings.Contains(service, "SettingService"):
		s.handleSetting(reqID, method, input)
	case strings.Contains(service, "PartyService"), strings.Contains(service, "ConversationService"):
		s.handleParty(reqID, method, input)
	case strings.Contains(service, "SessionService"), strings.Contains(service, "HeartbeatService"):
		s.handleSession(reqID, method, input)
	case strings.Contains(service, "HostedWorldService"):
		s.handleHostedWorld(reqID, method)
	default:
		// Return empty response so client doesn't hang
		s.respond(reqID, proto.EncodeEmptyResponse())
	}
}

func (s *Session) handleHostedWorld(reqID []byte, method string) {
	switch method {
	case "Login":
		s.respond(reqID, proto.EncodeHostedWorldLoginResponse(30))
	case "ListHostedWorlds":
		// An empty message is the valid encoding of an empty repeated worlds list.
		s.respond(reqID, proto.EncodeEmptyResponse())
	default:
		s.respond(reqID, proto.EncodeEmptyResponse())
	}
}

func (s *Session) respond(reqID []byte, data []byte) {
	resp := proto.EncodeClientboundMessage(reqID, data)
	s.sendRaw(resp)
}

// --- Cosmetic handlers ---
func (s *Session) handleCosmetic(reqID []byte, service, method string, input []byte) {
	switch method {
	case "Login":
		var respData []byte
		if strings.Contains(service, ".cosmetic.v2.") {
			outfits, err := s.db.CosmeticOutfits(s.uuid)
			if err != nil {
				log.Printf("ERROR outfit load user=%s: %v", s.username, err)
			}
			tree, err := s.db.CosmeticOutfitTree(s.uuid)
			if err != nil {
				log.Printf("ERROR outfit tree load user=%s: %v", s.username, err)
			}
			if len(outfits) == 0 {
				outfit := proto.EncodeCosmeticOutfit(proto.DefaultCosmeticOutfitID, "Default Outfit", s.cosmetics.EquippedCosmetics, s.cosmetics.EquippedBadge)
				outfits = []db.CosmeticOutfit{{ID: proto.DefaultCosmeticOutfitID, Data: outfit}}
				if err := s.db.SaveCosmeticOutfit(s.uuid, proto.DefaultCosmeticOutfitID, outfit); err != nil {
					log.Printf("ERROR default outfit persist user=%s: %v", s.username, err)
				}
			}
			if len(tree) == 0 {
				tree = proto.EncodeOutfitTree(outfits[0].ID)
				if err := s.db.SaveCosmeticOutfitTree(s.uuid, tree); err != nil {
					log.Printf("ERROR outfit tree persist user=%s: %v", s.username, err)
				}
			}
			rawOutfits := make([][]byte, 0, len(outfits))
			for _, outfit := range outfits {
				rawOutfits = append(rawOutfits, outfit.Data)
			}
			respData = proto.EncodeCosmeticV2LoginResponseWithOutfits(s.cosmetics, rawOutfits, tree)
		} else {
			respData = proto.EncodeCosmeticLoginResponse(s.cosmetics)
		}
		s.respond(reqID, respData)

	case "UpdateCosmeticSettings":
		d := proto.NewDecoder(input)
		for d.Remaining() > 0 {
			fn, wt, err := d.ReadTag()
			if err != nil {
				break
			}
			if fn == 1 { // settings message
				settingsData, err := d.ReadMessage()
				if err == nil {
					sd := proto.NewDecoder(settingsData)
					for sd.Remaining() > 0 {
						sfn, swt, serr := sd.ReadTag()
						if serr != nil {
							break
						}
						switch sfn {
						case 1:
							s.cosmetics.VisClothCloak, _ = sd.ReadBool()
						case 2:
							s.cosmetics.VisHatsOverHelmet, _ = sd.ReadBool()
						case 3:
							s.cosmetics.VisHatsOverSkin, _ = sd.ReadBool()
						case 4:
							s.cosmetics.VisOverChestplate, _ = sd.ReadBool()
						case 5:
							s.cosmetics.VisOverLeggings, _ = sd.ReadBool()
						case 6:
							s.cosmetics.VisOverBoots, _ = sd.ReadBool()
						case 7:
							s.cosmetics.VisFlipShoulder, _ = sd.ReadBool()
						default:
							sd.Skip(swt)
						}
					}
				}
			} else {
				d.Skip(wt)
			}
		}
		s.respond(reqID, proto.EncodeEmptyResponse())
		s.saveProfileLocked()

	case "LoadTabLogos":
		var logos []proto.TabLogo
		d := proto.NewDecoder(input)
		for d.Remaining() > 0 {
			fn, wt, err := d.ReadTag()
			if err != nil {
				break
			}
			if fn != 1 || wt != proto.LengthDelimited {
				d.Skip(wt)
				continue
			}
			raw, err := d.ReadBytes()
			if err != nil {
				break
			}
			uuid := parseUUIDFromBytes(raw)
			if profile, _ := s.db.GetProfile(uuid); profile != nil {
				logos = append(logos, proto.TabLogo{UUID: uuid, LogoColor: profile.LogoColor, PlusColor: profile.PlusColor, BadgeID: profile.EquippedBadge})
			}
		}
		s.respond(reqID, proto.EncodeLoadTabLogosResponse(logos))

	case "CreateOutfit":
		name := firstStringOrUUID(input)
		if name == "" {
			name = "Outfit"
		}
		outfitID := newUUID()
		outfit := proto.EncodeCosmeticOutfit(outfitID, name, nil, 0)
		if err := s.db.SaveCosmeticOutfit(s.uuid, outfitID, outfit); err != nil {
			log.Printf("ERROR outfit create user=%s: %v", s.username, err)
			s.respond(reqID, proto.EncodeOutfitMutationResponse(nil))
			break
		}
		s.respond(reqID, proto.EncodeOutfitMutationResponse(outfit))

	case "UpdateOutfit":
		outfit, outfitID, cosmetics, badge := parseOutfitUpdate(input)
		if len(outfit) == 0 {
			s.respond(reqID, proto.EncodeOutfitMutationResponse(nil))
			break
		}
		if outfitID == "" {
			log.Printf("WARN outfit update missing id user=%s", s.username)
			s.respond(reqID, proto.EncodeOutfitMutationResponse(nil))
			break
		}
		if err := s.db.SaveCosmeticOutfit(s.uuid, outfitID, outfit); err != nil {
			log.Printf("ERROR outfit update persist user=%s: %v", s.username, err)
			s.respond(reqID, proto.EncodeOutfitMutationResponse(nil))
			break
		}
		tree, _ := s.db.CosmeticOutfitTree(s.uuid)
		selectedOutfitID := parseOutfitTreeDefaultID(tree)
		selected := selectedOutfitID == "" || selectedOutfitID == outfitID
		if selected {
			s.cosmetics.EquippedCosmetics = cosmetics
			s.cosmetics.EquippedBadge = badge
			s.saveProfileLocked()
		}
		s.respond(reqID, proto.EncodeOutfitMutationResponse(outfit))
		if selected {
			s.sendCosmeticsPush()
		}

	case "SelectOutfit":
		tree, selected := parseOutfitTreeRequest(input)
		if len(tree) == 0 || selected == "" {
			log.Printf("WARN outfit select missing tree user=%s", s.username)
			s.respond(reqID, proto.EncodeOutfitMutationResponse(nil))
			break
		}
		outfit, err := s.db.CosmeticOutfit(s.uuid, selected)
		if err != nil || len(outfit) == 0 {
			log.Printf("WARN outfit select unknown user=%s outfit=%s err=%v", s.username, selected, err)
			s.respond(reqID, proto.EncodeOutfitMutationResponse(nil))
			break
		}
		if err := s.db.SaveCosmeticOutfitTree(s.uuid, tree); err != nil {
			log.Printf("ERROR outfit selection persist user=%s: %v", s.username, err)
			s.respond(reqID, proto.EncodeOutfitMutationResponse(nil))
			break
		}
		_, cosmetics, badge := parseOutfit(outfit)
		s.cosmetics.EquippedCosmetics = cosmetics
		s.cosmetics.EquippedBadge = badge
		s.saveProfileLocked()
		s.respond(reqID, proto.EncodeOutfitMutationResponse(nil))
		s.sendCosmeticsPush()

	case "DeleteOutfit":
		outfitID := firstStringOrUUID(input)
		if outfitID != "" {
			if err := s.db.DeleteCosmeticOutfit(s.uuid, outfitID); err != nil {
				log.Printf("ERROR outfit delete persist user=%s: %v", s.username, err)
			}
		}
		s.respond(reqID, proto.EncodeOutfitMutationResponse(nil))

	case "UpdateLunarPlusColor":
		d := proto.NewDecoder(input)
		for d.Remaining() > 0 {
			fn, wt, err := d.ReadTag()
			if err != nil {
				break
			}
			if fn != 3 || wt != proto.LengthDelimited {
				d.Skip(wt)
				continue
			}
			color, _ := d.ReadBytes()
			cd := proto.NewDecoder(color)
			if _, _, err := cd.ReadTag(); err == nil {
				value, _ := cd.ReadVarint()
				s.cosmetics.PlusColor = uint32(value)
			}
		}
		s.saveProfileLocked()
		s.respond(reqID, proto.EncodeEmptyResponse())
		s.sendCosmeticsPush()

	default:
		s.respond(reqID, proto.EncodeEmptyResponse())
	}
}

func parseOutfitUpdate(input []byte) (outfit []byte, outfitID string, cosmetics []int32, badge int32) {
	d := proto.NewDecoder(input)
	for d.Remaining() > 0 {
		fn, wt, err := d.ReadTag()
		if err != nil {
			return
		}
		if fn == 1 && wt == proto.LengthDelimited {
			outfit, _ = d.ReadBytes()
			break
		}
		d.Skip(wt)
	}
	outfitID, cosmetics, badge = parseOutfit(outfit)
	return
}

func parseOutfit(outfit []byte) (outfitID string, cosmetics []int32, badge int32) {
	od := proto.NewDecoder(outfit)
	for od.Remaining() > 0 {
		fn, wt, err := od.ReadTag()
		if err != nil {
			break
		}
		switch fn {
		case 1:
			raw, err := od.ReadBytes()
			if err == nil {
				outfitID = parseUUIDFromBytes(raw)
			}
		case 3:
			raw, err := od.ReadBytes()
			if err != nil {
				continue
			}
			cd := proto.NewDecoder(raw)
			if _, _, err := cd.ReadTag(); err == nil {
				id, _ := cd.ReadInt32()
				cosmetics = append(cosmetics, id)
			}
		case 8:
			badge, _ = od.ReadInt32()
		default:
			od.Skip(wt)
		}
	}
	return
}

func parseOutfitTreeRequest(input []byte) (tree []byte, defaultOutfitID string) {
	d := proto.NewDecoder(input)
	for d.Remaining() > 0 {
		fn, wt, err := d.ReadTag()
		if err != nil {
			return
		}
		if fn == 1 && wt == proto.LengthDelimited {
			tree, _ = d.ReadBytes()
			defaultOutfitID = parseOutfitTreeDefaultID(tree)
			return
		}
		d.Skip(wt)
	}
	return
}

func parseOutfitTreeDefaultID(tree []byte) string {
	d := proto.NewDecoder(tree)
	for d.Remaining() > 0 {
		fn, wt, err := d.ReadTag()
		if err != nil {
			return ""
		}
		if fn == 2 && wt == proto.LengthDelimited {
			raw, _ := d.ReadBytes()
			return parseUUIDFromBytes(raw)
		}
		d.Skip(wt)
	}
	return ""
}

// --- Emote handlers ---
func (s *Session) handleEmote(reqID []byte, method string, input []byte) {
	switch method {
	case "Login":
		respData := proto.EncodeEmoteLoginResponse(s.cosmetics)
		s.respond(reqID, respData)

	case "UseEmote":
		emoteID, metadata, jamID := int32(0), int32(0), int32(0)
		soundtrackURL := ""
		d := proto.NewDecoder(input)
		for d.Remaining() > 0 {
			fn, wt, err := d.ReadTag()
			if err != nil {
				break
			}
			switch fn {
			case 1:
				emoteID, _ = d.ReadInt32()
			case 2:
				metadata, _ = d.ReadInt32()
			case 3:
				soundtrackURL, _ = d.ReadString()
			case 4:
				jamID, _ = d.ReadInt32()
			default:
				d.Skip(wt)
			}
		}

		s.respond(reqID, proto.EncodeUseEmoteResponse(emoteID, metadata))

		// Broadcast UseEmotePush to all other players
		pushData := proto.EncodeUseEmotePush(s.uuid, emoteID, metadata, soundtrackURL, jamID)
		push := proto.EncodePush(
			"type.googleapis.com/lunarclient.websocket.emote.v1.UseEmotePush",
			pushData,
		)
		s.hub.broadcast <- broadcastMsg{
			from: s,
			data: push,
		}

	case "StopEmote":
		s.respond(reqID, proto.EncodeStopEmoteResponse())

		pushData := proto.EncodeStopEmotePush(s.uuid)
		push := proto.EncodePush(
			"type.googleapis.com/lunarclient.websocket.emote.v1.StopEmotePush",
			pushData,
		)
		s.hub.broadcast <- broadcastMsg{from: s, data: push}
	case "UpdateEquippedEmotes":
		// Parse equipped emote IDs
		eqEmotes := []int32{}
		d := proto.NewDecoder(input)
		for d.Remaining() > 0 {
			fn, wt, err := d.ReadTag()
			if err != nil {
				break
			}
			switch fn {
			case 1:
				if wt == proto.LengthDelimited {
					values, _ := d.ReadPackedInt32()
					eqEmotes = append(eqEmotes, values...)
				} else {
					v, _ := d.ReadInt32()
					eqEmotes = append(eqEmotes, v)
				}
			case 2:
				raw, _ := d.ReadBytes()
				ed := proto.NewDecoder(raw)
				if _, _, err := ed.ReadTag(); err == nil {
					v, _ := ed.ReadInt32()
					eqEmotes = append(eqEmotes, v)
				}
			default:
				d.Skip(wt)
			}
		}
		s.cosmetics.EquippedEmotes = eqEmotes
		s.respond(reqID, proto.EncodeUpdateEquippedEmotesResponse())
		s.saveProfileLocked()

	default:
		s.respond(reqID, proto.EncodeEmptyResponse())
	}
}

// --- Badge handlers ---
func (s *Session) handleBadge(reqID []byte, method string, input []byte) {
	switch method {
	case "Login":
		respData := proto.EncodeBadgeLoginResponse(s.cosmetics)
		s.respond(reqID, respData)

	case "EquipBadge":
		badgeID := int32(0)
		d := proto.NewDecoder(input)
		for d.Remaining() > 0 {
			fn, _, err := d.ReadTag()
			if err != nil {
				break
			}
			if fn == 1 { // badge_id
				badgeID, _ = d.ReadInt32()
			} else {
				d.Skip(proto.Varint)
			}
		}
		s.cosmetics.EquippedBadge = badgeID
		s.respond(reqID, proto.EncodeEquipBadgeResponse(badgeID))
		s.saveProfileLocked()

		// Broadcast updated cosmetics push
		s.sendCosmeticsPush()
	default:
		s.respond(reqID, proto.EncodeEmptyResponse())
	}
}

// --- Spray handlers ---
func (s *Session) handleSpray(reqID []byte, method string, input []byte) {
	switch method {
	case "Login":
		respData := proto.EncodeSprayLoginResponse(s.cosmetics)
		s.respond(reqID, respData)

	case "UseSpray":
		sprayID := int32(0)
		d := proto.NewDecoder(input)
		for d.Remaining() > 0 {
			fn, _, err := d.ReadTag()
			if err != nil {
				break
			}
			if fn == 1 { // spray_id
				sprayID, _ = d.ReadInt32()
			} else {
				d.Skip(proto.Varint)
			}
		}
		s.respond(reqID, proto.EncodeUseSprayResponse(sprayID))

		pushData := proto.EncodeUseSprayPush(s.uuid, sprayID)
		push := proto.EncodePush(
			"type.googleapis.com/lunarclient.websocket.spray.v1.UseSprayPush",
			pushData,
		)
		s.hub.broadcast <- broadcastMsg{from: s, data: push}
	case "RemoveSpray":
		sprayID := int32(0)
		d := proto.NewDecoder(input)
		for d.Remaining() > 0 {
			fn, _, err := d.ReadTag()
			if err != nil {
				break
			}
			if fn == 1 {
				sprayID, _ = d.ReadInt32()
			} else {
				d.Skip(proto.Varint)
			}
		}
		s.respond(reqID, proto.EncodeRemoveSprayResponse(sprayID))

		pushData := proto.EncodeRemoveSprayPush(s.uuid, sprayID)
		push := proto.EncodePush(
			"type.googleapis.com/lunarclient.websocket.spray.v1.RemoveSprayPush",
			pushData,
		)
		s.hub.broadcast <- broadcastMsg{from: s, data: push}

	case "UpdateEquippedSprays":
		eqSprays := []int32{}
		d := proto.NewDecoder(input)
		for d.Remaining() > 0 {
			fn, wt, err := d.ReadTag()
			if err != nil {
				break
			}
			switch fn {
			case 1:
				raw, _ := d.ReadBytes()
				sd := proto.NewDecoder(raw)
				if _, _, err := sd.ReadTag(); err == nil {
					v, _ := sd.ReadInt32()
					eqSprays = append(eqSprays, v)
				}
			default:
				d.Skip(wt)
			}
		}
		s.cosmetics.EquippedSprays = eqSprays
		s.respond(reqID, proto.EncodeUpdateEquippedSpraysResponse())
		s.saveProfileLocked()

	default:
		s.respond(reqID, proto.EncodeEmptyResponse())
	}
}

// sendCosmeticsPush broadcasts updated cosmetics to all other players
func (s *Session) sendCosmeticsPush() {
	pushData := proto.EncodePlayerCosmeticsPushV2(s.uuid, s.cosmetics)
	push := proto.EncodePush(
		"type.googleapis.com/lunarclient.websocket.cosmetic.v2.PlayerCosmeticsPushV2",
		pushData,
	)
	s.hub.broadcast <- broadcastMsg{from: s, data: push}
}

// JSON helper
func jsonStr(i interface{}) string {
	b, _ := json.Marshal(i)
	return string(b)
}

func hashBytes(b []byte) uint64 {
	h := uint64(14695981039346656037)
	for _, c := range b {
		h = (h ^ uint64(c)) * 1099511628211
	}
	return h
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
