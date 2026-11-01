FROM golang:1.27.3-alpine AS build
WORKDIR /src
ENV GOMAXPROCS=2
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go build -p 1 -trimpath -ldflags="-s -w" -o /out/walletledger ./cmd/server

FROM alpine:3.23 AS runtime
RUN addgroup -g 10001 walletledger && adduser -D -H -u 10001 -G walletledger walletledger
WORKDIR /app
COPY --from=build --chown=10001:10001 /out/walletledger /app/walletledger
USER 10001:10001
ENV PORT=8080 WALLETLEDGER_HOST=0.0.0.0
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=4s --start-period=5s --retries=5 CMD ["/app/walletledger", "health"]
ENTRYPOINT ["/app/walletledger"]
CMD ["serve"]
