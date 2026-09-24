# syntax=docker/dockerfile:1

# The bundle embeds into the binary and has no architecture of its own, so it
# is built once for the builder's platform. Built per target, the arm alpine
# builds die in rolldown, whose 32-bit ARM musl binding is a 0.0.0 stub.
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# CGO is off, so every target binary cross-compiles natively. GOARM is empty
# for the 64-bit targets, which the go tool accepts.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
ARG VERSION=dev
ARG COMMIT=dev
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN mkdir /empty
COPY --from=web /src/web/dist web/dist
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /aegis ./cmd/aegis

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /aegis /aegis
COPY --from=build --chown=65534:65534 /empty /var/lib/aegis
USER 65534:65534
EXPOSE 53/udp 53/tcp 8080/tcp
VOLUME ["/var/lib/aegis"]
ENTRYPOINT ["/aegis"]
CMD ["serve", "--db", "/var/lib/aegis/aegis.db", "--dns-address", "0.0.0.0:53", "--api-address", "0.0.0.0:8080"]
