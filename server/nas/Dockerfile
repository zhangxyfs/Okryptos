# okserver 镜像（多架构：linux/amd64 + linux/arm64）
# 构建（仓库根目录）：docker buildx build --platform linux/amd64,linux/arm64 -f server/nas/Dockerfile -t z7dream/okryptos-okserver:<版本> --push .
FROM golang:1.25-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/okserver ./cmd/okserver
COPY internal ./internal
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X okryptos/internal/version.Version=${VERSION}" -o /out/okserver ./cmd/okserver

FROM alpine:3.20
RUN adduser -D okserver
COPY --from=build /out/okserver /out/okserver
# named volume/匿名卷场景 /data 属 root，须先授权给运行用户，否则 OpenStore 打不开库必崩
RUN mkdir -p /data && chown okserver:okserver /data
USER okserver
VOLUME /data
EXPOSE 3100
ENTRYPOINT ["/out/okserver"]
