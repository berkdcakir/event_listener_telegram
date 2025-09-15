FROM golang:1.22-alpine AS build
WORKDIR /src

# Mod dosyalarını önce kopyala ve bağımlılıkları indir (build context repo kökü)
COPY go-sender/go.mod go-sender/go.sum ./
RUN go mod download

# Tüm kaynakları kopyala ve derle
COPY go-sender/ ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/app ./...

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=build /out/app /app/app

ENV API_HOST=0.0.0.0
ENV API_PORT=8080
EXPOSE 8080

CMD ["/app/app"]


