FROM golang:1.23-alpine AS build

WORKDIR /src

# go.mod ve go.sum aynı klasörde zaten
COPY go.mod go.sum ./
RUN go mod download

# kaynak kodu kopyala
COPY . .

# derle
RUN go build -o app main.go

# ---- runtime ----
FROM gcr.io/distroless/base-debian12

WORKDIR /root/
COPY --from=build /src/app .

CMD ["./app"]
