FROM golang:1.27.4 AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOPROXY=off
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/mock-upstream .

FROM scratch
COPY --from=build /out/mock-upstream /mock-upstream
ENTRYPOINT ["/mock-upstream"]
