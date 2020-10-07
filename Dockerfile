FROM golang:alpine AS builder
WORKDIR /go/src/github.com/wzshiming/socks4/
COPY . .
ENV CGO_ENABLED=0
RUN go install ./cmd/socks4

FROM alpine
EXPOSE 1080
COPY --from=builder /go/bin/socks4 /usr/local/bin/
ENTRYPOINT [ "/usr/local/bin/socks4" ]
