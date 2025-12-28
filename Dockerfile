FROM golang:1.22-alpine AS build
RUN apk add --no-cache git build-base
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY main.go .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags='-s -w' -o /go-service ./main.go

FROM scratch
COPY --from=build /go-service /go-service
EXPOSE 8080
ENTRYPOINT ["/go-service"]
