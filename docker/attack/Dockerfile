FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod flooder.go ./
RUN go mod tidy
RUN CGO_ENABLED=0 go build -o /flooder ./flooder.go

FROM debian:bookworm-slim
COPY --from=build /flooder /usr/local/bin/flooder
ENTRYPOINT ["/usr/local/bin/flooder"]
