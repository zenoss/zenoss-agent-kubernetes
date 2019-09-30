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
RUN export CGO_ENABLED=0 && \
    export GOOS=linux && \
    export GOARCH=amd64 && \
    export VERSION=$(git describe --all --exact-match `git rev-parse HEAD` | grep tags | sed 's/tags\///') && \
    export GIT_COMMIT=$(git rev-list -1 HEAD) && \
    export BUILD_TIME=$(date -u) && \
    echo && \
    echo "Set environment variables:" && \
    echo "  CGO_ENABLED=$CGO_ENABLED" && \
    echo "  GOOS=$GOOS" && \
    echo "  GOARCH=$GOARCH" && \
    echo "  BUILD_TIME=$BUILD_TIME" && \
    echo "  GIT_COMMIT=$GIT_COMMIT" && \
    echo "  VERSION=$VERSION" && \
    echo && \
    echo "Building..." && \
    go build -a --installsuffix cgo \
        -ldflags="-w -s -X main.Version=$VERSION -X main.GitCommit=$GIT_COMMIT -X \"main.BuildTime=$BUILD_TIME\"" \
        -o /zenoss-agent-kubernetes && \
    echo && \
    echo "Running tests..." && \
    go test -timeout 30s -cover ./...

# Build minimal image.
FROM scratch
COPY --from=build /zenoss-agent-kubernetes /zenoss-agent-kubernetes
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENTRYPOINT ["/zenoss-agent-kubernetes"]
