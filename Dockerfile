# syntax=docker/dockerfile:1
FROM golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/api ./cmd/api && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/dbsetup ./cmd/dbsetup

FROM scratch AS api
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/api /api
USER 65532:65532
EXPOSE 9090
ENTRYPOINT ["/api"]

FROM scratch AS migrate
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/dbsetup /dbsetup
COPY migrations /migrations
USER 65532:65532
ENTRYPOINT ["/dbsetup"]
