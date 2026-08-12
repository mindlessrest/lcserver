package server

import (
	"lcserver/db"
	"lcserver/proto"
	"testing"
)

func TestFriendTargetParsingDistinguishesUsernameAndUUID(t *testing.T) {
	username := proto.NewEncoder()
	username.String(1, "Alice123")
	if got := firstStringOrUUID(username.Bytes()); got != "Alice123" {
		t.Fatalf("username parsed as %q", got)
	}
	uuid := proto.NewEncoder()
	proto.EncodeUUID(1, "12345678-1234-5678-9abc-def012345678", uuid)
	if got := firstStringOrUUID(uuid.Bytes()); got != "12345678-1234-5678-9abc-def012345678" {
		t.Fatalf("UUID parsed as %q", got)
	}
}

func TestParseOutfitUpdate(t *testing.T) {
	outfit := proto.NewEncoder()
	proto.EncodeUUID(1, "12345678-1234-5678-9abc-def012345678", outfit)
	outfit.Message(3, proto.EncodeEquippedCosmetic(12))
	outfit.Message(3, proto.EncodeEquippedCosmetic(34))
	outfit.Int32(8, 7)
	request := proto.NewEncoder()
	request.Message(1, outfit.Bytes())
	raw, outfitID, cosmetics, badge := parseOutfitUpdate(request.Bytes())
	if len(raw) == 0 || outfitID != "12345678-1234-5678-9abc-def012345678" || len(cosmetics) != 2 || cosmetics[0] != 12 || cosmetics[1] != 34 || badge != 7 {
		t.Fatalf("bad outfit parse: raw=%d id=%s cosmetics=%v badge=%d", len(raw), outfitID, cosmetics, badge)
	}
}

func TestConversationInput(t *testing.T) {
	contents := proto.NewEncoder()
	contents.String(1, "hello")
	request := proto.NewEncoder()
	request.Message(1, proto.EncodeConversationReference("12345678-1234-5678-9abc-def012345678"))
	request.Message(2, contents.Bytes())
	target, raw := conversationInput(request.Bytes())
	if target != "12345678-1234-5678-9abc-def012345678" || len(raw) == 0 {
		t.Fatalf("bad conversation parse: target=%q contents=%x", target, raw)
	}
}

