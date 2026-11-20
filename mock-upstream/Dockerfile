FROM golang:1.27.4 AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /mock-upstream .

FROM scratch
COPY --from=build /mock-upstream /mock-upstream
EXPOSE 3000
ENTRYPOINT ["/mock-upstream"]
