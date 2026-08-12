package proto

import "testing"

func TestRPCEnvelopeRoundTrip(t *testing.T) {
	e := NewEncoder()
	e.WriteBytes(1, []byte("42"))
	e.String(2, "lunarclient.websocket.heartbeat.v1.HeartbeatService")
	e.String(3, "Heartbeat")
	e.WriteBytes(4, []byte{8, 1})
	m, err := ParseServerboundMessage(e.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if string(m.RequestID) != "42" || m.Method != "Heartbeat" || len(m.Input) != 2 {
		t.Fatalf("bad decode: %#v", m)
	}
	if got := EncodeClientboundMessage(m.RequestID, nil); len(got) == 0 || got[0] != 0x0a {
		t.Fatalf("response is not rpc_response: %x", got)
	}
}

func TestAuthSuccessContainsJWT(t *testing.T) {
	d := NewDecoder(EncodeAuthSuccess("lcserver:test"))
	field, wt, err := d.ReadTag()
	if err != nil || field != 2 || wt != LengthDelimited {
		t.Fatalf("bad wrapper: %d %d %v", field, wt, err)
	}
	raw, _ := d.ReadMessage()
	inner := NewDecoder(raw)
	field, _, _ = inner.ReadTag()
	jwt, _ := inner.ReadString()
	if field != 1 || jwt != "lcserver:test" {
		t.Fatalf("bad auth success jwt: field=%d jwt=%q", field, jwt)
	}
}

func TestCosmeticV2LoginHasResolvableDefaultOutfit(t *testing.T) {
	p := DefaultPlayerCosmetics()
	data := EncodeCosmeticV2LoginResponse(p)
	d := NewDecoder(data)
	var outfit, tree []byte
	seen := map[uint32]bool{}
	for d.Remaining() > 0 {
		field, wt, err := d.ReadTag()
		if err != nil {
			t.Fatal(err)
		}
		seen[field] = true
		if wt == LengthDelimited {
			raw, err := d.ReadMessage()
			if err != nil {
				t.Fatal(err)
			}
			if field == 10 {
				outfit = raw
			}
			if field == 11 {
				tree = raw
			}
		} else if err := d.Skip(wt); err != nil {
			t.Fatal(err)
		}
	}
	for _, field := range []uint32{1, 2, 3, 4, 7, 8, 10, 11, 12, 13, 14} {
		if !seen[field] {
			t.Fatalf("missing v2 field %d", field)
		}
	}
	if seen[5] || seen[6] {
		t.Fatal("fabricated free/owned cosmetic IDs were serialized")
	}
	readUUID := func(raw []byte, wanted uint32) []byte {
		x := NewDecoder(raw)
		for x.Remaining() > 0 {
			f, w, _ := x.ReadTag()
			if f == wanted {
				v, _ := x.ReadMessage()
				return v
			}
			x.Skip(w)
		}
		return nil
	}
	outfitID, treeID := readUUID(outfit, 1), readUUID(tree, 2)
	if len(outfitID) == 0 || string(outfitID) != string(treeID) {
		t.Fatalf("outfit tree does not resolve: outfit=%x tree=%x", outfitID, treeID)
	}
}

func TestHostedWorldLoginHasNonZeroRefreshInterval(t *testing.T) {
	d := NewDecoder(EncodeHostedWorldLoginResponse(30))
	var refresh int32
	for d.Remaining() > 0 {
		field, wt, err := d.ReadTag()
		if err != nil {
			t.Fatal(err)
		}
		if field == 5 {
			refresh, err = d.ReadInt32()
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := d.Skip(wt); err != nil {
			t.Fatal(err)
		}
	}
	if refresh < 5 {
		t.Fatalf("unsafe multiplayer refresh interval: %d", refresh)
	}
}

func TestPlayerCosmeticsPushV2UsesClientFieldNumbers(t *testing.T) {
	p := DefaultPlayerCosmetics()
	p.EquippedCosmetics = []int32{12, 34}
	p.EquippedBadge = 7
	d := NewDecoder(EncodePlayerCosmeticsPushV2("12345678-1234-5678-9abc-def012345678", p))
	counts := map[uint32]int{}
	for d.Remaining() > 0 {
		field, wt, err := d.ReadTag()
		if err != nil {
			t.Fatal(err)
		}
		counts[field]++
		if err := d.Skip(wt); err != nil {
			t.Fatal(err)
		}
	}
	for _, field := range []uint32{1, 2, 3, 4, 6, 7, 8} {
		if counts[field] == 0 {
			t.Fatalf("missing PlayerCosmeticsPushV2 field %d", field)
		}
	}
	if counts[6] != 2 {
		t.Fatalf("default_cosmetics count=%d", counts[6])
	}
	if counts[5] != 0 {
		t.Fatal("conditional_cosmetics was unexpectedly populated")
	}
}

func TestConversationPushShape(t *testing.T) {
	contents := NewEncoder()
	contents.String(1, "hello")
	message := EncodeConversationMessage("12345678-1234-4678-9abc-def012345678", 123, "12345678-1234-5678-9abc-def012345678", "Alice", contents.Bytes())
	d := NewDecoder(EncodeConversationMessagePush("87654321-4321-5678-9abc-def012345678", message))
	for expected := uint32(1); expected <= 2; expected++ {
		field, wt, err := d.ReadTag()
		if err != nil || field != expected || wt != LengthDelimited {
			t.Fatalf("bad push field: %d/%d %v", field, wt, err)
		}
		if _, err := d.ReadMessage(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestConversationReferenceUsesRequiredConversationOneof(t *testing.T) {
	d := NewDecoder(EncodeConversationReference("12345678-1234-5678-9abc-def012345678"))
	field, wt, err := d.ReadTag()
	if err != nil || field != 2 || wt != LengthDelimited {
		t.Fatalf("conversation reference used wrong oneof: field=%d wire=%d err=%v", field, wt, err)
	}
	raw, err := d.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("conversation UUID was empty")
	}
}

func TestLegacyPlayerCosmeticsPushShape(t *testing.T) {
	p := DefaultPlayerCosmetics()
	p.EquippedCosmetics = []int32{12, 34}
	p.EquippedBadge = 7
	d := NewDecoder(EncodePlayerCosmeticsPush("12345678-1234-5678-9abc-def012345678", p))
	seen := map[uint32]bool{}
	for d.Remaining() > 0 {
		field, wt, err := d.ReadTag()
		if err != nil {
			t.Fatal(err)
		}
		seen[field] = true
		if err := d.Skip(wt); err != nil {
			t.Fatal(err)
		}
	}
	for _, field := range []uint32{1, 2, 3, 4, 5, 6} {
		if !seen[field] {
			t.Fatalf("missing PlayerCosmeticsPush field %d", field)
		}
	}
}

func TestUUIDUsesFixed64WireFields(t *testing.T) {
	e := NewEncoder()
	EncodeUUID(1, "12345678-1234-5678-9abc-def012345678", e)
	d := NewDecoder(e.Bytes())
	field, wt, err := d.ReadTag()
	if err != nil || field != 1 || wt != LengthDelimited {
		t.Fatalf("bad UUID wrapper: %d/%d %v", field, wt, err)
	}
	raw, err := d.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	u := NewDecoder(raw)
	field, wt, err = u.ReadTag()
	if err != nil || field != 1 || wt != Fixed64 {
		t.Fatalf("high64 not fixed64: %d/%d %v", field, wt, err)
	}
	high, _ := u.ReadFixed64()
	field, wt, err = u.ReadTag()
	if err != nil || field != 2 || wt != Fixed64 {
		t.Fatalf("low64 not fixed64: %d/%d %v", field, wt, err)
	}
	low, _ := u.ReadFixed64()
	if high != 0x1234567812345678 || low != 0x9abcdef012345678 {
		t.Fatalf("wrong UUID halves: %x %x", high, low)
	}
}

func TestUseEmotePushPreservesMetadata(t *testing.T) {
	d := NewDecoder(EncodeUseEmotePush("12345678-1234-5678-9abc-def012345678", 42, 9, "https://example.test/emote.ogg", 3))
	seen := map[uint32]bool{}
	for d.Remaining() > 0 {
		field, wt, err := d.ReadTag()
		if err != nil {
			t.Fatal(err)
		}
		seen[field] = true
		if err := d.Skip(wt); err != nil {
			t.Fatal(err)
		}
	}
	for field := uint32(1); field <= 5; field++ {
		if !seen[field] {
			t.Fatalf("missing emote push field %d", field)
		}
	}
}
