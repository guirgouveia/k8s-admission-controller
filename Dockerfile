FROM golang:latest AS builder

WORKDIR /app
COPY . .

ARG BUILD_TIME
ENV BUILD_TIME=${BUILD_TIME:-unknown}

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X main.buildTime=${BUILD_TIME}" -o admission-controller

FROM alpine:latest
RUN apk add --no-cache ca-certificates

WORKDIR /
COPY --from=builder /app/admission-controller .

ENV LOG_LEVEL=info

ENTRYPOINT ["/admission-controller"]