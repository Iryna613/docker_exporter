FROM golang:1.22-alpine AS build
WORKDIR /src
COPY . .
RUN go build -o docker-stats-exporter main.go

FROM alpine:3.19
RUN adduser -D exporter
USER exporter
WORKDIR /app
COPY --from=build /src/docker-stats-exporter .
EXPOSE 9273
ENTRYPOINT ["./docker-stats-exporter"]
