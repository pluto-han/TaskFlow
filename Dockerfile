# syntax=docker/dockerfile:1
FROM golang:1.27.3-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS build
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
