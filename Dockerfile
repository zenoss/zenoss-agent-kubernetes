FROM golang:1.12-alpine AS build

# Install build toolchain dependencies.
RUN apk --no-cache add git ca-certificates && update-ca-certificates

RUN mkdir /build
WORKDIR /build

# Install go dependencies.
COPY go.mod .
COPY go.sum .
RUN go mod download

# Build binary.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -a --installsuffix cgo -ldflags="-w -s" \
    -o /zenoss-agent-kubernetes

# Build minimal image.
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /zenoss-agent-kubernetes /zenoss-agent-kubernetes
ENTRYPOINT ["/zenoss-agent-kubernetes"]
