# Chainora API - Huong dan setup backend tu dau den cuoi

Tai lieu nay huong dan setup backend local cho flow QR Login, bao gom:
- Chay Docker database
- Chay backend
- Test API dau tien (tao QR session)
- Debug va logging cho HTTP + WebSocket
- Cac terminal command thuong dung

## 1. Dieu kien tien quyet

- Go 1.24+
- Docker + Docker Compose
- GNU Make
- (Tuy chon) Abigen

Kiem tra nhanh:

    go version
    docker --version
    docker compose version
    make --version

Neu can cai abigen:

    make abigen

## 2. Setup cau hinh migration local

Tu root repo chainora-api:

    cp ./src/migration/config/local.yml.example ./src/migration/config/local.yml

File local.yml dung de nap DATABASE_URL va MIGRATIONS_DIR cho migration runner.

## 3. Chay database bang Docker

Khoi dong PostgreSQL:

    make up

Kiem tra container dang chay:

    docker compose ps

Xem log database:

    docker compose logs -f postgres

## 4. Cai dependencies va migrate DB

Chay tidy toan bo modules:

    make tidy

Chay migration:

    make migrate

Neu thanh cong, runner se in:
- Applying migration: ... Success
- Migrations completed successfully.

## 5. Chay backend

Chay API local:

    make rest

Mac dinh backend nghe tai:

    http://localhost:8080

Health check:

    curl -i http://localhost:8080/healthz

## 6. Test API dau tien: tao QR session

API tao session QR:

    curl -s http://localhost:8080/v1/auth/session | jq

Response ky vong:

    {
      "sessionId": "...",
      "nonce": "..."
    }

sessionId va nonce nay dung de render QR tren DApp.

## 7. Test WebSocket cho QR flow

Sau khi co sessionId, mo WebSocket den endpoint:

    ws://localhost:8080/v1/auth/ws/<sessionId>

Ban co the test bang 1 trong 2 tool:

Neu dung websocat:

    websocat ws://localhost:8080/v1/auth/ws/<sessionId>

Neu dung wscat:

    npx wscat -c ws://localhost:8080/v1/auth/ws/<sessionId>

Khi mobile verify chu ky thanh cong qua POST /v1/auth/verify, backend se push message co token JWT qua socket.

## 8. Debug va logging (backend + socket)

### 8.1 Logging HTTP backend

REST da co request logger middleware, log mau:

    GET /v1/auth/session -> 200 (1.2ms)

Cach debug nhanh:
- Chay backend foreground bang make rest de nhin log realtime trong terminal.
- Dung grep loc endpoint can xem.

Vi du:

    make rest | grep "/v1/auth"

### 8.2 Logging WebSocket

WebSocket hub nam trong AuthController.

De debug socket de hon, theo doi:
- Luc client connect/disconnect
- sessionId duoc register/unregister
- Luc broadcast verify event

Neu chay bang docker:

    docker compose logs -f api

Neu chay local:

    make rest

### 8.3 Checklist khi socket khong nhan du lieu

1. Da goi GET /v1/auth/session de tao dung sessionId chua
2. DApp co ket noi dung ws://.../v1/auth/ws/:sessionId khong
3. Mobile POST /v1/auth/verify dung sessionId va signature hop le khong
4. Backend co log VerifySignature 200 khong
5. Client WebSocket co bi dong som do timeout/network khong

## 9. Cac command terminal thuong dung

### Chay va quan ly service

    make up
    make rest
    make migrate
    make down

### Tao migration moi

    make migration
    make migration MIGRATION_NAME=create_auth_sessions

### Theo doi logs

    docker compose logs -f postgres
    docker compose logs -f api

### Kiem tra nhanh API

    curl -i http://localhost:8080/healthz
    curl -s http://localhost:8080/v1/auth/session | jq

### Kiem tra code style va compile

    find src -name '*.go' -print0 | xargs -0 gofmt -w
    cd src/core && go test ./...
    cd ../adapter && go test ./...
    cd ../migration && go test ./...
    cd ../rest && go test ./...

## 10. Luong test end-to-end nhanh nhat

1. make up
2. make tidy
3. make migrate
4. Terminal A: make rest
5. Terminal B: goi GET /v1/auth/session lay sessionId
6. Terminal C: mo ws /v1/auth/ws/:sessionId
7. Mobile goi POST /v1/auth/verify
8. Kiem tra terminal C da nhan payload verified + token JWT
