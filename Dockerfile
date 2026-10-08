# Fixed toolchain; the base digests used in validation are recorded in docs/validation.md.
FROM golang:1.22.12-bookworm@sha256:3d699e4d15d0f8f13c9195c0632a16702b8cbdece2955af1c23b37ae5d55a253 AS build
ENV GOMAXPROCS=2 GOFLAGS=-p=1
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -trimpath -ldflags='-s -w' -o /out/dashboard ./cmd/dashboard && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/naive-node ./cmd/naive-node

FROM debian:bookworm-slim@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/* && useradd --uid 10001 --create-home dashboard && \
    mkdir /data && chown dashboard:dashboard /data
COPY --from=build /out/dashboard /usr/local/bin/dashboard
COPY --from=build /out/naive-node /usr/local/bin/naive-node
USER 10001:10001
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/dashboard"]
