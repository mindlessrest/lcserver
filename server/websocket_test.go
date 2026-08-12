package server

import (
	"lcserver/db"
	"lcserver/proto"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func testIdentity(username string) []byte {
	p := proto.NewEncoder()
	proto.EncodeUUID(1, "12345678-1234-5678-9abc-def012345678", p)
	p.String(2, username)
	return p.Bytes()
}

func TestAuthAndGameHandshake(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	hub := NewHub(database)
	go hub.Run()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.WebSocketHandler)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	header := http.Header{"Accept": []string{"application/x-protobuf"}}
	auth, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		t.Fatal(err)
	}
	hello := proto.NewEncoder()
	hello.Message(1, testIdentity("tester"))
	outer := proto.NewEncoder()
	outer.Message(1, hello.Bytes())
	if err := auth.WriteMessage(websocket.BinaryMessage, outer.Bytes()); err != nil {
		t.Fatal(err)
	}
	_, authReply, err := auth.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	auth.Close()
	if len(authReply) < 4 {
		t.Fatalf("short auth reply: %x", authReply)
	}

	game, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer game.Close()
	identity := proto.NewEncoder()
	identity.Message(1, testIdentity("tester"))
	identity.String(3, "lcserver:test")
	handshake := proto.NewEncoder()
	handshake.Message(1, identity.Bytes())
	if err := game.WriteMessage(websocket.BinaryMessage, handshake.Bytes()); err != nil {
		t.Fatal(err)
	}
	rpc := proto.NewEncoder()
	rpc.WriteBytes(1, []byte("1"))
	rpc.String(2, "lunarclient.websocket.heartbeat.v1.HeartbeatService")
	rpc.String(3, "Heartbeat")
	if err := game.WriteMessage(websocket.BinaryMessage, rpc.Bytes()); err != nil {
		t.Fatal(err)
	}
	game.SetReadDeadline(time.Now().Add(2 * time.Second))
	mt, response, err := game.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if mt != websocket.BinaryMessage || len(response) == 0 || response[0] != 0x0a {
		t.Fatalf("bad rpc response type=%d bytes=%x", mt, response)
	}
	if hub.PlayerCount() != 1 {
		t.Fatalf("presence registration was not synchronous: players=%d", hub.PlayerCount())
	}
}
