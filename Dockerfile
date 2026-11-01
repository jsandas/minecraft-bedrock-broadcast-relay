FROM golang:1.27.3@sha256:2920bb124f1a2e12a1d39f0cab756ae0068441ee1da7655dec7f5e374944095b AS builder

WORKDIR /go/src

COPY . .

RUN go build -ldflags="-s -w" -o /nethernet_relay ./cmd/nethernet_relay

FROM gcr.io/distroless/base-debian13:nonroot@sha256:f9518b74815721f537a7458cc693b4406208c04fce23bbffab511516117f34fd

COPY --from=builder /nethernet_relay /nethernet_relay

EXPOSE 7551/udp

ENTRYPOINT ["/nethernet_relay"]