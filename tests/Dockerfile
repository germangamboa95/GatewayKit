FROM golang:1.27.4
WORKDIR /src
ENV CGO_ENABLED=0 GOPROXY=off
COPY . .
RUN go test -count=1 -run '^$' ./...
CMD ["go", "test", "-v", "-count=1", "./..."]
