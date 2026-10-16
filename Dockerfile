FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS build

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src
COPY go.mod go.sum ./
COPY vendor ./vendor
COPY app ./app
COPY cmd ./cmd
COPY internal ./internal
COPY proto ./proto

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -mod=vendor -trimpath -o /out/server ./cmd/server \
    && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -mod=vendor -trimpath -o /out/migrate ./cmd/migrate \
    && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -mod=vendor -trimpath -o /out/kafka-init ./cmd/kafka-init \
    && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -mod=vendor -trimpath -o /out/staging-smoke ./cmd/staging-smoke

FROM alpine:3.22@sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8
ARG OCI_VERSION=dev
ARG OCI_REVISION=unknown

RUN apk add --no-cache ca-certificates=20260909-r0 tzdata=2026e-r0
COPY --from=build /out/server /usr/local/bin/server
COPY --from=build /out/migrate /usr/local/bin/migrate
COPY --from=build /out/kafka-init /usr/local/bin/kafka-init
COPY --from=build /out/staging-smoke /usr/local/bin/staging-smoke

LABEL org.opencontainers.image.title="device-ingestion" \
    org.opencontainers.image.version="${OCI_VERSION}" \
    org.opencontainers.image.revision="${OCI_REVISION}"

USER 10001:10001
EXPOSE 8080 8081 50051
ENTRYPOINT ["/usr/local/bin/server"]
