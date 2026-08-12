package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"lcserver/db"
	"lcserver/server"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

//go:embed web/admin.html
var webFS embed.FS

var hub *server.Hub
var database *db.Database

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dbPath := flag.String("db", "", "sqlite database path (or LCSERVER_DB_PATH)")
	flag.Parse()

	log.SetFlags(log.LstdFlags)
	resolvedDBPath, err := resolveDBPath(*dbPath)
	if err != nil {
		log.Fatalf("CRITICAL db path: %v", err)
	}
	log.Printf("AUTH storage db=%s", resolvedDBPath)

	database, err = db.Open(resolvedDBPath)
	if err != nil {
		log.Fatalf("CRITICAL db: %v", err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			log.Printf("ERROR db close: %v", err)
		}
	}()

	hub = server.NewHub(database)
	go hub.Run()

	mux := http.NewServeMux()
	mux.HandleFunc("/", serveAdmin)
	mux.HandleFunc("/ws", hub.WebSocketHandler)
	mux.HandleFunc("/api/players", apiOnlinePlayers)
	mux.HandleFunc("/api/players/all", apiAllProfiles)
	mux.HandleFunc("/api/profile/", apiProfile)
	mux.HandleFunc("/health", apiHealth)

	httpServer := &http.Server{Addr: *addr, Handler: mux}
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Printf("ERROR server shutdown: %v", err)
		}
	}()

	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("CRITICAL server: %v", err)
	}
}

func resolveDBPath(flagPath string) (string, error) {
	path := flagPath
	if path == "" {
		path = os.Getenv("LCSERVER_DB_PATH")
	}
	if path == "" {
		path = "lcserver.db"
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return "", err
	}
	return abs, nil
}

func apiOnlinePlayers(w http.ResponseWriter, r *http.Request) {
	pl := hub.Players()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count": len(pl), "players": pl,
	})
}

func apiAllProfiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := database.AllProfiles()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profiles)
}

func apiProfile(w http.ResponseWriter, r *http.Request) {
	uuid := r.URL.Path[len("/api/profile/"):]
	if uuid == "" {
		http.Error(w, "missing uuid", 400)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == "OPTIONS" {
		return
	}
	if r.Method == "GET" {
		profile, err := database.GetProfile(uuid)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if profile == nil {
			http.Error(w, "not found", 404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(profile)
		return
	}
	if r.Method == "POST" || r.Method == "PUT" {
		var updates map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			http.Error(w, "bad json: "+err.Error(), 400)
			return
		}
		profile, err := database.GetProfile(uuid)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if profile == nil {
			profile = &db.UserProfile{UUID: uuid}
			if v, ok := updates["username"]; ok {
				if s, ok := v.(string); ok {
					profile.Username = s
				}
			}
			profile.Rank = "ADMIN"
			profile.LogoAlwaysShow = true
			profile.ArtistTools = true
			profile.TesterTools = true
			profile.VisHatsOverHelmet = true
			profile.VisHatsOverSkin = true
			profile.VisOverChestplate = true
			profile.VisOverLeggings = true
			profile.VisOverBoots = true
			for i := int32(1); i <= 500; i++ {
				profile.OwnedCosmetics = append(profile.OwnedCosmetics, i)
			}
			for i := int32(1); i <= 200; i++ {
				profile.OwnedEmotes = append(profile.OwnedEmotes, i)
			}
			for i := int32(1); i <= 100; i++ {
				profile.OwnedBadges = append(profile.OwnedBadges, i)
				profile.OwnedSprays = append(profile.OwnedSprays, i)
			}
		}
		if v, ok := updates["username"]; ok {
			if s, ok := v.(string); ok {
				profile.Username = s
			}
		}
		if v, ok := updates["rank"]; ok {
			if s, ok := v.(string); ok {
				profile.Rank = s
			}
		}
		if v, ok := updates["plus_color"]; ok {
			profile.PlusColor = uint32(toFloat64(v))
		}
		if v, ok := updates["logo_color"]; ok {
			profile.LogoColor = uint32(toFloat64(v))
		}
		if v, ok := updates["equipped_badge"]; ok {
			profile.EquippedBadge = int32(toFloat64(v))
		}
		if v, ok := updates["equipped_cosmetics"]; ok {
			if arr, ok := v.([]interface{}); ok {
				profile.EquippedCosmetics = toInt32Slice(arr)
			}
		}
		if v, ok := updates["equipped_emotes"]; ok {
			if arr, ok := v.([]interface{}); ok {
				profile.EquippedEmotes = toInt32Slice(arr)
			}
		}
		if v, ok := updates["equipped_sprays"]; ok {
			if arr, ok := v.([]interface{}); ok {
				profile.EquippedSprays = toInt32Slice(arr)
			}
		}
		if err := database.UpsertProfile(profile); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(profile)
		return
	}
	http.Error(w, "method not allowed", 405)
}

func apiHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"ok","players":%d}`, hub.PlayerCount())
}

func serveAdmin(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(webFS, "web/admin.html")
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func toFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}

func toInt32Slice(arr []interface{}) []int32 {
	out := make([]int32, 0, len(arr))
	for _, v := range arr {
		out = append(out, int32(toFloat64(v)))
	}
	return out
}
