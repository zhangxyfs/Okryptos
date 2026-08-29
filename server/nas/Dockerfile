# okserver 镜像（多架构：linux/amd64 + linux/arm64）
# 构建（仓库根目录）：docker buildx build --platform linux/amd64,linux/arm64 -f server/nas/Dockerfile -t openknowledge/okserver:<版本> --push .
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/okserver ./cmd/okserver
COPY internal ./internal
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /out/okserver ./cmd/okserver

FROM alpine:3.20
RUN adduser -D okserver
COPY --from=build /out/okserver /out/okserver
USER okserver
VOLUME /data
EXPOSE 3100
ENTRYPOINT ["/out/okserver"]
