# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
ARG BUILD_CHANNEL=ghcr
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.buildChannel=${BUILD_CHANNEL}" \
    -o /out/deepseek-search-fetch-mcp ./cmd/deepseek-search-fetch-mcp

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/deepseek-search-fetch-mcp /deepseek-search-fetch-mcp

# 容器内需要监听所有网卡，宿主机通过端口映射访问。
ENV DEEPSEEK_SEARCH_FETCH_HOST=0.0.0.0
EXPOSE 18081

USER nonroot:nonroot
ENTRYPOINT ["/deepseek-search-fetch-mcp"]
