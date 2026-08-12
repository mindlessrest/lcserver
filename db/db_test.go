package db

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestFriendsAndCosmeticsSurviveDatabaseReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lcserver.db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	alice := &UserProfile{
		UUID:              "11111111-1111-4111-8111-111111111111",
		Username:          "Alice",
		EquippedCosmetics: []int32{12, 34, 56},
		EquippedBadge:     7,
	}
	bob := &UserProfile{UUID: "22222222-2222-4222-8222-222222222222", Username: "Bob"}
	if err := first.UpsertProfile(alice); err != nil {
		t.Fatal(err)
	}
	if err := first.UpsertProfile(bob); err != nil {
		t.Fatal(err)
	}
	if err := first.SetFriendship(alice.UUID, bob.UUID, "FRIEND", "FRIEND"); err != nil {
		t.Fatal(err)
	}
	outfit, tree := []byte{0x0a, 0x01, 0x01}, []byte{0x12, 0x01, 0x02}
	if err := first.SaveCosmeticOutfit(alice.UUID, "outfit-1", outfit); err != nil {
		t.Fatal(err)
	}
	if err := first.SaveCosmeticOutfitTree(alice.UUID, tree); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	profile, err := second.GetProfile(alice.UUID)
	if err != nil || profile == nil {
		t.Fatalf("profile not restored: %v %#v", err, profile)
	}
	if len(profile.EquippedCosmetics) != 3 || profile.EquippedCosmetics[1] != 34 || profile.EquippedBadge != 7 {
		t.Fatalf("cosmetics not restored: %#v", profile)
	}
	entries, err := second.FriendEntries(alice.UUID)
	if err != nil || len(entries) != 1 || entries[0].FriendUUID != bob.UUID || entries[0].State != "FRIEND" {
		t.Fatalf("friendship not restored: %v %#v", err, entries)
	}
	outfits, err := second.CosmeticOutfits(alice.UUID)
	if err != nil || len(outfits) != 1 || !bytes.Equal(outfits[0].Data, outfit) {
		t.Fatalf("outfit not restored: %v %#v", err, outfits)
	}
	restoredTree, err := second.CosmeticOutfitTree(alice.UUID)
	if err != nil || !bytes.Equal(restoredTree, tree) {
		t.Fatalf("outfit tree not restored: %v %x", err, restoredTree)
	}
}
