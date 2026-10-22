# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w -X github.com/sxyr/agent-utils-cn/internal/version.Version=${VERSION}" \
    -o /out/agent-utils-cn ./cmd/agent-utils-cn

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/agent-utils-cn /agent-utils-cn

# 容器内需要监听所有网卡，宿主机通过端口映射访问。
ENV AGENT_UTILS_HOST=0.0.0.0
EXPOSE 18081

USER nonroot:nonroot
ENTRYPOINT ["/agent-utils-cn"]