func unwrapRPCOutput(t *testing.T, raw []byte) []byte {
	t.Helper()
	outer := proto.NewDecoder(raw)
	field, wt, err := outer.ReadTag()
	if err != nil || field != 1 || wt != proto.LengthDelimited {
		t.Fatalf("bad clientbound wrapper: field=%d wire=%d err=%v", field, wt, err)
	}
	rpc, err := outer.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	d := proto.NewDecoder(rpc)
	for d.Remaining() > 0 {
		field, wt, err = d.ReadTag()
		if err != nil {
			t.Fatal(err)
		}
		if field == 2 && wt == proto.LengthDelimited {
			output, err := d.ReadBytes()
			if err != nil {
				t.Fatal(err)
			}
			return output
		}
		if err := d.Skip(wt); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("RPC output not found")
	return nil
}

func unwrapPush(t *testing.T, raw []byte) (string, []byte) {
	t.Helper()
	outer := proto.NewDecoder(raw)
	field, wt, err := outer.ReadTag()
	if err != nil || field != 2 || wt != proto.LengthDelimited {
		t.Fatalf("bad push wrapper: field=%d wire=%d err=%v", field, wt, err)
	}
	any, err := outer.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	d := proto.NewDecoder(any)
	var typeURL string
	var payload []byte
	for d.Remaining() > 0 {
		field, wt, err = d.ReadTag()
		if err != nil {
			t.Fatal(err)
		}
		switch field {
		case 1:
			typeURL, err = d.ReadString()
		case 2:
			payload, err = d.ReadBytes()
		default:
			err = d.Skip(wt)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	return typeURL, payload
}

func TestSubscriptionV2ReturnsSavedCosmetics(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	profile := &db.UserProfile{
		UUID:              "22222222-2222-4222-8222-222222222222",
		Username:          "Bob",
		EquippedCosmetics: []int32{12, 34},
		EquippedBadge:     7,
	}
	if err := database.UpsertProfile(profile); err != nil {
		t.Fatal(err)
	}
	hub := NewHub(database)
	s := &Session{uuid: "11111111-1111-4111-8111-111111111111", username: "Alice", db: database, hub: hub, profile: &db.UserProfile{}, writeCh: make(chan []byte, 2)}
	request := proto.NewEncoder()
	proto.EncodeUUID(1, profile.UUID, request)
	s.handleRPC([]byte("sub"), "lunarclient.websocket.subscription.v1.SubscriptionService", "SubscribeV2", request.Bytes())
	output := unwrapRPCOutput(t, <-s.writeCh)
	d := proto.NewDecoder(output)
	field, wt, err := d.ReadTag()
	if err != nil || field != 1 || wt != proto.LengthDelimited {
		t.Fatalf("missing cosmetic push: field=%d wire=%d err=%v", field, wt, err)
	}
	push, err := d.ReadMessage()
	if err != nil || len(push) == 0 {
		t.Fatalf("empty cosmetic push: %v", err)
	}
}

func TestAwayStatusRemainsOnline(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	aliceProfile := &db.UserProfile{UUID: "11111111-1111-4111-8111-111111111111", Username: "Alice"}
	bobProfile := &db.UserProfile{UUID: "22222222-2222-4222-8222-222222222222", Username: "Bob"}
	if err := database.UpsertProfile(aliceProfile); err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertProfile(bobProfile); err != nil {
		t.Fatal(err)
	}
	if err := database.SetFriendState(aliceProfile.UUID, bobProfile.UUID, "FRIEND"); err != nil {
		t.Fatal(err)
	}
	hub := NewHub(database)
	alice := &Session{uuid: aliceProfile.UUID, username: aliceProfile.Username, db: database, hub: hub, profile: aliceProfile, writeCh: make(chan []byte, 2)}
	bob := &Session{uuid: bobProfile.UUID, username: bobProfile.Username, db: database, hub: hub, profile: bobProfile, writeCh: make(chan []byte, 2)}
	hub.sessions[alice.uuid], hub.sessions[bob.uuid] = alice, bob
	alice.broadcastFriendStatus(2, true)
	typeURL, payload := unwrapPush(t, <-bob.writeCh)
	if typeURL != "type.googleapis.com/lunarclient.websocket.friend.v1.FriendStatusPush" {
		t.Fatalf("wrong push type: %s", typeURL)
	}
	d := proto.NewDecoder(payload)
	field, _, err := d.ReadTag()
	if err != nil || field != 1 {
		t.Fatalf("away status encoded as offline: field=%d err=%v", field, err)
	}
}

func TestFriendRequestAcceptAndDirectMessage(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	aliceProfile := &db.UserProfile{UUID: "11111111-1111-4111-8111-111111111111", Username: "Alice", Rank: "ADMIN"}
	bobProfile := &db.UserProfile{UUID: "22222222-2222-4222-8222-222222222222", Username: "Bob", Rank: "ADMIN"}
	if err := database.UpsertProfile(aliceProfile); err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertProfile(bobProfile); err != nil {
		t.Fatal(err)
	}
	hub := NewHub(database)
	alice := &Session{uuid: aliceProfile.UUID, username: aliceProfile.Username, db: database, hub: hub, profile: aliceProfile, writeCh: make(chan []byte, 8)}
	bob := &Session{uuid: bobProfile.UUID, username: bobProfile.Username, db: database, hub: hub, profile: bobProfile, writeCh: make(chan []byte, 8)}
	hub.sessions[alice.uuid], hub.sessions[bob.uuid] = alice, bob

	sendRequest := proto.NewEncoder()
	sendRequest.String(1, "Bob")
	alice.handleFriend([]byte("send"), "SendFriendRequest", sendRequest.Bytes())
	entries, err := database.FriendEntries(bob.uuid)
	if err != nil || len(entries) != 1 || entries[0].State != "INBOUND" {
		t.Fatalf("bad inbound request: %v %#v", err, entries)
	}
	if len(bob.writeCh) != 1 {
		t.Fatalf("Bob did not receive request push: queued=%d", len(bob.writeCh))
	}

	acceptRequest := proto.NewEncoder()
	proto.EncodeUUID(1, alice.uuid, acceptRequest)
	bob.handleFriend([]byte("accept"), "AcceptFriendRequest", acceptRequest.Bytes())
	entries, err = database.FriendEntries(alice.uuid)
	if err != nil || len(entries) != 1 || entries[0].State != "FRIEND" {
		t.Fatalf("bad accepted friendship: %v %#v", err, entries)
	}

	contents := proto.NewEncoder()
	contents.String(1, "hello Bob")
	messageRequest := proto.NewEncoder()
	messageRequest.Message(1, proto.EncodeConversationReference(bob.uuid))
	messageRequest.Message(2, contents.Bytes())
	alice.handleConversation([]byte("message"), "SendConversationMessage", messageRequest.Bytes())
	messages, err := database.DirectMessages(alice.uuid, bob.uuid, 10)
	if err != nil || len(messages) != 1 {
		t.Fatalf("message was not persisted: %v %#v", err, messages)
	}
	if len(bob.writeCh) < 2 {
		t.Fatalf("Bob did not receive message push: queued=%d", len(bob.writeCh))
	}
}
