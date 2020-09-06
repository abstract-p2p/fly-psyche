FROM golang as base

WORKDIR /app

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

COPY go.mod .
# COPY go.sum .

RUN go mod download

COPY . .

# it will take the flags from the environment
RUN go build -o app

### Certs
FROM alpine:latest as certs
RUN apk --update add ca-certificates

### App
# FROM scratch as app
FROM alpine:latest as app
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=base app /
EXPOSE 8080
CMD ["/app"]
