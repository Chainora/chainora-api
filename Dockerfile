FROM golang:1.24-alpine AS builder
WORKDIR /app

RUN apk add --no-cache git ca-certificates

COPY . .
RUN go work sync && cd src/rest && go build -o /out/chainora-rest .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /out/chainora-rest /usr/local/bin/chainora-rest
EXPOSE 8080
CMD ["chainora-rest"]
