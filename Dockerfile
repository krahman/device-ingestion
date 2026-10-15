FROM golang:1.27-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
COPY vendor ./vendor
COPY app ./app
COPY cmd ./cmd
COPY internal ./internal
COPY proto ./proto

RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -trimpath -o /out/server ./cmd/server \
    && CGO_ENABLED=0 GOOS=linux go build -mod=vendor -trimpath -o /out/migrate ./cmd/migrate \
    && CGO_ENABLED=0 GOOS=linux go build -mod=vendor -trimpath -o /out/kafka-init ./cmd/kafka-init \
    && CGO_ENABLED=0 GOOS=linux go build -mod=vendor -trimpath -o /out/staging-smoke ./cmd/staging-smoke

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/server /usr/local/bin/server
COPY --from=build /out/migrate /usr/local/bin/migrate
COPY --from=build /out/kafka-init /usr/local/bin/kafka-init
COPY --from=build /out/staging-smoke /usr/local/bin/staging-smoke

USER 10001:10001
EXPOSE 8080 8081 50051
ENTRYPOINT ["/usr/local/bin/server"]
