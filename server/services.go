package server

import (
	"crypto/rand"
	"encoding/hex"
	"lcserver/db"
	"lcserver/proto"
	"log"
	"strings"
	"time"
)

func firstStringOrUUID(data []byte) string {
	d := proto.NewDecoder(data)
	for d.Remaining() > 0 {
		fn, wt, err := d.ReadTag()
		if err != nil {
			return ""
		}
		if wt == proto.LengthDelimited {
			raw, err := d.ReadBytes()
			if err != nil {
				return ""
			}
			if fn == 1 {
				if u, ok := strictUUID(raw); ok {
					return u
				}
				if len(raw) > 0 && !strings.ContainsRune(string(raw), '\x00') {
					return string(raw)
				}
			}
		} else {
			d.Skip(wt)
		}
	}
	return ""
}

func strictUUID(raw []byte) (string, bool) {
	d := proto.NewDecoder(raw)
	var high, low uint64
	seen := false
	for d.Remaining() > 0 {
		fn, wt, err := d.ReadTag()
		if err != nil || wt != proto.Fixed64 || (fn != 1 && fn != 2) {
			return "", false
		}
		value, err := d.ReadFixed64()
		if err != nil {
			return "", false
		}
		if fn == 1 {
			high = value
		} else {
			low = value
		}
		seen = true
	}
	if !seen {
		return "", false
	}
	return fmtUUID(high, low), true
}

