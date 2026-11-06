# syntax=docker/dockerfile:1
# Imagen de mcp-datos-gob-cl. Por defecto habla MCP por stdio:
#   docker run -i --rm ghcr.io/marcovaldiviesob/mcp-datos-gob-cl
# Modo HTTP (streamable HTTP en /mcp, /health para chequeos):
#   docker run --rm -p 8080:8080 ghcr.io/marcovaldiviesob/mcp-datos-gob-cl --http :8080

FROM --platform=$BUILDPLATFORM golang:1.26 AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/mcp-datos-gob-cl .

# distroless/static trae certificados CA y tzdata; corre como usuario sin privilegios.
FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION=dev
LABEL org.opencontainers.image.title="mcp-datos-gob-cl" \
      org.opencontainers.image.description="Servidor MCP con datos públicos de Chile en vivo" \
      org.opencontainers.image.source="https://github.com/MarcoValdiviesoB/mcp-datos-gob-cl" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}" \
      io.modelcontextprotocol.server.name="io.github.marcovaldiviesob/mcp-datos-gob-cl"
COPY --from=build /out/mcp-datos-gob-cl /mcp-datos-gob-cl
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/mcp-datos-gob-cl"]
