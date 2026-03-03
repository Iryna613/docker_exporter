FROM golang:1.26-alpine AS build
WORKDIR /src
COPY . .
RUN go build -o docker-exporter main.go

FROM alpine:3.19
RUN adduser -D exporter
USER exporter
WORKDIR /app
COPY --from=build /src/docker-exporter .
EXPOSE 9273
ENTRYPOINT ["./docker-exporter"]
