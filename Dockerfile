FROM golang:1.24-alpine AS builder
WORKDIR /app

RUN apk add --no-cache git ca-certificates

COPY . .
RUN go work sync && cd src/rest && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/chainora-rest .

FROM alpine:3.20
RUN addgroup -S app && adduser -S -G app app && apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /out/chainora-rest /usr/local/bin/chainora-rest
USER app:app
EXPOSE 8080
CMD ["chainora-rest"]
