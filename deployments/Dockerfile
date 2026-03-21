# deployments/Dockerfile builds the etcdisc phase 1 server image for local docker-compose usage.
FROM golang:1.25 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/etcdisc-server ./cmd/etcdisc-server

FROM gcr.io/distroless/base-debian12
COPY --from=build /out/etcdisc-server /usr/local/bin/etcdisc-server
COPY deployments/etcdisc.yaml /etc/etcdisc.yaml

EXPOSE 8080 9090

ENTRYPOINT ["/usr/local/bin/etcdisc-server"]
