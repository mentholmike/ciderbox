FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26-bookworm AS runner-build

ARG TARGETOS=linux
ARG TARGETARCH
WORKDIR /src
COPY cloudflare-container-runner/go.mod cloudflare-container-runner/main.go ./
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/crabbox-container-runner .

FROM docker.io/library/debian:bookworm-slim

RUN apt-get update \
  && apt-get install -y --no-install-recommends bash ca-certificates curl git jq ripgrep tar \
  && rm -rf /var/lib/apt/lists/*

COPY --from=runner-build /out/crabbox-container-runner /usr/local/bin/crabbox-container-runner

WORKDIR /workspace
EXPOSE 8787
ENTRYPOINT ["/usr/local/bin/crabbox-container-runner"]
