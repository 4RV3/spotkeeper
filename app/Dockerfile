FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26 AS build
# CMD selects the binary: spotkeeper (default) or fakeapi (local demo stand-in).
ARG CMD=spotkeeper
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# VERSION (the commit, written by CI) is shown in the UI
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w -X main.version=$(cat VERSION 2>/dev/null || echo dev)" -o /out/app ./cmd/${CMD} && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.source="https://github.com/4RV3/spotkeeper" \
      org.opencontainers.image.title="spotkeeper"
COPY --from=build /out/app /app
# /data must belong to the nonroot user (65532) or SQLite cannot create the database.
COPY --from=build --chown=65532:65532 /out/data /data
ENV DB_PATH=/data/spotkeeper.db
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/app"]
