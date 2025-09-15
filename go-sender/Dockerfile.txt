FROM golang:1.23-alpine AS build
WORKDIR /src

# go.mod ve go.sum’u kopyala
COPY go-sender/go.mod go-sender/go.sum ./
RUN go mod download

# kaynak kodu kopyala
COPY go-sender/ ./

# derle
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/app .

# runtime image
FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=build /out/app /app/app

ENV API_HOST=0.0.0.0
ENV API_PORT=8080
EXPOSE 8080

CMD ["/app/app"]