func (s *Session) handleFriend(reqID []byte, method string, input []byte) {
	switch method {
	case "Login":
		e := proto.NewEncoder()
		e.Enum(1, 1) // ONLINE
		e.Bool(3, true)
		entries, err := s.db.FriendEntries(s.uuid)
		if err != nil {
			log.Printf("ERROR friend login user=%s: %v", s.username, err)
		}
		for _, entry := range entries {
			profile, err := s.db.GetProfile(entry.FriendUUID)
			if err != nil || profile == nil {
				continue
			}
			switch entry.State {
			case "OUTBOUND":
				e.Message(6, proto.EncodeFriendRequest(profile.UUID, profile.Username, entry.CreatedAt, profile.LogoColor, profile.PlusColor, profile.Rank, profile.EquippedBadge))
			case "INBOUND":
				e.Message(7, proto.EncodeFriendRequest(profile.UUID, profile.Username, entry.CreatedAt, profile.LogoColor, profile.PlusColor, profile.Rank, profile.EquippedBadge))
			case "FRIEND":
				if s.hub.isOnline(profile.UUID) {
					e.Message(11, proto.EncodeOnlineFriend(profile.UUID, profile.Username, 1, false, profile.LogoColor, profile.PlusColor, profile.Rank, profile.EquippedBadge, entry.CreatedAt))
				} else {
					e.Message(2, proto.EncodeOfflineFriend(profile.UUID, profile.Username, profile.LastSeen, entry.CreatedAt, profile.LogoColor, profile.PlusColor, profile.Rank, profile.EquippedBadge))
				}
			}
		}
		e.Enum(9, 1)
		e.Enum(10, 1)
		proto.EncodeTimestamp(12, s.profile.CreatedAt, 0, e)
		s.respond(reqID, e.Bytes())
	case "SendFriendRequest":
		targetName := firstStringOrUUID(input)
		target, err := s.db.GetProfileByUsername(targetName)
		if err != nil || target == nil {
			s.respond(reqID, proto.EncodeSendFriendRequestResponse(3, "", "", 0, 0, "", 0))
			break
		}
		if target.UUID == s.uuid {
			s.respond(reqID, proto.EncodeSendFriendRequestResponse(5, target.UUID, target.Username, target.LogoColor, target.PlusColor, target.Rank, target.EquippedBadge))
			break
		}
		entries, _ := s.db.FriendEntries(s.uuid)
		status := int32(1)
		for _, entry := range entries {
			if entry.FriendUUID == target.UUID {
				switch entry.State {
				case "FRIEND":
					status = 6
				case "INBOUND":
					status = 7
				case "OUTBOUND":
					status = 8
				}
				break
			}
		}
		if status == 1 {
			if err := s.db.SetFriendship(s.uuid, target.UUID, "OUTBOUND", "INBOUND"); err != nil {
				log.Printf("ERROR friend request persist user=%s target=%s: %v", s.username, target.Username, err)
				s.respond(reqID, proto.EncodeSendFriendRequestResponse(3, target.UUID, target.Username, target.LogoColor, target.PlusColor, target.Rank, target.EquippedBadge))
				break
			}
			push := proto.EncodePush("type.googleapis.com/lunarclient.websocket.friend.v1.FriendRequestReceivedPush", proto.EncodeFriendRequestReceivedPush(s.uuid, s.username, s.profile.LogoColor, s.profile.PlusColor, s.profile.Rank, s.profile.EquippedBadge))
			s.hub.sendToUUID(target.UUID, push)
		}
		s.respond(reqID, proto.EncodeSendFriendRequestResponse(status, target.UUID, target.Username, target.LogoColor, target.PlusColor, target.Rank, target.EquippedBadge))
	case "AcceptFriendRequest":
		target := firstStringOrUUID(input)
		var offlineFriend []byte
		if target != "" {
			if err := s.db.SetFriendship(s.uuid, target, "FRIEND", "FRIEND"); err != nil {
				log.Printf("ERROR friend accept persist user=%s target=%s: %v", s.username, target, err)
				s.respond(reqID, proto.EncodeAcceptFriendRequestResponse(3, nil))
				break
			}
			if profile, _ := s.db.GetProfile(target); profile != nil {
				offlineFriend = proto.EncodeOfflineFriend(profile.UUID, profile.Username, profile.LastSeen, time.Now().Unix(), profile.LogoColor, profile.PlusColor, profile.Rank, profile.EquippedBadge)
			}
			push := proto.EncodePush("type.googleapis.com/lunarclient.websocket.friend.v1.FriendRequestAcceptedPush", proto.EncodeFriendRequestAcceptedPush(s.uuid, s.username))
			s.hub.sendToUUID(target, push)
		}
		s.respond(reqID, proto.EncodeAcceptFriendRequestResponse(1, offlineFriend))
		if target != "" {
			refresh := proto.EncodePush("type.googleapis.com/lunarclient.websocket.conversation.v1.RefreshConversationsPush", proto.EncodeEmptyResponse())
			s.sendRaw(refresh)
			s.hub.sendToUUID(target, refresh)
			if targetSession := s.hub.session(target); targetSession != nil {
				s.sendFriendStatusTo(target, 1, true, time.Now().Unix())
				targetSession.sendFriendStatusTo(s.uuid, 1, true, time.Now().Unix())
			}
		}
	case "DenyFriendRequest", "CancelFriendRequest", "RemoveFriend":
		target := firstStringOrUUID(input)
		if target != "" {
			if err := s.db.SetFriendship(s.uuid, target, "", ""); err != nil {
				log.Printf("ERROR friend remove persist user=%s target=%s: %v", s.username, target, err)
				s.respond(reqID, proto.EncodeEmptyResponse())
				break
			}
			pushType := "FriendRemovedYouPush"
			if method == "DenyFriendRequest" {
				pushType = "FriendRequestDeniedPush"
			}
			if method == "CancelFriendRequest" {
				pushType = "FriendRequestCanceledPush"
			}
			payload := proto.NewEncoder()
			proto.EncodeUUID(1, s.uuid, payload)
			s.hub.sendToUUID(target, proto.EncodePush("type.googleapis.com/lunarclient.websocket.friend.v1."+pushType, payload.Bytes()))
		}
		s.respond(reqID, proto.EncodeEmptyResponse())
	case "BroadcastStatusChange":
		status := int32(1)
		d := proto.NewDecoder(input)
		if _, _, err := d.ReadTag(); err == nil {
			status, _ = d.ReadInt32()
		}
		s.broadcastFriendStatus(status, status == 1)
		s.respond(reqID, proto.EncodeEmptyResponse())
	case "AddFriendPin", "RemoveFriendPin":
		target := firstStringOrUUID(input)
		if target != "" {
			if err := s.db.SetFriendPinned(s.uuid, target, method == "AddFriendPin"); err != nil {
				log.Printf("ERROR friend pin persist user=%s target=%s: %v", s.username, target, err)
			}
		}
		s.respond(reqID, proto.EncodeEmptyResponse())
	default:
		// Broadcast/status/privacy methods have empty response messages.
		s.respond(reqID, proto.EncodeEmptyResponse())
	}
}

func parseSetting(input []byte) (id string, value []byte) {
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
		setting, _ := d.ReadMessage()
		sd := proto.NewDecoder(setting)
		for sd.Remaining() > 0 {
			f, w, e := sd.ReadTag()
			if e != nil {
				break
			}
			switch f {
			case 1:
				id, _ = sd.ReadString()
			case 2:
				value, _ = sd.ReadBytes()
			default:
				sd.Skip(w)
			}
		}
	}
	return
}

