# build stage
FROM golang:alpine AS builder
RUN apk add --no-cache git
WORKDIR /go/src/app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /go/bin/app ./cmd/apis  # <-- fixed line

# final stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /go/bin/app /app
ENTRYPOINT ["/app"]
LABEL Name=platformapi Version=0.0.1
EXPOSE 3000
