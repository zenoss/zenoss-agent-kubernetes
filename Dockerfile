# Get latest certificates from Alpine.
FROM alpine:latest AS certs
RUN apk --no-cache --update add ca-certificates

# Build using the appropriate Alpine golang image.
FROM golang:1.12-alpine AS build

# Install build toolchain dependencies.
RUN apk --no-cache add git

RUN mkdir /build
WORKDIR /build

# Install go dependencies.
COPY go.mod .
COPY go.sum .
RUN go mod download

# Build binary.
COPY . .
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN VERSION=$(git describe --all --exact-match `git rev-parse HEAD` | grep tags | sed 's/tags\///') \
    GIT_COMMIT=$(git rev-list -1 HEAD) \
    BUILD_TIME=$(date -u) \
    go build -a --installsuffix cgo \
        -ldflags="-w -s -X main.Version=$VERSION -X main.GitCommit=$GIT_COMMIT -X \"main.BuildTime=$BUILD_TIME\"" \
        -o /zenoss-agent-kubernetes && \
    go test -timeout 30s -cover ./...

# Build minimal image.
FROM scratch
COPY --from=build /zenoss-agent-kubernetes /zenoss-agent-kubernetes
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENTRYPOINT ["/zenoss-agent-kubernetes"]