func (s *Session) handleSetting(reqID []byte, method string, input []byte) {
	scope := "client"
	if strings.Contains(method, "Launcher") {
		scope = "launcher"
	}
	id, value := parseSetting(input)
	if id != "" {
		if err := s.db.PutSetting(s.uuid, scope, id, value); err != nil {
			log.Printf("ERROR setting save user=%s id=%s: %v", s.username, id, err)
		}
	}
	s.respond(reqID, proto.EncodeEmptyResponse())
}

func (s *Session) handleSession(reqID []byte, method string, input []byte) {
	// Heartbeat responses are empty in the generated client protocol. Touching the
	// profile makes session liveness durable for the admin/API side.
	if s.profile != nil {
		s.profile.LastSeen = time.Now().Unix()
		if err := s.db.UpsertProfile(s.profile); err != nil {
			log.Printf("ERROR session touch user=%s: %v", s.username, err)
		}
	}
	s.respond(reqID, proto.EncodeEmptyResponse())
}

func (s *Session) handleParty(reqID []byte, method string, input []byte) {
	s.handleConversation(reqID, method, input)
}

func (s *Session) broadcastFriendStatus(status int32, justCameOnline bool) {
	entries, _ := s.db.FriendEntries(s.uuid)
	for _, entry := range entries {
		if entry.State != "FRIEND" {
			continue
		}
		s.sendFriendStatusTo(entry.FriendUUID, status, justCameOnline, entry.CreatedAt)
	}
}

func (s *Session) sendFriendStatusTo(friendUUID string, status int32, justCameOnline bool, friendsSince int64) {
	if s.profile == nil {
		return
	}
	payload := proto.NewEncoder()
	if status >= 1 && status <= 3 {
		payload.Message(1, proto.EncodeOnlineFriend(s.uuid, s.username, status, justCameOnline, s.profile.LogoColor, s.profile.PlusColor, s.profile.Rank, s.profile.EquippedBadge, friendsSince))
	} else {
		payload.Message(2, proto.EncodeOfflineFriend(s.uuid, s.username, time.Now().Unix(), friendsSince, s.profile.LogoColor, s.profile.PlusColor, s.profile.Rank, s.profile.EquippedBadge))
	}
	s.hub.sendToUUID(friendUUID, proto.EncodePush("type.googleapis.com/lunarclient.websocket.friend.v1.FriendStatusPush", payload.Bytes()))
}

func subscriptionTargets(input []byte) []string {
	d := proto.NewDecoder(input)
	targets := make([]string, 0)
	seen := make(map[string]struct{})
	for d.Remaining() > 0 {
		fn, wt, err := d.ReadTag()
		if err != nil {
			break
		}
		if fn != 1 || wt != proto.LengthDelimited {
			d.Skip(wt)
			continue
		}
		raw, err := d.ReadMessage()
		if err != nil {
			break
		}
		uuid, ok := strictUUID(raw)
		if !ok {
			continue
		}
		if _, exists := seen[uuid]; !exists {
			seen[uuid] = struct{}{}
			targets = append(targets, uuid)
		}
	}
	return targets
}

