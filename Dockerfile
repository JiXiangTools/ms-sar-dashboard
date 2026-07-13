FROM dockerhub.seobot.cc/library/golang:1.26 AS builder

ARG COMMIT_ID=dev
ARG BUILD_TIME=unknown
ARG GOPROXY=https://goproxy.cn,direct
ARG HTTP_PROXY=
ARG HTTPS_PROXY=
ARG ALL_PROXY=
ARG NO_PROXY=
ARG http_proxy=
ARG https_proxy=
ARG all_proxy=
ARG no_proxy=

ENV GOPROXY=${GOPROXY}
ENV HTTP_PROXY=${HTTP_PROXY}
ENV HTTPS_PROXY=${HTTPS_PROXY}
ENV ALL_PROXY=${ALL_PROXY}
ENV NO_PROXY=${NO_PROXY}
ENV http_proxy=${http_proxy}
ENV https_proxy=${https_proxy}
ENV all_proxy=${all_proxy}
ENV no_proxy=${no_proxy}

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -tags nomsgpack -trimpath \
    -ldflags "-s -w -X github.com/JiXiangTools/ms-sar-dashboard/internal/version.Commit=${COMMIT_ID} -X github.com/JiXiangTools/ms-sar-dashboard/internal/version.BuildTime=${BUILD_TIME}" \
    -o /out/ms-sar-dashboard ./cmd/ms-sar-dashboard

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "-s -w" \
    -o /out/ms-sar-dbtool ./cmd/ms-sar-dbtool

FROM dockerhub.seobot.cc/library/alpine:3.21

WORKDIR /app

RUN addgroup -S app && adduser -S -G app app

COPY --from=builder /out/ms-sar-dashboard /app/ms-sar-dashboard
COPY --from=builder /out/ms-sar-dbtool /app/ms-sar-dbtool
COPY admin/docker-entrypoint.sh /app/docker-entrypoint.sh
COPY configs /app/configs
COPY upgrade/sql /app/upgrade/sql

RUN chmod +x /app/docker-entrypoint.sh && chown -R app:app /app

EXPOSE 8081

USER app

ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["-config", "/app/configs/prod.yaml"]
