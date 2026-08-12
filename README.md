# lcserver

Local protocol-compatible WebSocket backend for the decompiled Lunar Client build in `lunarsrc`.

## Run

```powershell
$env:LCSERVER_DB_PATH = "C:\lcserver-data\lcserver.db"
go run . -addr :8080
```

Always set `LCSERVER_DB_PATH` to an absolute persistent location in PM2/production. The `-db` flag overrides the environment variable. If neither is set, the server uses an absolute path to `lcserver.db` in its current working directory.

The Java redirect agent targets `wss://api.mindless.rest/ws`. The same endpoint accepts both connections used by the client:

- Authenticator socket: identified by `Accept: application/x-protobuf`; receives authenticator `Hello` and returns `AuthSuccess.jwt`.
- Game socket: its first binary frame is the raw `handshake.v1.Handshake`; later frames are `protocol.v1.ServerboundWebSocketMessage` RPC envelopes.

Only binary WebSocket messages are accepted. RFC 6455 ping/pong and close handling are provided by Gorilla WebSocket. Read, handshake, RPC dispatch, response size, disconnect, and unsupported-service events are logged.

## Implemented services

- Authentication and durable player sessions
- Cosmetics v1/v2, emotes, badges, sprays, complete outfit lists, and selected outfit trees
- Cosmetic subscription snapshots that survive entity refreshes, perspective changes, and emotes
- Friends, requests, pins, real-time online/away/busy presence, and privacy acknowledgements
- Friend direct messages with persisted history and Lunar-compatible conversation references
- Settings updates persisted by player and client/launcher scope
- Heartbeat/session liveness
- Hosted-world compatibility with a bounded multiplayer refresh interval (prevents Lunar's main-menu 0ms RPC loop)
- Party compatibility: this client generation has no generated `PartyService`; its group/party surface is `ConversationService`. Login/list calls return valid empty protobuf results and mutations are acknowledged. `PartyService` is also accepted for adjacent builds.

SQLite migrations create `users`, `user_settings`, `friendships`, `parties`, `party_members`, `cosmetic_outfits`, and `cosmetic_outfit_trees`. Existing databases migrate in place. Friend changes are committed atomically, and WAL/full-sync mode plus graceful shutdown protects saved state during service restarts.

## Verify

```powershell
go test ./...
go vet ./...
go build ./...
```

The integration test performs both WebSocket upgrades, authenticator hello/auth-success exchange, raw game handshake, and a framed heartbeat RPC round trip.