func (s *Session) handleSubscription(reqID []byte, method string, input []byte) {
	if method == "Unsubscribe" {
		s.respond(reqID, proto.EncodeEmptyResponse())
		return
	}
	if method != "Subscribe" && method != "SubscribeV2" {
		s.respond(reqID, proto.EncodeEmptyResponse())
		return
	}

	response := proto.NewEncoder()
	for _, uuid := range subscriptionTargets(input) {
		profile, err := s.db.GetProfile(uuid)
		if err != nil {
			log.Printf("ERROR subscription profile=%s user=%s: %v", uuid, s.username, err)
			continue
		}
		if profile == nil {
			continue
		}
		cosmetics := profileToCosmetics(profile)
		if uuid == s.uuid && s.cosmetics != nil {
			cosmetics = s.cosmetics
		}
		if method == "SubscribeV2" {
			response.Message(1, proto.EncodePlayerCosmeticsPushV2(uuid, cosmetics))
		} else {
			response.Message(1, proto.EncodePlayerCosmeticsPush(uuid, cosmetics))
		}
	}
	s.respond(reqID, response.Bytes())
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func conversationInput(input []byte) (target string, contents []byte) {
	d := proto.NewDecoder(input)
	for d.Remaining() > 0 {
		fn, wt, err := d.ReadTag()
		if err != nil {
			break
		}
		if wt != proto.LengthDelimited {
			d.Skip(wt)
			continue
		}
		raw, err := d.ReadBytes()
		if err != nil {
			break
		}
		if fn == 1 {
			reference := proto.NewDecoder(raw)
			if _, _, err := reference.ReadTag(); err == nil {
				uuidRaw, _ := reference.ReadBytes()
				target = parseUUIDFromBytes(uuidRaw)
			}
		} else if fn == 2 {
			contents = raw
		}
	}
	return
}

func (s *Session) isFriend(uuid string) bool {
	entries, _ := s.db.FriendEntries(s.uuid)
	for _, entry := range entries {
		if entry.FriendUUID == uuid && entry.State == "FRIEND" {
			return true
		}
	}
	return false
}

func (s *Session) conversationFor(friend *db.UserProfile) []byte {
	e := proto.NewEncoder()
	e.Message(1, proto.EncodeConversationReference(friend.UUID))
	e.Message(2, proto.EncodeConversationParticipant(s.uuid, s.username, s.profile.LogoColor, s.profile.PlusColor, s.profile.Rank, s.profile.EquippedBadge))
	e.Message(2, proto.EncodeConversationParticipant(friend.UUID, friend.Username, friend.LogoColor, friend.PlusColor, friend.Rank, friend.EquippedBadge))
	e.Enum(3, 1)
	proto.EncodeTimestamp(4, time.Now().Unix(), 0, e)
	return e.Bytes()
}

func (s *Session) handleConversation(reqID []byte, method string, input []byte) {
	switch method {
	case "Login":
		e := proto.NewEncoder()
		entries, _ := s.db.FriendEntries(s.uuid)
		count := int32(0)
		for _, entry := range entries {
			if entry.State == "FRIEND" {
				e.Message(1, proto.EncodeConversationStub(entry.FriendUUID))
				count++
			}
		}
		e.Int32(2, 32)
		e.Int32(5, count)
		e.Int32(6, 2000)
		e.Int32(7, 20)
		s.respond(reqID, e.Bytes())
	case "GetConversations":
		e := proto.NewEncoder()
		e.Enum(1, 1)
		entries, _ := s.db.FriendEntries(s.uuid)
		count := int32(0)
		for _, entry := range entries {
			if entry.State != "FRIEND" {
				continue
			}
			if friend, _ := s.db.GetProfile(entry.FriendUUID); friend != nil {
				e.Message(2, s.conversationFor(friend))
				count++
			}
		}
		e.Int32(3, count)
		s.respond(reqID, e.Bytes())
	case "SendConversationMessage":
		target, contents := conversationInput(input)
		if target == "" || len(contents) == 0 || !s.isFriend(target) {
			e := proto.NewEncoder()
			e.Enum(1, 2)
			s.respond(reqID, e.Bytes())
			return
		}
		message := &db.DirectMessage{ID: newUUID(), SenderUUID: s.uuid, RecipientUUID: target, Contents: append([]byte(nil), contents...), SentAt: time.Now().Unix()}
		if err := s.db.SaveDirectMessage(message); err != nil {
			e := proto.NewEncoder()
			e.Enum(1, 2)
			s.respond(reqID, e.Bytes())
			return
		}
		e := proto.NewEncoder()
		e.Enum(1, 1)
		s.respond(reqID, e.Bytes())
		encoded := proto.EncodeConversationMessage(message.ID, message.SentAt, s.uuid, s.username, contents)
		s.hub.sendToUUID(s.uuid, proto.EncodePush("type.googleapis.com/lunarclient.websocket.conversation.v1.ConversationMessagePush", proto.EncodeConversationMessagePush(target, encoded)))
		s.hub.sendToUUID(target, proto.EncodePush("type.googleapis.com/lunarclient.websocket.conversation.v1.ConversationMessagePush", proto.EncodeConversationMessagePush(s.uuid, encoded)))
	case "LoadConversation":
		target, _ := conversationInput(input)
		e := proto.NewEncoder()
		messages, _ := s.db.DirectMessages(s.uuid, target, 100)
		for _, message := range messages {
			senderName := s.username
			if message.SenderUUID != s.uuid {
				if profile, _ := s.db.GetProfile(message.SenderUUID); profile != nil {
					senderName = profile.Username
				}
			}
			e.Message(1, proto.EncodeConversationMessage(message.ID, message.SentAt, message.SenderUUID, senderName, message.Contents))
		}
		s.respond(reqID, e.Bytes())
	case "PreSendAction":
		e := proto.NewEncoder()
		e.Enum(1, 1)
		s.respond(reqID, e.Bytes())
	default:
		s.respond(reqID, proto.EncodeEmptyResponse())
	}
}
