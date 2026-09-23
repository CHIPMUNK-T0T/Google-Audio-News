FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/uploader ./cmd/uploader

FROM alpine:3.22
RUN apk add --no-cache ffmpeg ca-certificates
WORKDIR /app
COPY --from=build /out/uploader /app/uploader
COPY assets /app/assets
ENV BACKGROUND_IMAGE=/app/assets/background.png
USER 65534:65534
ENTRYPOINT ["/app/uploader"]
