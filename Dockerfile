FROM golang:1.27.4 AS build
WORKDIR /src
ENV CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/gatewaykit .

FROM scratch
COPY --from=build /out/gatewaykit /gatewaykit
EXPOSE 8080
ENTRYPOINT ["/gatewaykit"]
