FROM golang:latest AS builder

WORKDIR /app
COPY . .

ARG BUILD_TIME
ENV BUILD_TIME=${BUILD_TIME:-unknown}

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X main.buildTime=${BUILD_TIME}" -o admission-controller

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot

ENV LOG_LEVEL=info

WORKDIR /

COPY --from=builder /app/admission-controller .

USER 65532:65532

ENTRYPOINT ["/admission-controller"]